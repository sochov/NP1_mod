package adapters

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"reflect"
	"time"
)

const cellularProbeHost = "example.com"

// CellularInternetProbeResult describes NP1-origin traffic only. It does not
// claim that an AP client, forwarding or NAT has been tested.
type CellularInternetProbeResult struct {
	Status            string    `json:"status"`
	Source            string    `json:"source"`
	Interface         string    `json:"interface,omitempty"`
	DNSVerified       bool      `json:"dnsVerified"`
	TLSVerified       bool      `json:"tlsVerified"`
	HTTPVerified      bool      `json:"httpVerified"`
	IdentityUnchanged bool      `json:"identityUnchanged"`
	StartedAt         time.Time `json:"startedAt"`
	ObservedAt        time.Time `json:"observedAt"`
}

type cellularProbeDependencies struct {
	snapshot func(context.Context) (apDNSIdentity, error)
	resolve  func(context.Context, apDNSIdentity) ([]netip.Addr, error)
	dial     func(context.Context, string, netip.Addr, netip.AddrPort) (net.Conn, error)
	roots    func() (*x509.CertPool, error)
	now      func() time.Time
}

// ProbeCellularInternet has a fixed destination, an eight-second total budget,
// no network configuration writes, and no certificate-verification bypass.
// A caller must separately arrange any authorized native STA roundtrip.
func ProbeCellularInternet(ctx context.Context) (CellularInternetProbeResult, error) {
	return runCellularInternetProbe(ctx, cellularProbeDependencies{
		snapshot: apCellularDNSSnapshot,
		resolve:  resolveCellularProbe,
		dial:     cellularProbeDialTCP,
		roots:    x509.SystemCertPool,
		now:      time.Now,
	})
}

func runCellularInternetProbe(parent context.Context, d cellularProbeDependencies) (CellularInternetProbeResult, error) {
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	result := CellularInternetProbeResult{Status: "failed", Source: "np1-origin-bound-cellular", StartedAt: d.now()}
	fail := func(code string) (CellularInternetProbeResult, error) {
		result.ObservedAt = d.now()
		return result, errors.New(code)
	}
	before, err := d.snapshot(ctx)
	if err != nil || !validCellularProbeIdentity(before) {
		return fail("CELLULAR_PROBE_IDENTITY_UNAVAILABLE")
	}
	result.Interface = before.iface
	addresses, err := d.resolve(ctx, before)
	if err != nil || len(addresses) == 0 || len(addresses) > 16 {
		return fail("CELLULAR_PROBE_DNS_FAILED")
	}
	for _, address := range addresses {
		if !cellularProbePublicIPv4(address) {
			return fail("CELLULAR_PROBE_DNS_INVALID")
		}
	}
	result.DNSVerified = true
	roots, err := d.roots()
	if err != nil || roots == nil {
		return fail("CELLULAR_PROBE_CA_UNAVAILABLE")
	}
	// One address is used; a failed path cannot silently fall back to a different
	// interface, source address or unbound system dialer.
	target := netip.AddrPortFrom(addresses[0], 443)
	connection, err := d.dial(ctx, before.iface, before.address.Addr(), target)
	if err != nil {
		return fail("CELLULAR_PROBE_TCP_FAILED")
	}
	defer connection.Close()
	stopCancelClose := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stopCancelClose()
	local, localErr := netip.ParseAddrPort(connection.LocalAddr().String())
	remote, remoteErr := netip.ParseAddrPort(connection.RemoteAddr().String())
	if localErr != nil || remoteErr != nil || local.Addr().Unmap() != before.address.Addr() || remote != target {
		return fail("CELLULAR_PROBE_SOCKET_IDENTITY_CHANGED")
	}
	deadline, _ := ctx.Deadline()
	if connection.SetDeadline(deadline) != nil {
		return fail("CELLULAR_PROBE_DEADLINE_FAILED")
	}
	secure := tls.Client(connection, &tls.Config{ServerName: cellularProbeHost, RootCAs: roots, MinVersion: tls.VersionTLS12})
	if secure.HandshakeContext(ctx) != nil {
		return fail("CELLULAR_PROBE_TLS_FAILED")
	}
	result.TLSVerified = true
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+cellularProbeHost+"/", nil)
	request.Header.Set("User-Agent", "NP1-Control-Probe/1")
	request.Close = true
	if request.Write(secure) != nil {
		return fail("CELLULAR_PROBE_HTTP_FAILED")
	}
	response, err := http.ReadResponse(bufio.NewReader(io.LimitReader(secure, 32768)), request)
	if err != nil {
		return fail("CELLULAR_PROBE_HTTP_FAILED")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fail("CELLULAR_PROBE_HTTP_STATUS")
	}
	result.HTTPVerified = true
	after, err := d.snapshot(ctx)
	if err != nil || ctx.Err() != nil || !reflect.DeepEqual(before, after) {
		return fail("CELLULAR_PROBE_IDENTITY_CHANGED")
	}
	result.Status, result.IdentityUnchanged, result.ObservedAt = "passed", true, d.now()
	return result, nil
}

func validCellularProbeIdentity(id apDNSIdentity) bool {
	if (id.iface != "usb0" && id.iface != "usb0.1" && id.iface != "usb0.2") || id.index <= 0 || id.boot == "" || !id.address.IsValid() || !id.address.Addr().Is4() || !id.address.Addr().IsGlobalUnicast() || len(id.servers) == 0 || len(id.servers) > 4 {
		return false
	}
	for _, server := range id.servers {
		if !server.Is4() || !server.IsGlobalUnicast() {
			return false
		}
	}
	return true
}

func cellularProbePublicIPv4(address netip.Addr) bool {
	if !address.Is4() || !address.IsGlobalUnicast() || address.IsPrivate() {
		return false
	}
	// Reject special-purpose/documentation ranges as well as RFC1918. DNS
	// rebinding to a local management endpoint must never become a probe.
	for _, prefix := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4"} {
		if netip.MustParsePrefix(prefix).Contains(address) {
			return false
		}
	}
	return true
}

func resolveCellularProbe(ctx context.Context, id apDNSIdentity) ([]netip.Addr, error) {
	return resolveCellularProbeWithDial(ctx, id, func(ctx context.Context, iface string, source netip.Addr, server netip.AddrPort) (net.Conn, error) {
		return apDialBoundDNS(ctx, iface, source, server)
	})
}

func resolveCellularProbeWithDial(ctx context.Context, id apDNSIdentity, dial func(context.Context, string, netip.Addr, netip.AddrPort) (net.Conn, error)) ([]netip.Addr, error) {
	// Each candidate comes from the pinned Linux snapshot, and the DNS socket
	// itself is bound to the verified interface and source. The Go resolver
	// validates transaction IDs, questions and DNS records.
	var last error
	for _, server := range id.servers {
		resolver := &net.Resolver{PreferGo: true, StrictErrors: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			if network != "udp" && network != "udp4" {
				return nil, errors.New("CELLULAR_PROBE_DNS_TRANSPORT_UNSUPPORTED")
			}
			return dial(ctx, id.iface, id.address.Addr(), netip.AddrPortFrom(server, 53))
		}}
		addresses, err := resolver.LookupNetIP(ctx, "ip4", cellularProbeHost+".")
		if err == nil && len(addresses) > 0 {
			return addresses, nil
		}
		last = err
		if ctx.Err() != nil {
			break
		}
	}
	return nil, last
}
