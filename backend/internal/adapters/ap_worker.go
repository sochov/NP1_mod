package adapters

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/np1-control-center/ncc/internal/config"
	"golang.org/x/crypto/pbkdf2"
)

const apPHY = "phy0"

type apProcess struct {
	PID    int    `json:"pid"`
	Start  string `json:"start"`
	Binary string `json:"binary"`
	Config string `json:"config"`
}
type apOwnership struct {
	IfIndex int       `json:"ifIndex"`
	Alias   string    `json:"alias"`
	BootID  string    `json:"bootId"`
	Hostapd apProcess `json:"hostapd"`
	DHCP    apProcess `json:"dhcp"`
}
type apRuntime struct {
	directory, helper string
	ownership         apOwnership
	state             APWorkerState
	children          []*apChild
	created           bool
	sharing           *apSharingController
}
type apChild struct {
	process apProcess
	cmd     *exec.Cmd
	done    chan error
	log     *apBoundedLog
}
type apOutput struct {
	bytes.Buffer
	truncated bool
}

func (w *apOutput) Write(b []byte) (int, error) {
	n := len(b)
	left := 65536 - w.Len()
	if left > 0 {
		if left > n {
			left = n
		}
		_, _ = w.Buffer.Write(b[:left])
	}
	if n > left {
		w.truncated = true
	}
	return n, nil
}
func apRun(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var out apOutput
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	e := cmd.Run()
	if e != nil {
		command := strings.ToUpper(filepath.Base(name))
		command = strings.Map(func(r rune) rune {
			if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
				return r
			}
			return '_'
		}, command)
		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return "", errors.New("AP_" + command + "_TIMED_OUT")
			}
			return "", errors.New("AP_" + command + "_CANCELLED")
		}
		if exit, ok := e.(*exec.ExitError); ok {
			return "", errors.New("AP_" + command + "_EXIT_" + strconv.Itoa(exit.ExitCode()))
		}
		return "", errors.New("AP_" + command + "_START_FAILED")
	}
	if out.truncated {
		return "", errors.New("AP_COMMAND_OUTPUT_LIMIT")
	}
	return strings.TrimSpace(out.String()), nil
}

type apBoundedLog struct {
	mu      sync.Mutex
	f       *os.File
	written int64
}

