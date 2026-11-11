package adapters

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func dnsFixtureAnswer(query []byte) []byte {
	response := append([]byte(nil), query...)
	response[2] = 0x81
	response[3] = 0x80
	binary.BigEndian.PutUint16(response[6:8], 1)
	return append(response, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 30, 0, 4, 93, 184, 216, 34)
}
func dnsFixtureQuery() []byte {
	return []byte{0x12, 0x34, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0, 0, 1, 0, 1}
}
func TestAPSharingDNSAnswerValidation(t *testing.T) {
	valid := dnsFixtureAnswer(dnsFixtureQuery())
	if !apDNSAResponse(valid, 0x1234) {
		t.Fatal("valid compressed A rejected")
	}
	cases := map[string]func([]byte) []byte{
		"id": func(v []byte) []byte { v[0] ^= 1; return v }, "rcode": func(v []byte) []byte { v[3] = 0x83; return v }, "truncated": func(v []byte) []byte { v[2] |= 2; return v },
		"empty": func(v []byte) []byte { v[6], v[7] = 0, 0; return v[:29] }, "question": func(v []byte) []byte { v[13] = 'z'; return v },
		"private-a":     func(v []byte) []byte { copy(v[len(v)-4:], []byte{192, 168, 1, 1}); return v },
		"pointer-cycle": func(v []byte) []byte { v[12], v[13] = 0xc0, 0x0c; return v },
		"short-rdata":   func(v []byte) []byte { return v[:len(v)-1] },
		"unrelated-a":   func(v []byte) []byte { return append(append(v[:29], 3, 'b', 'a', 'd', 0), v[31:]...) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			if apDNSAResponse(mutate(append([]byte(nil), valid...)), 0x1234) {
				t.Fatal("invalid DNS response accepted")
			}
		})
	}
	// A record after a CNAME must belong to that exact chain, not just any answer.
	cname := append([]byte(nil), valid[:29]...)
	binary.BigEndian.PutUint16(cname[6:8], 2)
	cname = append(cname, 0xc0, 0x0c, 0, 5, 0, 1, 0, 0, 0, 30, 0, 9, 6, 't', 'a', 'r', 'g', 'e', 't', 0)
	// Correct RDLENGTH for "target." is eight bytes.
	cname[39], cname[40] = 0, 8
	cname = append(cname, 6, 't', 'a', 'r', 'g', 'e', 't', 0, 0, 1, 0, 1, 0, 0, 0, 30, 0, 4, 93, 184, 216, 34)
	if !apDNSAResponse(cname, 0x1234) {
		t.Fatal("valid CNAME/A chain rejected")
	}
}

func TestAPSharingDNSUDPFixture(t *testing.T) {
	for _, mode := range []string{"valid", "wrong-id", "empty-a", "wrong-peer", "timeout", "wrong-local"} {
		t.Run(mode, func(t *testing.T) {
			server, e := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if e != nil {
				t.Fatal(e)
			}
			defer server.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				buffer := make([]byte, 512)
				n, peer, e := server.ReadFromUDP(buffer)
				if e != nil {
					return
				}
				answer := dnsFixtureAnswer(buffer[:n])
				switch mode {
				case "wrong-id":
					answer[0] ^= 1
				case "empty-a":
					answer = answer[:29]
					answer[6], answer[7] = 0, 0
				case "timeout":
					return
				case "wrong-peer":
					other, e := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
					if e == nil {
						defer other.Close()
						_, _ = other.WriteToUDP(answer, peer)
					}
					return
				}
				_, _ = server.WriteToUDP(answer, peer)
			}()
			dial := func(_ context.Context, _ string, _ netip.Addr, remote netip.AddrPort) (*net.UDPConn, error) {
				return net.DialUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}, net.UDPAddrFromAddrPort(remote))
			}
			ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
			defer cancel()
			source := netip.MustParseAddr("127.0.0.1")
			if mode == "wrong-local" {
				source = netip.MustParseAddr("127.0.0.2")
			}
			e = apBoundDNSQuery(ctx, dial, "fixture0", source, server.LocalAddr().(*net.UDPAddr).AddrPort())
			if (e == nil) != (mode == "valid") {
				t.Fatalf("mode=%s result=%v", mode, e)
			}
			_ = server.Close()
			<-done
		})
	}
}

