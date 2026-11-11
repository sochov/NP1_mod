package modem

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A private management witness, never returned as APN settings or public data.
// APN changes may remove cellular defaults; the local AP must remain usable.
type nativeAPNHotspotBaseline struct {
	BootID        string `json:"bootId"`
	IfIndex       int    `json:"ifIndex"`
	AddressPrefix string `json:"addressPrefix"`
	OwnershipHash string `json:"ownershipHash"`
	Service       string `json:"service"`
}

type nativeAPNHotspotIO struct {
	read    func(string) ([]byte, error)
	command func(context.Context, string, ...string) (string, error)
	http    func(context.Context, string) error
	now     func() time.Time
}

var nativeAPNHex64 = regexp.MustCompile(`^[a-f0-9]{64}$`)
var nativeAPNBootPattern = regexp.MustCompile(`^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$`)
var nativeAPNInvocationPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func nativeAPNHotspotHTTPResponse(status int, data []byte) bool {
	return status == 200 && len(data) <= 8192 && bytes.Contains(data, []byte(`<div id="app">`))
}

func nativeAPNProcessMatches(raw []byte, pid int, start string) bool {
	if len(raw) > 8192 || pid <= 1 {
		return false
	}
	text := string(raw)
	end := strings.LastIndexByte(text, ')')
	if end < 0 || !strings.HasPrefix(text, strconv.Itoa(pid)+" (") {
		return false
	}
	fields := strings.Fields(text[end+1:])
	if len(fields) < 20 || fields[0] == "Z" || fields[0] == "X" || fields[19] != start {
		return false
	}
	value, err := strconv.ParseUint(start, 10, 64)
	return err == nil && value > 0 && strconv.FormatUint(value, 10) == start
}

func nativeAPNServiceReady(raw string) bool {
	values := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return false
		}
		if _, duplicate := values[key]; duplicate {
			return false
		}
		values[key] = value
	}
	pid, err := strconv.ParseUint(values["MainPID"], 10, 32)
	return err == nil && pid > 1 && values["Id"] == "np1-control-ap.service" && values["ActiveState"] == "active" && values["SubState"] == "running" && values["Job"] == "" && nativeAPNInvocationPattern.MatchString(values["InvocationID"])
}

func nativeAPNHotspotAddress(raw string) (netip.Prefix, error) {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) != 1 {
		return netip.Prefix{}, errors.New("APN_HOTSPOT_ADDRESS_AMBIGUOUS")
	}
	fields := strings.Fields(lines[0])
	if len(fields) < 4 || fields[1] != "nccap0" || fields[2] != "inet" {
		return netip.Prefix{}, errors.New("APN_HOTSPOT_ADDRESS_UNVERIFIED")
	}
	value, err := netip.ParsePrefix(fields[3])
	if err != nil || !value.Addr().Is4() || !value.Addr().IsGlobalUnicast() || value.Bits() < 8 || value.Bits() > 30 {
		return netip.Prefix{}, errors.New("APN_HOTSPOT_ADDRESS_UNVERIFIED")
	}
	return value, nil
}

