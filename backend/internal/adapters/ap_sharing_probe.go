package adapters

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/netip"
	"os"
	"strings"
	"time"
)

func apSharingWPAStatus(ctx context.Context) (string, error) {
	var token [12]byte
	if _, e := rand.Read(token[:]); e != nil {
		return "", errors.New("AP_UPSTREAM_STATUS_UNAVAILABLE")
	}
	connection, e := net.DialUnix("unixgram", &net.UnixAddr{Name: "@ncc-ap-uplink-" + hex.EncodeToString(token[:]), Net: "unixgram"}, &net.UnixAddr{Name: "/run/wifi/wpa_supplicant/wlan0", Net: "unixgram"})
	if e != nil {
		return "", errors.New("AP_UPSTREAM_STATUS_UNAVAILABLE")
	}
	defer connection.Close()
	deadline := time.Now().Add(2 * time.Second)
	if earlier, ok := ctx.Deadline(); ok && earlier.Before(deadline) {
		deadline = earlier
	}
	_ = connection.SetDeadline(deadline)
	if _, e = connection.Write([]byte("STATUS")); e != nil {
		return "", errors.New("AP_UPSTREAM_STATUS_UNAVAILABLE")
	}
	raw := make([]byte, 16385)
	n, e := connection.Read(raw)
	if e != nil || n > 16384 {
		return "", errors.New("AP_UPSTREAM_STATUS_UNAVAILABLE")
	}
	return string(raw[:n]), nil
}
func apSharingStatusFields(raw string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			out[key] = value
		}
	}
	return out
}
func apParseUpstreamRoute(raw, iface, source string) (string, error) {
	fail := func() (string, error) { return "", errors.New("AP_UPSTREAM_ROUTE_UNVERIFIED") }
	line := strings.Split(strings.TrimSpace(raw), "\n")[0]
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return fail()
	}
	if fields[0] == "unreachable" || fields[0] == "blackhole" || fields[0] == "prohibit" || fields[0] == "local" {
		return fail()
	}
	dev, src, gateway := "", "", ""
	for i := 0; i < len(fields)-1; i++ {
		switch fields[i] {
		case "dev":
			dev = fields[i+1]
		case "src":
			src = fields[i+1]
		case "via":
			gateway = fields[i+1]
		}
	}
	if dev != iface || src != source {
		return fail()
	}
	if gateway != "" {
		ip, e := netip.ParseAddr(gateway)
		if e != nil || !ip.Is4() || !ip.IsGlobalUnicast() {
			return fail()
		}
	}
	return gateway, nil
}
func apResolverIPv4(raw string) []string {
	out := []string{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		ip, e := netip.ParseAddr(fields[1])
		if e != nil || !ip.Is4() || !ip.IsGlobalUnicast() || ip.String() == apSharingGateway {
			continue
		}
		duplicate := false
		for _, previous := range out {
			duplicate = duplicate || previous == ip.String()
		}
		if !duplicate {
			out = append(out, ip.String())
		}
		if len(out) == 4 {
			break
		}
	}
	return out
}
func readVerifiedSTAUpstream(ctx context.Context) (APSharingUpstream, error) {
	statusRaw, e := apSharingWPAStatus(ctx)
	if e != nil {
		return APSharingUpstream{}, e
	}
	status := apSharingStatusFields(statusRaw)
	if status["wpa_state"] != "COMPLETED" {
		return APSharingUpstream{}, errors.New("AP_STA_NOT_CONNECTED")
	}
	iface, e := net.InterfaceByName("wlan0")
	if e != nil || iface.Flags&net.FlagUp == 0 {
		return APSharingUpstream{}, errors.New("AP_STA_NOT_CONNECTED")
	}
	addresses, e := iface.Addrs()
	if e != nil {
		return APSharingUpstream{}, errors.New("AP_STA_ADDRESS_UNAVAILABLE")
	}
	var address netip.Addr
	bits := 0
	for _, candidate := range addresses {
		prefix, e := netip.ParsePrefix(candidate.String())
		if e == nil && prefix.Addr().Is4() && prefix.Addr().IsGlobalUnicast() && (status["ip_address"] == "" || prefix.Addr().String() == status["ip_address"]) {
			if address.IsValid() {
				return APSharingUpstream{}, errors.New("AP_STA_ADDRESS_AMBIGUOUS")
			}
			address = prefix.Addr()
			bits = prefix.Bits()
		}
	}
	if !address.IsValid() {
		return APSharingUpstream{}, errors.New("AP_STA_ADDRESS_UNAVAILABLE")
	}
	route, e := apRun(ctx, "ip", "-4", "route", "get", "1.1.1.1", "oif", "wlan0")
	if e != nil {
		return APSharingUpstream{}, errors.New("AP_UPSTREAM_ROUTE_UNVERIFIED")
	}
	gateway, e := apParseUpstreamRoute(route, "wlan0", address.String())
	if e != nil {
		return APSharingUpstream{}, e
	}
	resolver, e := os.ReadFile("/etc/resolv.conf")
	if e != nil || len(resolver) > 16384 {
		return APSharingUpstream{}, errors.New("AP_UPSTREAM_DNS_UNVERIFIED")
	}
	dns := []string{}
	for _, server := range apResolverIPv4(string(resolver)) {
		if server == address.String() {
			continue
		}
		proof, routeError := apRun(ctx, "ip", "-4", "route", "get", server, "oif", "wlan0")
		if routeError == nil {
			if _, routeError = apParseUpstreamRoute(proof, "wlan0", address.String()); routeError == nil {
				dns = append(dns, server)
			}
		}
	}
	after, e := net.InterfaceByName("wlan0")
	if e != nil || after.Index != iface.Index || after.Flags&net.FlagUp == 0 {
		return APSharingUpstream{}, errors.New("AP_UPSTREAM_CHANGED")
	}
	statusRaw, e = apSharingWPAStatus(ctx)
	if e != nil {
		return APSharingUpstream{}, e
	}
	current := apSharingStatusFields(statusRaw)
	for _, key := range []string{"wpa_state", "id", "ssid", "bssid", "ip_address"} {
		if status[key] != current[key] {
			return APSharingUpstream{}, errors.New("AP_UPSTREAM_CHANGED")
		}
	}
	return newAPSharingUpstream("wifi", "wlan0", address.String(), bits, gateway, dns, "wpa-completed-linux-route-resolver", time.Now().UTC())
}
