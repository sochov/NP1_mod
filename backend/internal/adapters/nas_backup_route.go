package adapters

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/np1-control-center/ncc/internal/nasbackup"
)

type nasRouteRule struct {
	route, timing, ssid string
	resolveDNS          bool
}
type nasRouteIdentity struct {
	iface, ssid, id, bssid, boot, gateway, resolver string
	index                                           int
	source                                          netip.Addr
	dns                                             []netip.Addr
}

func (nasRouteIdentity) String() string     { return "NAS route identity [redacted]" }
func (v nasRouteIdentity) GoString() string { return v.String() }

type nasRouteDependencies struct {
	snapshot func(context.Context, nasRouteRule, netip.Addr) (nasRouteIdentity, error)
	resolve  func(context.Context, nasRouteIdentity, string) ([]netip.Addr, error)
	dial     func(context.Context, string, netip.Addr, netip.AddrPort) (net.Conn, error)
}
type nasRouteBinding struct {
	rule         nasRouteRule
	target       nasbackup.TargetView
	address      netip.Addr
	identity     nasRouteIdentity
	dependencies nasRouteDependencies
}

func (b *nasRouteBinding) String() string   { return "NAS bound route [redacted]" }
func (b *nasRouteBinding) GoString() string { return b.String() }
func prepareNASRoute(ctx context.Context, rule nasRouteRule, target nasbackup.TargetView) (*nasRouteBinding, error) {
	return prepareNASRouteWith(ctx, rule, target, nasRouteDependencies{readNASRouteIdentity, resolveNASRouteHost, cellularProbeDialTCP})
}
func prepareNASRouteWith(parent context.Context, rule nasRouteRule, target nasbackup.TargetView, d nasRouteDependencies) (*nasRouteBinding, error) {
	if (rule.route != "wifi" && rule.route != "tailscale") || target.Host == "" || target.Port < 1 || target.Port > 65535 {
		return nil, errors.New("NAS_ROUTE_PROOF_INVALID")
	}
	_, literalError := netip.ParseAddr(target.Host)
	rule.resolveDNS = literalError != nil
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	before, err := d.snapshot(ctx, rule, netip.Addr{})
	if err != nil {
		return nil, err
	}
	if !validNASRouteIdentity(rule, before) {
		return nil, errors.New("NAS_ROUTE_PROOF_INVALID")
	}
	var addresses []netip.Addr
	if address, e := netip.ParseAddr(target.Host); e == nil {
		addresses = []netip.Addr{address}
	} else {
		addresses, err = d.resolve(ctx, before, target.Host)
		if err != nil {
			return nil, errors.New("NAS_ROUTE_DNS_UNAVAILABLE")
		}
	}
	if len(addresses) == 0 || len(addresses) > 16 {
		return nil, errors.New("NAS_ROUTE_DNS_UNAVAILABLE")
	}
	for _, address := range addresses {
		if !address.Is4() || !address.IsGlobalUnicast() {
			continue
		}
		after, err := d.snapshot(ctx, rule, address)
		if err != nil {
			continue
		}
		if !validNASRouteIdentity(rule, after) {
			return nil, errors.New("NAS_ROUTE_PROOF_INVALID")
		}
		a, b := before, after
		a.gateway, b.gateway = "", ""
		if !reflect.DeepEqual(a, b) {
			return nil, errors.New("NAS_ROUTE_CHANGED")
		}
		return &nasRouteBinding{rule, target, address, after, d}, nil
	}
	return nil, errors.New("NAS_ROUTE_UNAVAILABLE")
}
func (b *nasRouteBinding) check(ctx context.Context) error {
	current, err := b.dependencies.snapshot(ctx, b.rule, b.address)
	if err != nil || !validNASRouteIdentity(b.rule, current) || !reflect.DeepEqual(current, b.identity) {
		return errors.New("NAS_ROUTE_CHANGED")
	}
	return nil
}
func validNASRouteIdentity(rule nasRouteRule, id nasRouteIdentity) bool {
	expected := "wlan0"
	if rule.route == "tailscale" {
		expected = "tailscale0"
	}
	if id.iface != expected || id.index <= 0 || !id.source.Is4() || !id.source.IsGlobalUnicast() || id.boot == "" {
		return false
	}
	if rule.route == "tailscale" && !netip.MustParsePrefix("100.64.0.0/10").Contains(id.source) {
		return false
	}
	if (rule.route == "wifi" || rule.timing == "ssid") && (id.ssid == "" || id.id == "") {
		return false
	}
	return rule.timing != "ssid" || id.ssid == rule.ssid
}
func (b *nasRouteBinding) transport() nasbackup.TransportOptions {
	return nasbackup.TransportOptions{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || !strings.EqualFold(host, b.target.Host) || port != strconv.Itoa(b.target.Port) || network != "tcp" && network != "tcp4" {
			return nil, errors.New("NAS_ROUTE_ENDPOINT_CHANGED")
		}
		if err = b.check(ctx); err != nil {
			return nil, err
		}
		target := netip.AddrPortFrom(b.address, uint16(b.target.Port))
		connection, err := b.dependencies.dial(ctx, b.identity.iface, b.identity.source, target)
		if err != nil {
			return nil, errors.New("NAS_BOUND_CONNECT_FAILED")
		}
		local, e1 := netip.ParseAddrPort(connection.LocalAddr().String())
		remote, e2 := netip.ParseAddrPort(connection.RemoteAddr().String())
		if e1 != nil || e2 != nil || local.Addr().Unmap() != b.identity.source || remote != target {
			connection.Close()
			return nil, errors.New("NAS_BOUND_SOCKET_CHANGED")
		}
		if err = b.check(ctx); err != nil {
			connection.Close()
			return nil, err
		}
		return connection, nil
	}}
}
func (b *nasRouteBinding) monitor(ctx context.Context, cancel context.CancelFunc, onError func(error)) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkCtx, done := context.WithTimeout(ctx, 3*time.Second)
			err := b.check(checkCtx)
			done()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				onError(err)
				cancel()
				return
			}
		}
	}
}
func readNASRouteIdentity(parent context.Context, rule nasRouteRule, target netip.Addr) (nasRouteIdentity, error) {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	if rule.route != "wifi" && rule.route != "tailscale" {
		return nasRouteIdentity{}, errors.New("NAS_ROUTE_INVALID")
	}
	id := nasRouteIdentity{iface: "wlan0"}
	if rule.route == "tailscale" {
		id.iface = "tailscale0"
	}
	statusFields := map[string]string{}
	if rule.route == "wifi" || rule.timing == "ssid" {
		raw, err := apSharingWPAStatus(ctx)
		if err != nil {
			return id, errors.New("NAS_WIFI_NOT_CONNECTED")
		}
		status, err := parseWiFiStatus(raw)
		if err != nil || status.State != "COMPLETED" {
			return id, errors.New("NAS_WIFI_NOT_CONNECTED")
		}
		if rule.timing == "ssid" && status.SSID != rule.ssid {
			return id, errors.New("NAS_SSID_NOT_CONNECTED")
		}
		statusFields = apSharingStatusFields(raw)
		id.ssid, id.id, id.bssid = status.SSID, statusFields["id"], statusFields["bssid"]
	}
	iface, err := net.InterfaceByName(id.iface)
	if err != nil || iface.Flags&net.FlagUp == 0 {
		return id, errors.New("NAS_ROUTE_NOT_CONNECTED")
	}
	id.index = iface.Index
	addresses, err := iface.Addrs()
	if err != nil {
		return id, errors.New("NAS_ROUTE_ADDRESS_UNAVAILABLE")
	}
	for _, raw := range addresses {
		prefix, err := netip.ParsePrefix(raw.String())
		if err != nil || !prefix.Addr().Is4() || !prefix.Addr().IsGlobalUnicast() {
			continue
		}
		if rule.route == "wifi" && statusFields["ip_address"] != "" && prefix.Addr().String() != statusFields["ip_address"] {
			continue
		}
		if rule.route == "tailscale" && !netip.MustParsePrefix("100.64.0.0/10").Contains(prefix.Addr()) {
			continue
		}
		if id.source.IsValid() {
			return id, errors.New("NAS_ROUTE_ADDRESS_AMBIGUOUS")
		}
		id.source = prefix.Addr()
	}
	if !id.source.IsValid() {
		return id, errors.New("NAS_ROUTE_ADDRESS_UNAVAILABLE")
	}
	id.boot = strings.TrimSpace(read("/proc/sys/kernel/random/boot_id"))
	if id.boot == "" {
		return id, errors.New("NAS_ROUTE_BOOT_UNAVAILABLE")
	}
	if target.IsValid() {
		raw, err := apCellularIPCommand(ctx, "-4", "route", "get", target.String(), "oif", id.iface, "from", id.source.String())
		if err != nil {
			return id, errors.New("NAS_ROUTE_UNAVAILABLE")
		}
		id.gateway, err = apParseUpstreamRoute(raw, id.iface, id.source.String())
		if err != nil {
			return id, errors.New("NAS_ROUTE_UNAVAILABLE")
		}
	}
	if !rule.resolveDNS {
		return id, nil
	}
	if rule.route == "tailscale" {
		id.dns = []netip.Addr{netip.MustParseAddr("100.100.100.100")}
	} else {
		raw, err := os.ReadFile("/etc/resolv.conf")
		if err != nil || len(raw) > 16384 {
			return id, errors.New("NAS_ROUTE_DNS_UNAVAILABLE")
		}
		id.resolver = string(raw)
		for _, server := range apResolverIPv4(string(raw)) {
			address, _ := netip.ParseAddr(server)
			id.dns = append(id.dns, address)
		}
	}
	return id, nil
}
func resolveNASRouteHost(ctx context.Context, id nasRouteIdentity, host string) ([]netip.Addr, error) {
	for _, server := range id.dns {
		raw, err := apCellularIPCommand(ctx, "-4", "route", "get", server.String(), "oif", id.iface, "from", id.source.String())
		if err != nil {
			continue
		}
		if _, err = apParseUpstreamRoute(raw, id.iface, id.source.String()); err != nil {
			continue
		}
		resolver := &net.Resolver{PreferGo: true, StrictErrors: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			if network == "udp" || network == "udp4" {
				return apDialBoundDNS(ctx, id.iface, id.source, netip.AddrPortFrom(server, 53))
			}
			if network == "tcp" || network == "tcp4" {
				return cellularProbeDialTCP(ctx, id.iface, id.source, netip.AddrPortFrom(server, 53))
			}
			return nil, errors.New("NAS_ROUTE_DNS_TRANSPORT_INVALID")
		}}
		addresses, err := resolver.LookupNetIP(ctx, "ip4", strings.TrimSuffix(host, ".")+".")
		if err == nil && len(addresses) > 0 {
			return addresses, nil
		}
	}
	return nil, errors.New("NAS_ROUTE_DNS_UNAVAILABLE")
}
