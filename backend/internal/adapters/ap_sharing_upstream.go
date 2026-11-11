package adapters

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"time"
)

// APSharingUpstream carries private route/DNS proof. Formatting and public JSON
// deliberately omit individual addresses; only the controller builds commands.
type APSharingUpstream struct {
	kind, iface, source string
	address             netip.Addr
	prefix              netip.Prefix
	gateway             netip.Addr
	dns                 []netip.Addr
	observedAt          time.Time
}
type APSharingUpstreamView struct {
	Kind       string    `json:"kind"`
	Interface  string    `json:"interface"`
	Source     string    `json:"source"`
	ObservedAt time.Time `json:"observedAt"`
}

func (u APSharingUpstream) public() APSharingUpstreamView {
	return APSharingUpstreamView{u.kind, u.iface, u.source, u.observedAt}
}
func (u APSharingUpstream) MarshalJSON() ([]byte, error) { return json.Marshal(u.public()) }
func (u APSharingUpstream) String() string {
	return fmt.Sprintf("AP upstream kind=%s interface=%s source=%s", u.kind, u.iface, u.source)
}
func (u APSharingUpstream) GoString() string { return u.String() }

var apSharingInterfaceName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,14}$`)

func newAPSharingUpstream(kind, ifname, ipv4 string, prefixBits int, gateway string, dns []string, source string, observedAt time.Time) (APSharingUpstream, error) {
	fail := func() (APSharingUpstream, error) { return APSharingUpstream{}, errors.New("AP_UPSTREAM_PROOF_INVALID") }
	if (kind != "cellular" && kind != "wifi") || !apSharingInterfaceName.MatchString(ifname) || ifname == apInterface || ifname == "lo" || ifname == "tailscale0" || kind == "wifi" && ifname != "wlan0" || prefixBits < 1 || prefixBits > 32 || observedAt.IsZero() || source == "" || len(source) > 128 {
		return fail()
	}
	address, e := netip.ParseAddr(ipv4)
	if e != nil || !address.Is4() || !address.IsGlobalUnicast() {
		return fail()
	}
	u := APSharingUpstream{kind: kind, iface: ifname, address: address, prefix: netip.PrefixFrom(address, prefixBits).Masked(), source: source, observedAt: observedAt}
	if gateway != "" {
		u.gateway, e = netip.ParseAddr(gateway)
		if e != nil || !u.gateway.Is4() || !u.gateway.IsGlobalUnicast() || !u.prefix.Contains(u.gateway) {
			return fail()
		}
	}
	if len(dns) == 0 {
		return APSharingUpstream{}, errors.New("AP_UPSTREAM_DNS_UNVERIFIED")
	}
	if len(dns) > 4 {
		return fail()
	}
	for _, value := range dns {
		address, e := netip.ParseAddr(value)
		if e != nil || !address.Is4() || !address.IsGlobalUnicast() || address.String() == "192.168.226.1" {
			return fail()
		}
		duplicate := false
		for _, previous := range u.dns {
			duplicate = duplicate || previous == address
		}
		if !duplicate {
			u.dns = append(u.dns, address)
		}
	}
	return u, nil
}
func (u APSharingUpstream) usable(now time.Time) bool {
	return (u.kind == "cellular" || u.kind == "wifi") && apSharingInterfaceName.MatchString(u.iface) && u.address.Is4() && u.prefix.IsValid() && len(u.dns) > 0 && !u.observedAt.IsZero() && !u.observedAt.After(now.Add(time.Second)) && now.Sub(u.observedAt) <= 10*time.Second
}
func chooseAPSharingUpstream(cellular, wifi APSharingUpstream, now time.Time) (APSharingUpstream, error) {
	if cellular.kind == "cellular" && cellular.usable(now) {
		return cellular, nil
	}
	if wifi.kind == "wifi" && wifi.usable(now) {
		return wifi, nil
	}
	return APSharingUpstream{}, errors.New("AP_UPSTREAM_UNAVAILABLE")
}