func (l *apBoundedLog) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.written+int64(len(b)) > 128*1024 {
		if e := l.f.Truncate(0); e != nil {
			return 0, e
		}
		if _, e := l.f.Seek(0, 0); e != nil {
			return 0, e
		}
		l.written = 0
	}
	n, e := l.f.Write(b)
	l.written += int64(n)
	return n, e
}
func apPrivate(runtime string) string { return filepath.Join(runtime, "private") }
func apPrepare(directory string) error {
	if e := os.MkdirAll(directory, 0750); e != nil {
		return e
	}
	if st, e := os.Lstat(directory); e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("AP_RUNTIME_INVALID")
	}
	if e := os.Chmod(directory, 0750); e != nil {
		return e
	}
	return os.MkdirAll(apPrivate(directory), 0700)
}
func apReadOwnership(directory string) (apOwnership, error) {
	var own apOwnership
	b, e := os.ReadFile(filepath.Join(apPrivate(directory), "ownership.json"))
	if e != nil {
		return own, e
	}
	if len(b) > 8192 {
		return own, errors.New("AP_OWNERSHIP_INVALID")
	}
	e = json.Unmarshal(b, &own)
	return own, e
}
func (r *apRuntime) saveOwnership() error {
	return config.Atomic(filepath.Join(apPrivate(r.directory), "ownership.json"), r.ownership)
}
func (r *apRuntime) writeState(stage string, e error) error {
	r.state.State = stage
	r.state.ErrorCode = apSafeError(e)
	r.state.UpdatedAt = time.Now().UTC()
	if e != nil {
		failure := apFailure{Code: apSafeError(e), Stage: stage, At: r.state.UpdatedAt}
		path := filepath.Join(apPrivate(r.directory), "failure.json")
		preserve := false
		if strings.Contains(failure.Code, "CANCELLED") {
			var previous apFailure
			if b, readErr := os.ReadFile(path); readErr == nil && len(b) <= 2048 && json.Unmarshal(b, &previous) == nil && previous.Code != "" {
				preserve = true
			}
		}
		if !preserve {
			_ = config.Atomic(path, failure)
		}
	}
	path := filepath.Join(r.directory, "status.json")
	if e := config.Atomic(path, r.state); e != nil {
		return e
	}
	return os.Chmod(path, 0640)
}
func apInterfaceIdentity() (int, string, string, error) {
	base := "/sys/class/net/" + apInterface
	b, e := os.ReadFile(base + "/ifindex")
	if e != nil {
		return 0, "", "", e
	}
	index, e := strconv.Atoi(strings.TrimSpace(string(b)))
	if e != nil {
		return 0, "", "", e
	}
	phy, e := os.Readlink(base + "/phy80211")
	if e != nil {
		return 0, "", "", e
	}
	alias, e := os.ReadFile(base + "/ifalias")
	return index, filepath.Base(phy), strings.TrimSpace(string(alias)), e
}
func apOwnsInterface(own apOwnership) bool {
	index, phy, alias, e := apInterfaceIdentity()
	return e == nil && index == own.IfIndex && phy == apPHY && alias == own.Alias && strings.HasPrefix(alias, "np1-control-ap:") && len(alias) == 47
}
func apProcessStart(pid int) (string, bool) {
	b, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if e != nil {
		return "", false
	}
	end := strings.LastIndexByte(string(b), ')')
	if end < 0 {
		return "", false
	}
	fields := strings.Fields(string(b[end+1:]))
	if len(fields) < 20 || fields[0] == "Z" {
		return "", false
	}
	return fields[19], true
}
func apMatchesProcess(p apProcess) bool {
	if p.PID <= 1 || p.Start == "" {
		return false
	}
	start, live := apProcessStart(p.PID)
	if !live || start != p.Start {
		return false
	}
	b, e := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", p.PID))
	if e != nil {
		return false
	}
	args := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
	binary, conf := false, false
	for _, arg := range args {
		binary = binary || arg == p.Binary
		conf = conf || arg == p.Config || arg == "--conf-file="+p.Config
	}
	return binary && conf
}
func apStopProcess(p apProcess) error {
	if !apMatchesProcess(p) {
		return nil
	}
	process, e := os.FindProcess(p.PID)
	if e != nil {
		return e
	}
	_ = process.Signal(syscall.SIGTERM)
	until := time.Now().Add(3 * time.Second)
	for apMatchesProcess(p) && time.Now().Before(until) {
		time.Sleep(50 * time.Millisecond)
	}
	if apMatchesProcess(p) {
		_ = process.Kill()
	}
	until = time.Now().Add(time.Second)
	for apMatchesProcess(p) && time.Now().Before(until) {
		time.Sleep(50 * time.Millisecond)
	}
	if apMatchesProcess(p) {
		return errors.New("AP_CHILD_STOP_FAILED")
	}
	return nil
}
func apCleanupAt(directory string) error {
	own, e := apReadOwnership(directory)
	if os.IsNotExist(e) {
		if _, err := os.Stat("/sys/class/net/" + apInterface); os.IsNotExist(err) {
			return nil
		}
		return errors.New("AP_INTERFACE_NOT_OWNED")
	}
	if e != nil {
		return errors.New("AP_OWNERSHIP_INVALID")
	}
	boot := read("/proc/sys/kernel/random/boot_id")
	if own.BootID == "" || own.BootID != boot {
		return errors.New("AP_OWNERSHIP_BOOT_MISMATCH")
	}
	var result error
	for _, p := range []apProcess{own.DHCP, own.Hostapd} {
		if e := apStopProcess(p); e != nil {
			result = e
		}
	}
	if result != nil {
		return result
	}
	if e := newAPSharingFirewall(directory).cleanup(context.Background()); e != nil {
		return e
	}
	if _, e := os.Stat("/sys/class/net/" + apInterface); e == nil {
		if !apOwnsInterface(own) {
			return errors.New("AP_INTERFACE_OWNERSHIP_CHANGED")
		}
		if _, e = apRun(context.Background(), "iw", "dev", apInterface, "del"); e != nil {
			result = e
		}
	}
	if _, e := os.Stat("/sys/class/net/" + apInterface); e == nil {
		return errors.New("AP_INTERFACE_CLEANUP_FAILED")
	}
	for _, name := range []string{"hostapd.conf", "dnsmasq.conf", "join.conf", "leases", "dnsmasq.pid", "upstream-dns.conf"} {
		if e := os.Remove(filepath.Join(apPrivate(directory), name)); e != nil && !os.IsNotExist(e) {
			result = e
		}
	}
	if result == nil {
		_ = os.Remove(filepath.Join(apPrivate(directory), "ownership.json"))
	}
	return result
}