func dnsCacheFixture() (apDNSIdentity, apCellularRouteProof) {
	id := apDNSIdentity{iface: "usb0.1", boot: "boot", index: 7, address: netip.MustParsePrefix("10.7.0.2/24"), gateway: "10.7.0.1", servers: []netip.Addr{netip.MustParseAddr("9.9.9.9")}}
	return id, apCellularRouteProof{iface: id.iface, address: id.address, gateway: id.gateway}
}
func TestAPSharingDNSCacheAndIdentityInvalidation(t *testing.T) {
	id, proof := dnsCacheFixture()
	now := time.Now()
	var probes atomic.Int32
	verifier := apDNSVerifier{d: apDNSDependencies{snapshot: func(context.Context) (apDNSIdentity, error) { return id, nil }, probe: func(context.Context, apDNSIdentity, netip.Addr) error { probes.Add(1); return nil }, now: func() time.Time { return now }}}
	for i := 0; i < 4; i++ {
		dns, e := verifier.verify(context.Background(), proof)
		if e != nil || len(dns) != 1 {
			t.Fatal("valid cache proof rejected", e)
		}
	}
	if probes.Load() != 1 {
		t.Fatal("successful proof was reprobed during cache lifetime")
	}
	for _, change := range []func(){func() { id.index++ }, func() { id.boot = "reboot" }, func() { id.servers = []netip.Addr{netip.MustParseAddr("8.8.8.8")} }} {
		before := probes.Load()
		change()
		if _, e := verifier.verify(context.Background(), proof); e != nil {
			t.Fatal(e)
		}
		if probes.Load() != before+1 {
			t.Fatal("identity change did not invalidate cache")
		}
	}
	now = now.Add(61 * time.Second)
	before := probes.Load()
	if _, e := verifier.verify(context.Background(), proof); e != nil {
		t.Fatal(e)
	}
	if probes.Load() != before+1 {
		t.Fatal("expired cache reused")
	}
	id.gateway = "10.7.0.3"
	if _, e := verifier.verify(context.Background(), proof); e == nil {
		t.Fatal("route mismatch accepted")
	}
	id.gateway = proof.gateway
	id.address = netip.MustParsePrefix("10.7.0.4/24")
	if _, e := verifier.verify(context.Background(), proof); e == nil {
		t.Fatal("address mismatch accepted")
	}
}
func TestAPSharingDNSChangedDuringProbeIsNotCached(t *testing.T) {
	id, proof := dnsCacheFixture()
	calls := 0
	verifier := apDNSVerifier{d: apDNSDependencies{snapshot: func(context.Context) (apDNSIdentity, error) {
		calls++
		if calls == 2 {
			id.index++
		}
		return id, nil
	}, probe: func(context.Context, apDNSIdentity, netip.Addr) error { return nil }, now: time.Now}}
	if _, e := verifier.verify(context.Background(), proof); e == nil || !strings.Contains(e.Error(), "IDENTITY_CHANGED") {
		t.Fatal("changed interface proof accepted", e)
	}
	if verifier.identity != nil || len(verifier.verified) != 0 {
		t.Fatal("changed proof entered cache")
	}
}
func TestAPSharingDNSFailureBackoffAndRecovery(t *testing.T) {
	id, proof := dnsCacheFixture()
	now := time.Now()
	var probes atomic.Int32
	fail := true
	verifier := apDNSVerifier{d: apDNSDependencies{snapshot: func(context.Context) (apDNSIdentity, error) { return id, nil }, probe: func(context.Context, apDNSIdentity, netip.Addr) error {
		probes.Add(1)
		if fail {
			return errors.New("fixture")
		}
		return nil
	}, now: func() time.Time { return now }}}
	for i := 0; i < 3; i++ {
		if _, e := verifier.verify(context.Background(), proof); e == nil {
			t.Fatal("failed query accepted")
		}
	}
	if probes.Load() != 1 {
		t.Fatal("failed upstream probed every collector tick")
	}
	now = now.Add(6 * time.Second)
	fail = false
	if _, e := verifier.verify(context.Background(), proof); e != nil {
		t.Fatal("DNS did not recover after bounded backoff", e)
	}
}

func TestAPSharingDNSExplicitSourceRoute(t *testing.T) {
	if _, e := apParseUpstreamRoute("9.9.9.9 from 10.7.0.2 via 10.7.0.1 dev usb0.1 uid 0\n cache", "usb0.1", "10.7.0.2"); e != nil {
		t.Fatal("explicit route source not recognized", e)
	}
	for _, raw := range []string{"9.9.9.9 from 10.7.0.3 dev usb0.1", "9.9.9.9 from 10.7.0.2 dev wlan0", "9.9.9.9 from 10.7.0.2 dev usb0.1 src 10.7.0.3"} {
		if _, e := apParseUpstreamRoute(raw, "usb0.1", "10.7.0.2"); e == nil {
			t.Fatal("contradictory DNS path proof accepted")
		}
	}
}