func readNativeAPNHotspot(ctx context.Context, d nativeAPNHotspotIO) (nativeAPNHotspotBaseline, error) {
	var out nativeAPNHotspotBaseline
	boot, err := d.read("/proc/sys/kernel/random/boot_id")
	if err != nil || !nativeAPNBootPattern.MatchString(strings.TrimSpace(string(boot))) {
		return out, errors.New("APN_HOTSPOT_BOOT_UNVERIFIED")
	}
	out.BootID = strings.TrimSpace(string(boot))
	raw, err := d.read("/sys/class/net/nccap0/ifindex")
	if err != nil {
		return out, errors.New("APN_HOTSPOT_INTERFACE_UNAVAILABLE")
	}
	out.IfIndex, err = strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || out.IfIndex <= 0 {
		return out, errors.New("APN_HOTSPOT_INTERFACE_UNVERIFIED")
	}
	raw, err = d.read("/sys/class/net/nccap0/flags")
	flags, parseErr := strconv.ParseUint(strings.TrimSpace(string(raw)), 0, 32)
	if err != nil || parseErr != nil || flags&1 == 0 {
		return out, errors.New("APN_HOTSPOT_INTERFACE_DOWN")
	}
	raw, err = d.read("/run/np1-control-ap/status.json")
	var state struct {
		Enabled   bool      `json:"enabled"`
		DHCPReady bool      `json:"dhcpReady"`
		State     string    `json:"state"`
		UpdatedAt time.Time `json:"updatedAt"`
	}
	now := d.now()
	if err != nil || len(raw) > 65536 || json.Unmarshal(raw, &state) != nil || !state.Enabled || !state.DHCPReady || state.State != "enabled" || state.UpdatedAt.IsZero() || state.UpdatedAt.After(now.Add(time.Second)) || now.Sub(state.UpdatedAt) > 7*time.Second {
		return out, errors.New("APN_HOTSPOT_WORKER_UNVERIFIED")
	}
	ownership, err := d.read("/run/np1-control-ap/private/ownership.json")
	var owner struct {
		IfIndex int    `json:"ifIndex"`
		BootID  string `json:"bootId"`
		Hostapd struct {
			PID   int    `json:"pid"`
			Start string `json:"start"`
		} `json:"hostapd"`
		DHCP struct {
			PID   int    `json:"pid"`
			Start string `json:"start"`
		} `json:"dhcp"`
	}
	if err != nil || len(ownership) > 32768 || json.Unmarshal(ownership, &owner) != nil || owner.IfIndex != out.IfIndex || owner.BootID != out.BootID || owner.Hostapd.PID <= 1 || owner.DHCP.PID <= 1 || owner.Hostapd.Start == "" || owner.DHCP.Start == "" {
		return out, errors.New("APN_HOTSPOT_OWNER_UNVERIFIED")
	}
	for _, process := range []struct {
		pid   int
		start string
	}{{owner.Hostapd.PID, owner.Hostapd.Start}, {owner.DHCP.PID, owner.DHCP.Start}} {
		raw, processError := d.read("/proc/" + strconv.Itoa(process.pid) + "/stat")
		if processError != nil || !nativeAPNProcessMatches(raw, process.pid, process.start) {
			return out, errors.New("APN_HOTSPOT_PROCESS_CHANGED")
		}
	}
	hash := sha256.Sum256(ownership)
	out.OwnershipHash = hex.EncodeToString(hash[:])
	out.Service, err = d.command(ctx, "/bin/systemctl", "show", "np1-control-ap.service", "-p", "Id", "-p", "MainPID", "-p", "InvocationID", "-p", "ActiveState", "-p", "SubState", "-p", "Job")
	if err != nil || !nativeAPNServiceReady(out.Service) {
		return out, errors.New("APN_HOTSPOT_SERVICE_UNVERIFIED")
	}
	address, err := d.command(ctx, "/sbin/ip", "-o", "-4", "address", "show", "dev", "nccap0")
	if err != nil {
		return out, errors.New("APN_HOTSPOT_ADDRESS_UNAVAILABLE")
	}
	prefix, err := nativeAPNHotspotAddress(address)
	if err != nil {
		return out, err
	}
	out.AddressPrefix = prefix.String()
	if err = d.http(ctx, prefix.Addr().String()); err != nil {
		return out, errors.New("APN_HOTSPOT_HTTP_UNAVAILABLE")
	}
	return out, nil
}

func nativeAPNManagementReady(before nativeAPNNetworkBaseline) bool {
	if before.hotspot != nil {
		value := before.hotspot
		prefix, err := netip.ParsePrefix(value.AddressPrefix)
		return err == nil && prefix.Addr().Is4() && prefix.Addr().IsGlobalUnicast() && prefix.Bits() >= 8 && prefix.Bits() <= 30 && value.IfIndex > 0 && nativeAPNHex64.MatchString(value.OwnershipHash) && nativeAPNBootPattern.MatchString(value.BootID) && nativeAPNServiceReady(value.Service)
	}
	prefix, err := netip.ParsePrefix(before.prefix)
	return err == nil && prefix.Addr().Is4() && prefix.Addr().IsGlobalUnicast() && before.wifi.state == "COMPLETED" && before.wifi.ssid != "" && prefix.Addr().String() == before.wifi.address
}

func nativeAPNManagementKind(before nativeAPNNetworkBaseline) string {
	if before.hotspot != nil {
		if before.wifi.state == "COMPLETED" {
			return "wifi-sta+hotspot"
		}
		return "hotspot"
	}
	return "wifi-sta"
}
