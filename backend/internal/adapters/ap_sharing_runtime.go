package adapters

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Ready means that routing/NAT and the owned DNS listener were read back. It
// does not assert that the carrier/remote Internet answers every destination.
type APSharingState struct {
	Enabled    bool                   `json:"enabled"`
	State      string                 `json:"state"`
	Ready      bool                   `json:"ready"`
	DNSReady   bool                   `json:"dnsReady"`
	Upstream   *APSharingUpstreamView `json:"upstream,omitempty"`
	ErrorCode  string                 `json:"errorCode,omitempty"`
	ObservedAt time.Time              `json:"observedAt"`
	Preference string                 `json:"preference"`
}
type apSharingController struct {
	firewall   *apSharingFirewall
	directory  string
	selected   *APSharingUpstream
	state      APSharingState
	dns        string
	preference string
}

func newAPSharingController(directory string) *apSharingController {
	return &apSharingController{directory: directory, firewall: newAPSharingFirewall(directory), preference: "cellular", state: APSharingState{Enabled: true, State: "waiting-upstream", Preference: "cellular"}}
}
func apSharingDNSPath(directory string) string {
	return filepath.Join(apPrivate(directory), "upstream-dns.conf")
}
func apSharingWriteDNS(directory, text string) error {
	// Rename the complete file; SIGHUP clears dnsmasq's old cache and re-reads the
	// servers-file. No system resolver or stock daemon configuration is changed.
	path := apSharingDNSPath(directory)
	temporary := path + ".next"
	f, e := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if e != nil {
		return errors.New("AP_SHARING_DNS_WRITE_FAILED")
	}
	_, e = f.WriteString(text)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e == nil {
		e = os.Rename(temporary, path)
	}
	if e != nil {
		_ = os.Remove(temporary)
		return errors.New("AP_SHARING_DNS_WRITE_FAILED")
	}
	return nil
}
func (s *apSharingController) prepare(ctx context.Context) error {
	if e := apSharingWriteDNS(s.directory, ""); e != nil {
		return e
	}
	return s.firewall.apply(ctx, nil)
}
func apSharingReloadDNS(p apProcess) error {
	if !apMatchesProcess(p) {
		return errors.New("AP_SHARING_DNS_IDENTITY_CHANGED")
	}
	process, e := os.FindProcess(p.PID)
	if e != nil {
		return errors.New("AP_SHARING_DNS_RELOAD_FAILED")
	}
	if process.Signal(syscall.SIGHUP) != nil {
		return errors.New("AP_SHARING_DNS_RELOAD_FAILED")
	}
	return nil
}
func (s *apSharingController) refresh(ctx context.Context, p apProcess, dnsReady bool) APSharingState {
	now := time.Now().UTC()
	state := APSharingState{Enabled: true, State: "waiting-upstream", DNSReady: dnsReady, ObservedAt: now, Preference: apDefaultPreference(s.preference)}
	u, selectionErr := readAPPreferredUpstream(ctx, s.preference, ReadVerifiedCellularUpstream, readVerifiedSTAUpstream, time.Now)
	var selected *APSharingUpstream
	if selectionErr == nil {
		selected = &u
	}
	// observedAt changes on every proof read, but only route/DNS identity requires
	// changing kernel state or flushing clients' DNS cache.
	same := s.selected == nil && selected == nil
	if s.selected != nil && selected != nil {
		old, next := *s.selected, *selected
		old.observedAt = time.Time{}
		next.observedAt = time.Time{}
		same = reflect.DeepEqual(old, next)
	}
	var e error
	if !same {
		if e = apSharingWriteDNS(s.directory, ""); e == nil {
			e = apSharingReloadDNS(p)
		}
		s.dns = ""
	}
	if e == nil {
		e = s.firewall.apply(ctx, selected)
	}
	if e == nil && selected != nil {
		plan, _ := apBuildSharingPlan(s.firewall.owner.Token, selected)
		if s.dns != plan.dns {
			e = apSharingWriteDNS(s.directory, plan.dns)
			if e == nil {
				e = apSharingReloadDNS(p)
			}
			if e == nil {
				s.dns = plan.dns
			}
		}
	}
	if e != nil {
		state.State = "error"
		state.ErrorCode = apSafeError(e)
		s.selected = nil
		// Best effort closes forwarding after a partial failure. AP association and
		// DHCP continue even when the upstream or sharing controller is unavailable.
		_ = apSharingWriteDNS(s.directory, "")
		_ = apSharingReloadDNS(p)
		s.dns = ""
		_ = s.firewall.apply(ctx, nil)
	} else {
		s.selected = selected
		if selected != nil && dnsReady {
			view := selected.public()
			state.Upstream = &view
			state.Ready = true
			state.State = "ready"
		}
	}
	s.state = state
	return state
}

func apOwnsTCP(pid, port int) (bool, error) { return apOwnsPort(pid, port, "tcp") }
func apOwnsPort(pid, port int, protocol string) (bool, error) {
	if protocol != "tcp" && protocol != "udp" {
		return false, errors.New("AP_SOCKET_PROTOCOL_INVALID")
	}
	b, e := os.ReadFile("/proc/net/" + protocol)
	if e != nil {
		return false, errors.New("AP_SOCKET_TABLE_UNAVAILABLE")
	}
	inodes := apSocketInodes(string(b), port)
	if protocol == "tcp" {
		// A connected socket with local port 53 is not a DNS listener.
		listening := []string{}
		for _, line := range strings.Split(string(b), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 3 && fields[3] == "0A" {
				listening = append(listening, line)
			}
		}
		inodes = apSocketInodes(strings.Join(listening, "\n"), port)
	}
	fds, e := os.ReadDir(filepath.Join("/proc", strconv.Itoa(pid), "fd"))
	if e != nil {
		return false, errors.New("AP_DNS_FD_UNAVAILABLE")
	}
	for _, fd := range fds {
		link, e := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "fd", fd.Name()))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return false, errors.New("AP_DNS_FD_UNAVAILABLE")
		}
		if len(link) > 9 && link[:8] == "socket:[" && link[len(link)-1:] == "]" && inodes[link[8:len(link)-1]] {
			return true, nil
		}
	}
	return false, nil
}