// RunAPCleanup recovers only resources whose private record, boot, ifindex, PHY and ifalias agree.
func RunAPCleanup() error {
	if e := apPrepare(APRuntimeDirectory); e != nil {
		return e
	}
	release, e := apLock(filepath.Join(apPrivate(APRuntimeDirectory), "worker.lock"))
	if e != nil {
		return errors.New("AP_WORKER_BUSY")
	}
	defer release()
	if e = apCleanupAt(APRuntimeDirectory); e != nil {
		return e
	}
	c, _ := apLoadConfig(filepath.Join(APDirectory, "config.json"))
	r := apRuntime{directory: APRuntimeDirectory, state: APWorkerState{Revision: c.Revision}}
	return r.writeState("stopped", nil)
}
func apPrefix(value string) (*net.IPNet, bool) {
	if ip := net.ParseIP(value); ip != nil && ip.To4() != nil {
		return &net.IPNet{IP: ip.To4(), Mask: net.CIDRMask(32, 32)}, true
	}
	_, n, e := net.ParseCIDR(value)
	return n, e == nil && n.IP.To4() != nil
}
func apOverlap(text string) bool {
	_, candidate, _ := net.ParseCIDR(apAddress)
	for _, line := range strings.Split(text, "\n") {
		for _, field := range strings.Fields(line) {
			if field == "default" || field == "0.0.0.0/0" {
				continue
			}
			if n, ok := apPrefix(field); ok && (n.Contains(candidate.IP) || candidate.Contains(n.IP)) {
				return true
			}
		}
	}
	return false
}
func apCheckSubnet(ctx context.Context) error {
	routes, e := apRun(ctx, "ip", "-4", "route", "show", "table", "all")
	if e != nil {
		return e
	}
	addresses, e := apRun(ctx, "ip", "-4", "-o", "address", "show")
	if e != nil {
		return e
	}
	if apOverlap(routes) || apOverlap(addresses) {
		return errors.New("AP_SUBNET_OVERLAP")
	}
	return nil
}
func apChannel(ctx context.Context, helper string, fallback bool) (int, error) {
	value, e := apRun(ctx, "python3", helper, "--ap-state")
	if e != nil {
		return 0, e
	}
	if value == "disconnected" && fallback {
		return 6, nil
	}
	fields := strings.Fields(value)
	if len(fields) == 2 && fields[0] == "associated" {
		n, e := strconv.Atoi(fields[1])
		if e == nil && n >= 1 && n <= 13 {
			return n, nil
		}
	}
	return 0, errors.New("AP_WAITING_STA")
}
func apHostapdConfig(c apConfiguration, channel int, directory string) string {
	psk := pbkdf2.Key([]byte(c.Passphrase), []byte(c.SSID), 4096, 32, sha1.New)
	return fmt.Sprintf("interface=%s\ndriver=nl80211\nssid2=%s\nhw_mode=g\nchannel=%d\nbeacon_int=100\nwmm_enabled=1\nauth_algs=1\nwpa=2\nwpa_psk=%s\nwpa_key_mgmt=WPA-PSK\nrsn_pairwise=CCMP\nctrl_interface=%s\nlogger_syslog=0\nlogger_stdout=-1\nlogger_stdout_level=2\n", apInterface, hex.EncodeToString([]byte(c.SSID)), channel, hex.EncodeToString(psk), filepath.ToSlash(filepath.Join(apPrivate(directory), "ctrl")))
}
func apDHCPConfig(directory string) string {
	return fmt.Sprintf("port=53\ninterface=%s\nlisten-address=192.168.226.1\nbind-interfaces\nexcept-interface=lo\nno-resolv\nno-hosts\nservers-file=%s\nuser=root\ngroup=root\ndhcp-range=192.168.226.20,192.168.226.99,255.255.255.0,10m\ndhcp-option=3,192.168.226.1\ndhcp-option=6,192.168.226.1\ndhcp-leasefile=%s\npid-file=%s\nlog-facility=-\n", apInterface, apSharingDNSPath(directory), filepath.Join(apPrivate(directory), "leases"), filepath.Join(apPrivate(directory), "dnsmasq.pid"))
}
func (r *apRuntime) startChild(binary, configuration string, args ...string) (apProcess, error) {
	f, e := os.OpenFile(filepath.Join(apPrivate(r.directory), filepath.Base(binary)+".log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if e != nil {
		return apProcess{}, e
	}
	log := &apBoundedLog{f: f}
	cmd := exec.Command(binary, args...)
	cmd.Stdout = log
	cmd.Stderr = log
	if e = cmd.Start(); e != nil {
		f.Close()
		return apProcess{}, errors.New("AP_CHILD_START_FAILED")
	}
	child := &apChild{done: make(chan error, 1), log: log, cmd: cmd}
	r.children = append(r.children, child)
	go func() { child.done <- cmd.Wait(); _ = f.Close() }()
	start, ok := apProcessStart(cmd.Process.Pid)
	if !ok {
		_ = cmd.Process.Kill()
		return apProcess{}, errors.New("AP_CHILD_EXITED")
	}
	child.process = apProcess{cmd.Process.Pid, start, binary, configuration}
	// /proc/cmdline may briefly be empty while exec changes the process image.
	// Never infer an exit from the first exact argv read after Start.
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		if apMatchesProcess(child.process) {
			return child.process, nil
		}
		if current, live := apProcessStart(child.process.PID); !live || current != start {
			return apProcess{}, errors.New("AP_CHILD_EXITED")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return apProcess{}, errors.New("AP_CHILD_IDENTITY_UNCONFIRMED")
}
func apHostapdStatus(value string, channel int) (*int, error) {
	fields := map[string]string{}
	for _, line := range strings.Split(value, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			fields[k] = v
		}
	}
	if fields["state"] != "ENABLED" || fields["channel"] != strconv.Itoa(channel) {
		return nil, errors.New("AP_NOT_ENABLED_ON_STA_CHANNEL")
	}
	value, ok := fields["num_sta[0]"]
	if !ok {
		return nil, nil
	}
	n, e := strconv.Atoi(value)
	if e != nil || n < 0 || n > 4096 {
		return nil, errors.New("AP_CLIENT_COUNT_INVALID")
	}
	return &n, nil
}
func apSocketInodes(udp string, port int) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(udp, "\n") {
		f := strings.Fields(line)
		if len(f) < 10 {
			continue
		}
		_, suffix, ok := strings.Cut(f[1], ":")
		p, e := strconv.ParseUint(suffix, 16, 16)
		if ok && e == nil && int(p) == port {
			out[f[9]] = true
		}
	}
	return out
}
func apOwnsUDP(pid, port int) (bool, error) {
	b, e := os.ReadFile("/proc/net/udp")
	if e != nil {
		return false, errors.New("AP_UDP_TABLE_UNAVAILABLE")
	}
	inodes := apSocketInodes(string(b), port)
	fds, e := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
	if e != nil {
		if os.IsPermission(e) {
			return false, errors.New("AP_DHCP_FD_PERMISSION_DENIED")
		}
		return false, errors.New("AP_DHCP_FD_UNAVAILABLE")
	}
	for _, fd := range fds {
		link, e := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", pid, fd.Name()))
		if e != nil {
			if os.IsNotExist(e) {
				continue
			}
			if os.IsPermission(e) {
				return false, errors.New("AP_DHCP_FD_PERMISSION_DENIED")
			}
			return false, errors.New("AP_DHCP_FD_UNAVAILABLE")
		}
		if strings.HasPrefix(link, "socket:[") && strings.HasSuffix(link, "]") && inodes[strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")] {
			return true, nil
		}
	}
	return false, nil
}
func (r *apRuntime) verify(ctx context.Context) error {
	if !apOwnsInterface(r.ownership) {
		return errors.New("AP_INTERFACE_OWNERSHIP_CHANGED")
	}
	if !apMatchesProcess(r.ownership.Hostapd) || !apMatchesProcess(r.ownership.DHCP) {
		return errors.New("AP_CHILD_EXITED")
	}
	status, e := apRun(ctx, "hostapd_cli", "-s", apPrivate(r.directory), "-p", filepath.Join(apPrivate(r.directory), "ctrl"), "-i", apInterface, "status")
	if e != nil {
		return e
	}
	count, e := apHostapdStatus(status, r.state.Channel)
	if e != nil {
		return e
	}
	address, e := apRun(ctx, "ip", "-4", "-o", "address", "show", "dev", apInterface)
	if e != nil {
		return e
	}
	if !strings.Contains(" "+address+" ", " inet "+apAddress+" ") {
		return errors.New("AP_ADDRESS_NOT_CONFIRMED")
	}
	dhcp, e := apOwnsUDP(r.ownership.DHCP.PID, 67)
	if e != nil {
		return e
	}
	dns, e := apOwnsUDP(r.ownership.DHCP.PID, 53)
	if e != nil {
		return e
	}
	tcpDNS, e := apOwnsTCP(r.ownership.DHCP.PID, 53)
	if e != nil {
		return e
	}
	if !dhcp || !dns || !tcpDNS {
		return errors.New("AP_DHCP_SOCKET_NOT_CONFIRMED")
	}
	if r.sharing != nil {
		r.state.Sharing = r.sharing.refresh(ctx, r.ownership.DHCP, dns && tcpDNS)
	}
	r.state.Enabled = true
	r.state.DHCPReady = true
	r.state.ClientCount = count
	r.captureClients(ctx)
	r.state.Address = apAddress
	apRememberChannel(r.directory, r.state.Channel)
	return r.writeState("enabled", nil)
}
func (r *apRuntime) start(ctx context.Context, c apConfiguration, channel int) error {
	if _, e := os.Stat("/sys/class/net/" + apInterface); !os.IsNotExist(e) {
		return errors.New("AP_INTERFACE_ALREADY_EXISTS")
	}
	if e := apCheckSubnet(ctx); e != nil {
		return e
	}
	private := apPrivate(r.directory)
	hostConfig := filepath.Join(private, "hostapd.conf")
	dhcpConfig := filepath.Join(private, "dnsmasq.conf")
	if e := os.WriteFile(hostConfig, []byte(apHostapdConfig(c, channel, r.directory)), 0600); e != nil {
		return e
	}
	if e := apSharingWriteDNS(r.directory, ""); e != nil {
		return e
	}
	if e := os.WriteFile(dhcpConfig, []byte(apDHCPConfig(r.directory)), 0600); e != nil {
		return e
	}
	if _, e := apRun(ctx, "dnsmasq", "--test", "--conf-file="+dhcpConfig); e != nil {
		return errors.New("AP_DHCP_CONFIG_REJECTED")
	}
	token := make([]byte, 16)
	if _, e := rand.Read(token); e != nil {
		return e
	}
	r.ownership = apOwnership{Alias: "np1-control-ap:" + hex.EncodeToString(token), BootID: read("/proc/sys/kernel/random/boot_id")}
	if _, e := apRun(ctx, "iw", "phy", apPHY, "interface", "add", apInterface, "type", "__ap"); e != nil {
		return e
	}
	r.created = true
	r.traceStart("interface-created")
	// Record creation before any further mutation. Until alias is confirmed cleanup remains fail-closed.
	index, phy, _, e := apInterfaceIdentity()
	if e != nil || phy != apPHY {
		return errors.New("AP_INTERFACE_IDENTITY_UNAVAILABLE")
	}
	r.ownership.IfIndex = index
	if e = r.saveOwnership(); e != nil {
		return e
	}
	if _, e = apRun(ctx, "ip", "link", "set", "dev", apInterface, "alias", r.ownership.Alias); e != nil {
		return e
	}
	if !apOwnsInterface(r.ownership) {
		return errors.New("AP_INTERFACE_OWNERSHIP_CHANGED")
	}
	r.traceStart("alias-confirmed")
	r.sharing = newAPSharingController(r.directory)
	r.sharing.preference = apDefaultPreference(c.Preference)
	if e = r.sharing.prepare(ctx); e != nil {
		return e
	}
	if _, e = apRun(ctx, "ip", "address", "add", apAddress, "dev", apInterface); e != nil {
		return e
	}
	r.traceStart("address-added")
	r.ownership.Hostapd, e = r.startChild("/usr/sbin/hostapd", hostConfig, hostConfig)
	r.traceStart("hostapd-start-returned")
	if e != nil {
		return e
	}
	if e = r.saveOwnership(); e != nil {
		return e
	}
	r.state.Channel = channel
	until := time.Now().Add(10 * time.Second)
	ready := false
	var statusError error
	for time.Now().Before(until) {
		if !apMatchesProcess(r.ownership.Hostapd) {
			r.traceStart("hostapd-process-unmatched")
			return errors.New("AP_HOSTAPD_EXITED")
		}
		status, e := apRun(ctx, "hostapd_cli", "-s", private, "-p", filepath.Join(private, "ctrl"), "-i", apInterface, "status")
		statusError = e
		if e == nil {
			if _, e = apHostapdStatus(status, channel); e == nil {
				ready = true
				break
			}
			statusError = e
		}
		if ctx.Err() != nil {
			if statusError != nil {
				return statusError
			}
			return ctx.Err()
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ready {
		if statusError != nil {
			return statusError
		}
		return errors.New("AP_HOSTAPD_STATUS_UNVERIFIED")
	}
	r.ownership.DHCP, e = r.startChild("/usr/bin/dnsmasq", dhcpConfig, "--keep-in-foreground", "--conf-file="+dhcpConfig)
	if e != nil {
		return e
	}
	if e = r.saveOwnership(); e != nil {
		return e
	}
	until = time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		if e = r.verify(ctx); e == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		time.Sleep(100 * time.Millisecond)
	}
	return e
}
func (r *apRuntime) stop() error {
	// In-memory direct children remain ours even if ownership persistence failed.
	for _, child := range r.children {
		_ = apStopProcess(child.process)
		// Live creator also owns the returned child handle. If exact argv is
		// temporarily unavailable, finish this same starttime child before deleting
		// its interface; crash recovery still requires strict persisted identity.
		if child.cmd != nil && child.process.PID > 1 {
			if start, live := apProcessStart(child.process.PID); live && start == child.process.Start {
				_ = child.cmd.Process.Signal(syscall.SIGTERM)
				until := time.Now().Add(time.Second)
				for time.Now().Before(until) {
					if start, live = apProcessStart(child.process.PID); !live || start != child.process.Start {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if start, live = apProcessStart(child.process.PID); live && start == child.process.Start {
					_ = child.cmd.Process.Kill()
				}
			}
		}
	}
	// Close the creation-to-alias window only in the still-live creator. Crash cleanup
	// never accepts an unmarked interface or a reused/replaced ifindex.
	if r.created && r.ownership.IfIndex > 0 {
		index, phy, alias, identityErr := apInterfaceIdentity()
		if identityErr == nil && index == r.ownership.IfIndex && phy == apPHY && alias == "" {
			_, _ = apRun(context.Background(), "iw", "dev", apInterface, "del")
		}
	}
	e := apCleanupAt(r.directory)
	for _, child := range r.children {
		select {
		case <-child.done:
		case <-time.After(time.Second):
		}
	}
	r.children = nil
	r.created = false
	r.state.Enabled = false
	r.state.DHCPReady = false
	r.state.Sharing = APSharingState{Enabled: true, State: "stopped"}
	r.sharing = nil
	r.state.ClientCount = nil
	r.state.Clients = nil
	r.state.ClientsAvailable = false
	for _, name := range []string{"hostapd.conf", "dnsmasq.conf", "join.conf"} {
		_ = os.Remove(filepath.Join(apPrivate(r.directory), name))
	}
	if e != nil {
		_ = r.writeState("error", e)
	} else if r.state.ErrorCode != "" {
		_ = r.writeState("error", errors.New(r.state.ErrorCode))
	} else {
		_ = r.writeState("stopped", nil)
	}
	return e
}
func apNewRuntime(directory, helper string, c apConfiguration) (*apRuntime, func(), error) {
	if e := apPrepare(directory); e != nil {
		return nil, nil, e
	}
	release, e := apLock(filepath.Join(apPrivate(directory), "worker.lock"))
	if e != nil {
		return nil, nil, errors.New("AP_WORKER_BUSY")
	}
	if e = apCleanupAt(directory); e != nil {
		release()
		return nil, nil, e
	}
	return &apRuntime{directory: directory, helper: helper, state: APWorkerState{Revision: c.Revision}}, release, nil
}

// RunAP keeps its own hotspot available and shares a verified cellular or STA uplink.
// Stock supplicant, hostapd, bridge, routing tables and firewall chains are retained.
func RunAP(ctx context.Context) error {
	c, e := apLoadConfig(filepath.Join(APDirectory, "config.json"))
	if e != nil {
		return e
	}
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	r, release, e := apNewRuntime(APRuntimeDirectory, filepath.Join(filepath.Dir(exe), "ap-channel.py"), c)
	if e != nil {
		return e
	}
	defer release()
	defer r.stop()
	if !c.Enabled {
		return r.writeState("stopped", nil)
	}
	for ctx.Err() == nil {
		apReloadPreference(r, &c)
		associated, observationError := apChannel(ctx, r.helper, false)
		if observationError != nil {
			associated = 0
		}
		decision, e := apChooseChannel(0, associated, apReadLastChannel(r.directory, read("/proc/sys/kernel/random/boot_id")), c.StandaloneFallback)
		if e != nil {
			_ = r.writeState("waiting-sta", e)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(2 * time.Second):
			}
			apReloadPreference(r, &c)
			continue
		}
		channel := decision.Channel
		r.state.ChannelSource = decision.Source
		if e = r.start(ctx, c, channel); e != nil {
			_ = r.writeState("error", e)
			return e
		}
		for ctx.Err() == nil {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(2 * time.Second):
			}
			changed, e := r.steadyTick(ctx, &c, channel)
			if e != nil {
				_ = r.writeState("error", e)
				return e
			}
			if changed {
				break
			}
		}
	}
	return nil
}

func apBaseline(ctx context.Context, helper string, ignoreOwn bool) (map[string]string, error) {
	out := map[string]string{}
	for _, unit := range []string{"app-@Communication.service", "app-@DriveRec.service", "app-@ImageRecog.service", "app-@Voice.service", "qmmf-server.service", "NEventWatcherCom.service"} {
		value, e := apRun(ctx, "systemctl", "show", unit, "-p", "MainPID", "-p", "ActiveState", "-p", "SubState")
		pidOK := false
		for _, line := range strings.Split(value, "\n") {
			if strings.HasPrefix(line, "MainPID=") {
				pid, parseErr := strconv.Atoi(strings.TrimPrefix(line, "MainPID="))
				pidOK = parseErr == nil && pid > 1
			}
		}
		if e != nil || !strings.Contains(value, "ActiveState=active") || !pidOK {
			return nil, errors.New("AP_STOCK_BASELINE_UNHEALTHY")
		}
		out[unit] = value
	}
	qcmap, e := apRun(ctx, "systemctl", "show", "QCMAP_ConnectionManagerd.service", "-p", "MainPID", "-p", "ActiveState", "-p", "SubState")
	if e != nil {
		return nil, errors.New("AP_QCMAP_UNIT_BASELINE_UNAVAILABLE")
	}
	out["QCMAP_ConnectionManagerd.service"] = qcmap
	processes, e := os.ReadDir("/proc")
	if e != nil {
		return nil, errors.New("AP_PROCESS_BASELINE_UNAVAILABLE")
	}
	qcmaps := []string{}
	for _, entry := range processes {
		pid, parseErr := strconv.Atoi(entry.Name())
		if parseErr != nil || pid < 2 {
			continue
		}
		args, readErr := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if readErr != nil {
			continue
		}
		first := strings.SplitN(string(args), "\x00", 2)[0]
		if strings.Contains(strings.ToLower(filepath.Base(first)), "qcmap") {
			if start, live := apProcessStart(pid); live {
				qcmaps = append(qcmaps, strconv.Itoa(pid)+":"+start)
			}
		}
	}
	sort.Strings(qcmaps)
	out["QCMAP-processes"] = strings.Join(qcmaps, " ") // Empty is an observed baseline, not a claim of daemon health.
	value, e := apRun(ctx, "python3", helper, "--snapshot")
	if e != nil {
		return nil, e
	}
	out["STA"] = value
	for _, family := range []string{"-4", "-6"} {
		for _, kind := range []string{"route", "rule"} {
			args := []string{family, kind, "show"}
			if kind == "route" {
				args = append(args, "table", "all")
			}
			value, e := apRun(ctx, "ip", args...)
			if e != nil {
				return nil, e
			}
			lines := []string{}
			for _, line := range strings.Split(value, "\n") {
				line = strings.Join(strings.Fields(line), " ")
				if ignoreOwn && family == "-4" && (line == sharingExpectedRule() || strings.Contains(" "+line+" ", " table "+apSharingTable+" ") && strings.Contains(" "+line+" ", " proto "+apSharingProtocol+" ")) {
					continue
				}
				if ignoreOwn && strings.Contains(" "+line+" ", " dev "+apInterface+" ") {
					continue
				}
				if line != "" {
					lines = append(lines, line)
				}
			}
			sort.Strings(lines)
			out[family+"-"+kind] = strings.Join(lines, "\n")
		}
	}
	return out, nil
}
func apCompareBaseline(a, b map[string]string) error {
	if len(a) != len(b) {
		return errors.New("AP_BASELINE_CHANGED")
	}
	for k, v := range a {
		if b[k] != v {
			return errors.New("AP_BASELINE_CHANGED")
		}
	}
	return nil
}

// RunAPSmoke is bounded to 30 seconds after readiness, with baseline/cleanup read-back on all exits.
func RunAPSmoke(ctx context.Context) (result error) {
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	base := filepath.Dir(exe)
	directory := filepath.Join(base, "runtime")
	helper := filepath.Join(base, "ap-channel.py")
	before, e := apBaseline(ctx, helper, false)
	if e != nil {
		return e
	}
	if e = config.Atomic(filepath.Join(base, "before.json"), before); e != nil {
		return e
	}
	secret := make([]byte, 16)
	if _, e = rand.Read(secret); e != nil {
		return e
	}
	c := apConfiguration{Revision: 1, Enabled: true, SSID: "NP1-LAN-TEST", Passphrase: hex.EncodeToString(secret)}
	r, release, e := apNewRuntime(directory, helper, c)
	if e != nil {
		return e
	}
	defer release()
	defer func() {
		if e := r.stop(); e != nil {
			result = e
		}
		after, e := apBaseline(context.Background(), helper, false)
		if e != nil {
			result = e
		} else {
			_ = config.Atomic(filepath.Join(base, "after.json"), after)
			if e = apCompareBaseline(before, after); e != nil {
				result = e
			}
		}
	}()
	channel, e := apChannel(ctx, helper, false)
	if e != nil {
		return e
	}
	if e = r.start(ctx, c, channel); e != nil {
		return e
	}
	if e = config.Atomic(filepath.Join(apPrivate(directory), "join.conf"), map[string]string{"ssid": c.SSID, "passphrase": c.Passphrase}); e != nil {
		return e
	}
	activeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	during, e := apBaseline(activeCtx, helper, true)
	if e != nil {
		return e
	}
	_ = config.Atomic(filepath.Join(base, "during.json"), during)
	if e = apCompareBaseline(before, during); e != nil {
		return e
	}
	fmt.Println("AP + owned DHCP UDP/67 ready for 30 seconds; private connection data is in runtime/private/join.conf")
	for {
		select {
		case <-activeCtx.Done():
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return nil
		case <-time.After(time.Second):
		}
		current, e := apChannel(activeCtx, helper, false)
		if activeCtx.Err() != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return nil
		}
		if e != nil || current != channel {
			return errors.New("AP_STA_CHANNEL_CHANGED")
		}
		if e = r.verify(activeCtx); e != nil {
			if activeCtx.Err() != nil && ctx.Err() == nil {
				return nil
			}
			return e
		}
	}
}
