package adapters

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// STAOutageTrial reproduces "the saved Wi-Fi went out of range" without touching the
// stock Wi-Fi setting. DISCONNECT, and even DISABLE_NETWORK, are undone by the stock
// owner within seconds, so the trial pins every saved network to a locally administered
// BSSID that no access point uses. The station then scans like a car out of range. The
// change lives in the supplicant's memory only (the stock configuration file is read-only
// and update_config is off), and the BSSID is always reset to "any" afterwards.
// Each observation is one JSON line on out.
type STAOutageSample struct {
	At           time.Time         `json:"at"`
	Phase        string            `json:"phase"`
	WPAState     string            `json:"wpaState"`
	DefaultIface string            `json:"defaultIface"`
	Addresses    map[string]string `json:"addresses"`
	CM           bool              `json:"connectionManager"`
	DNS          bool              `json:"dns"`
	TCP          bool              `json:"tcp"`
	HTTPS        bool              `json:"https"`
	APUpstream   string            `json:"apUpstream"`
	APReady      bool              `json:"apReady"`
	Networks     string            `json:"networks"`
	Error        string            `json:"error,omitempty"`
}

func wpaState() string {
	raw, e := vendorWPA("STATUS")
	if e != nil {
		return "unreadable"
	}
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "wpa_state=") {
			return strings.TrimPrefix(line, "wpa_state=")
		}
	}
	return "unknown"
}
func defaultIface() string {
	f, e := os.Open("/proc/net/route")
	if e != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) > 7 && fields[1] == "00000000" && fields[7] == "00000000" {
			return fields[0]
		}
	}
	return ""
}
func uplinkAddresses() map[string]string {
	out := map[string]string{}
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if !(strings.HasPrefix(iface.Name, "wlan0") || strings.HasPrefix(iface.Name, "usb0") || strings.HasPrefix(iface.Name, "rmnet")) {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ip, _, e := net.ParseCIDR(addr.String()); e == nil && ip.To4() != nil {
				out[iface.Name] = addr.String()
			}
		}
	}
	return out
}
func connectionManagerRunning() bool {
	entries, _ := os.ReadDir("/proc")
	for _, entry := range entries {
		if _, e := strconv.Atoi(entry.Name()); e != nil {
			continue
		}
		raw, e := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if e == nil && strings.Contains(strings.ToLower(string(raw)), "quectel-cm") {
			return true
		}
	}
	return false
}
func apUpstream() (string, bool) {
	raw, e := os.ReadFile(filepath.Join(APRuntimeDirectory, "status.json"))
	if e != nil {
		return "", false
	}
	var state APWorkerState
	if json.Unmarshal(raw, &state) != nil {
		return "", false
	}
	kind := ""
	if state.Sharing.Upstream != nil {
		kind = state.Sharing.Upstream.Kind
	}
	return kind, state.Sharing.Ready
}
func reachability(ctx context.Context) (dns, tcp, https bool) {
	probe, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	if addrs, e := net.DefaultResolver.LookupHost(probe, "www.gstatic.com"); e == nil && len(addrs) > 0 {
		dns = true
	}
	if conn, e := (&net.Dialer{Timeout: 4 * time.Second}).DialContext(probe, "tcp", "1.1.1.1:443"); e == nil {
		tcp = true
		conn.Close()
	}
	client := &http.Client{Timeout: 6 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}}
	if response, e := client.Get("https://www.gstatic.com/generate_204"); e == nil {
		https = response.StatusCode == 204
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
	}
	return
}
func sample(ctx context.Context, phase string) STAOutageSample {
	s := STAOutageSample{At: time.Now().UTC(), Phase: phase, WPAState: wpaState(), DefaultIface: defaultIface(), Addresses: uplinkAddresses(), CM: connectionManagerRunning()}
	s.APUpstream, s.APReady = apUpstream()
	if raw, e := vendorWPA("LIST_NETWORKS"); e == nil {
		var rows []string
		for _, line := range strings.Split(strings.TrimSpace(raw), "\n")[1:] {
			f := strings.Split(line, "\t")
			if len(f) >= 4 {
				rows = append(rows, f[0]+":"+f[3])
			}
		}
		s.Networks = strings.Join(rows, " ")
	}
	s.DNS, s.TCP, s.HTTPS = reachability(ctx)
	return s
}

func RunSTAOutageTrial(ctx context.Context, hold time.Duration, out io.Writer) error {
	if os.Geteuid() != 0 {
		return errors.New("STA_OUTAGE_ROOT_REQUIRED")
	}
	if hold < 20*time.Second || hold > 10*time.Minute {
		return errors.New("STA_OUTAGE_HOLD_RANGE")
	}
	encoder := json.NewEncoder(out)
	before := sample(ctx, "before")
	encoder.Encode(before)
	if before.WPAState != "COMPLETED" {
		return fmt.Errorf("STA_OUTAGE_NOT_CONNECTED:%s", before.WPAState)
	}
	listing, e := vendorWPA("LIST_NETWORKS")
	if e != nil {
		return errors.New("STA_OUTAGE_LIST_FAILED")
	}
	networks, e := parseWiFiNetworks(listing)
	if e != nil {
		return errors.New("STA_OUTAGE_LIST_FAILED")
	}
	var pinned []int
	// From here on the station is always restored, even if the trial is interrupted.
	defer func() {
		for _, id := range pinned {
			vendorWPA("SET_NETWORK " + strconv.Itoa(id) + " bssid any")
		}
		vendorWPA("RECONNECT")
		deadline := time.Now().Add(90 * time.Second)
		for time.Now().Before(deadline) {
			if wpaState() == "COMPLETED" && defaultIface() == "wlan0" {
				break
			}
			time.Sleep(3 * time.Second)
		}
		encoder.Encode(sample(context.Background(), "restored"))
	}()
	for _, network := range networks {
		reply, e := vendorWPA("SET_NETWORK " + strconv.Itoa(network.ID) + " bssid 02:00:00:00:00:01")
		if e != nil || strings.TrimSpace(reply) != "OK" {
			return errors.New("STA_OUTAGE_PIN_FAILED")
		}
		pinned = append(pinned, network.ID)
	}
	if reply, e := vendorWPA("DISCONNECT"); e != nil || strings.TrimSpace(reply) != "OK" {
		return errors.New("STA_OUTAGE_DISCONNECT_FAILED")
	}
	vendorWPA("REASSOCIATE")
	until := time.Now().Add(hold)
	for time.Now().Before(until) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
		encoder.Encode(sample(ctx, "outage"))
	}
	return nil
}

// WiFiScanProbe reports whether the supplicant answers and which saved SSIDs a scan sees.
// It is used to learn what remains available while the stock STA mode is off.
func WiFiScanProbe(out io.Writer) {
	result := map[string]any{"at": time.Now().UTC(), "wpaState": wpaState()}
	_, statErr := os.Stat("/sys/class/net/wlan0")
	result["wlan0"] = statErr == nil
	if reply, e := vendorWPA("SCAN"); e == nil {
		result["scan"] = strings.TrimSpace(reply)
		time.Sleep(6 * time.Second)
		if raw, e := vendorWPA("SCAN_RESULTS"); e == nil {
			result["bssCount"] = len(strings.Split(strings.TrimSpace(raw), "\n")) - 1
		}
	} else {
		result["scanError"] = e.Error()
	}
	if raw, e := vendorWPA("LIST_NETWORKS"); e == nil {
		result["savedNetworks"] = len(strings.Split(strings.TrimSpace(raw), "\n")) - 1
	}
	json.NewEncoder(out).Encode(result)
}
