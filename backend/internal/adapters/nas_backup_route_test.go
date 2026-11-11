package adapters

import (
	"context"
	"errors"
	"github.com/np1-control-center/ncc/internal/nasbackup"
	"net"
	"net/netip"
	"testing"
)

func nasRouteFixture() nasRouteIdentity {
	return nasRouteIdentity{iface: "wlan0", index: 4, source: netip.MustParseAddr("192.0.2.10"), ssid: "Fixture LAN", id: "4", bssid: "fixture-bss", boot: "fixture-boot", dns: []netip.Addr{netip.MustParseAddr("192.0.2.53")}}
}
func TestNASBindingPinsInterfaceSourceDestinationAndSSID(t *testing.T) {
	id := nasRouteFixture()
	calls := 0
	target := nasbackup.TargetView{Host: "nas.example.test", Port: 445}
	remoteIP := netip.MustParseAddr("192.0.2.20")
	d := nasRouteDependencies{snapshot: func(_ context.Context, rule nasRouteRule, address netip.Addr) (nasRouteIdentity, error) {
		if rule.route != "wifi" || rule.timing != "ssid" || rule.ssid != "Fixture LAN" {
			t.Fatal("policy condition lost")
		}
		return id, nil
	}, resolve: func(_ context.Context, current nasRouteIdentity, host string) ([]netip.Addr, error) {
		if current.iface != "wlan0" || host != target.Host {
			t.Fatal("DNS identity changed")
		}
		return []netip.Addr{remoteIP}, nil
	}, dial: func(_ context.Context, iface string, source netip.Addr, endpoint netip.AddrPort) (net.Conn, error) {
		calls++
		if iface != "wlan0" || source != id.source || endpoint != netip.AddrPortFrom(remoteIP, 445) {
			t.Fatal("socket not pinned")
		}
		client, server := net.Pipe()
		server.Close()
		return cellularProbeConnection{client, net.TCPAddrFromAddrPort(netip.AddrPortFrom(source, 1234)), net.TCPAddrFromAddrPort(endpoint)}, nil
	}}
	binding, err := prepareNASRouteWith(context.Background(), nasRouteRule{route: "wifi", timing: "ssid", ssid: "Fixture LAN"}, target, d)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := binding.transport().DialContext(context.Background(), "tcp", "nas.example.test:445")
	if err != nil {
		t.Fatal(err)
	}
	connection.Close()
	if calls != 1 {
		t.Fatal("unexpected dial count")
	}
	if _, err = binding.transport().DialContext(context.Background(), "tcp", "other.example.test:445"); err == nil || calls != 1 {
		t.Fatal("endpoint fallback allowed")
	}
	id.ssid = "Different LAN"
	if _, err = binding.transport().DialContext(context.Background(), "tcp", "nas.example.test:445"); err == nil || calls != 1 {
		t.Fatal("SSID change used old connection proof")
	}
}
func TestNASBindingRejectsMissingOrChangingProofWithoutOtherRouteFallback(t *testing.T) {
	for _, mode := range []string{"empty", "ifindex", "source", "boot", "ssid", "gateway", "loopback-dns"} {
		t.Run(mode, func(t *testing.T) {
			id := nasRouteFixture()
			snapshots := 0
			d := nasRouteDependencies{snapshot: func(_ context.Context, _ nasRouteRule, target netip.Addr) (nasRouteIdentity, error) {
				snapshots++
				if mode == "empty" {
					return nasRouteIdentity{}, nil
				}
				if snapshots > 1 {
					switch mode {
					case "ifindex":
						id.index++
					case "source":
						id.source = netip.MustParseAddr("192.0.2.11")
					case "boot":
						id.boot = "new-boot"
					case "ssid":
						id.ssid = "other"
					case "gateway":
						if snapshots > 2 {
							id.gateway = "192.0.2.254"
						}
					}
				}
				return id, nil
			}, resolve: func(context.Context, nasRouteIdentity, string) ([]netip.Addr, error) {
				if mode == "loopback-dns" {
					return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
				}
				return []netip.Addr{netip.MustParseAddr("192.0.2.20")}, nil
			}, dial: func(context.Context, string, netip.Addr, netip.AddrPort) (net.Conn, error) {
				return nil, errors.New("must not dial")
			}}
			binding, err := prepareNASRouteWith(context.Background(), nasRouteRule{route: "wifi", timing: "realtime"}, nasbackup.TargetView{Host: "nas.example.test", Port: 445}, d)
			if mode == "gateway" && err == nil {
				err = binding.check(context.Background())
			}
			if err == nil {
				t.Fatal("unverified/changing proof accepted")
			}
		})
	}
}
func TestNASTailnetProofCannotBecomeWiFi(t *testing.T) {
	id := nasRouteFixture()
	if validNASRouteIdentity(nasRouteRule{route: "tailscale"}, id) {
		t.Fatal("wifi proof accepted as tailnet")
	}
	id.iface = "tailscale0"
	id.source = netip.MustParseAddr("100.64.1.2")
	if !validNASRouteIdentity(nasRouteRule{route: "tailscale"}, id) {
		t.Fatal("tailnet proof rejected")
	}
}
