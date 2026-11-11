package adapters

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"
)

type cellularProbeFixture struct {
	id             apDNSIdentity
	after          apDNSIdentity
	addresses      []netip.Addr
	certificate    tls.Certificate
	trust          *x509.CertPool
	status         string
	resolveError   error
	rootError      error
	wrongSocket    bool
	dials          int
	snapshots      int
	requestedHosts chan string
}

type cellularProbeConnection struct {
	net.Conn
	local, remote net.Addr
}

func (c cellularProbeConnection) LocalAddr() net.Addr  { return c.local }
func (c cellularProbeConnection) RemoteAddr() net.Addr { return c.remote }

func cellularProbeCertificate(t *testing.T, host string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: host}, DNSNames: []string{host}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, _ := x509.MarshalECPrivateKey(key)
	cert, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: private}))
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	parsed, _ := x509.ParseCertificate(der)
	pool.AddCert(parsed)
	return cert, pool
}

func newCellularProbeFixture(t *testing.T) *cellularProbeFixture {
	t.Helper()
	cert, trust := cellularProbeCertificate(t, cellularProbeHost)
	id := apDNSIdentity{iface: "usb0.1", index: 42, boot: "fixture-boot", address: netip.MustParsePrefix("192.0.2.9/24"), gateway: "192.0.2.1", servers: []netip.Addr{netip.MustParseAddr("192.0.2.53")}}
	return &cellularProbeFixture{id: id, after: id, addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}, certificate: cert, trust: trust, status: "200 OK", requestedHosts: make(chan string, 1)}
}

func (f *cellularProbeFixture) dependencies(t *testing.T) cellularProbeDependencies {
	t.Helper()
	return cellularProbeDependencies{
		snapshot: func(context.Context) (apDNSIdentity, error) {
			f.snapshots++
			if f.snapshots == 1 {
				return f.id, nil
			}
			return f.after, nil
		},
		resolve: func(_ context.Context, id apDNSIdentity) ([]netip.Addr, error) {
			if id.iface != "usb0.1" || id.address != f.id.address {
				t.Error("resolver identity changed")
			}
			return f.addresses, f.resolveError
		},
		roots: func() (*x509.CertPool, error) { return f.trust, f.rootError },
		now:   time.Now,
		dial: func(ctx context.Context, iface string, source netip.Addr, target netip.AddrPort) (net.Conn, error) {
			f.dials++
			if iface != f.id.iface || source != f.id.address.Addr() || target.Port() != 443 || target.Addr() != f.addresses[0] {
				t.Error("dial did not retain fixed source/interface/target")
			}
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				return nil, err
			}
			go func() {
				defer listener.Close()
				server, err := listener.Accept()
				if err != nil {
					return
				}
				defer server.Close()
				_ = server.SetDeadline(time.Now().Add(8 * time.Second))
				secure := tls.Server(server, &tls.Config{Certificates: []tls.Certificate{f.certificate}, MinVersion: tls.VersionTLS12, SessionTicketsDisabled: true})
				if secure.Handshake() != nil {
					return
				}
				request, err := http.ReadRequest(bufio.NewReader(secure))
				if err != nil {
					return
				}
				f.requestedHosts <- request.Host
				_, _ = secure.Write([]byte("HTTP/1.1 " + f.status + "\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
			}()
			client, err := (&net.Dialer{}).DialContext(ctx, "tcp4", listener.Addr().String())
			if err != nil {
				_ = listener.Close()
				return nil, err
			}
			if f.wrongSocket {
				source = netip.MustParseAddr("192.0.2.99")
			}
			return cellularProbeConnection{client, net.TCPAddrFromAddrPort(netip.AddrPortFrom(source, 12345)), net.TCPAddrFromAddrPort(target)}, nil
		},
	}
}

func TestCellularProbeFixedVerifiedHTTPS(t *testing.T) {
	f := newCellularProbeFixture(t)
	result, err := runCellularInternetProbe(context.Background(), f.dependencies(t))
	if err != nil || result.Status != "passed" || !result.DNSVerified || !result.TLSVerified || !result.HTTPVerified || !result.IdentityUnchanged || f.snapshots != 2 || f.dials != 1 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	select {
	case host := <-f.requestedHosts:
		if host != cellularProbeHost {
			t.Fatal("request used an unexpected host")
		}
	default:
		t.Fatal("fixed-host request was not observed")
	}
	raw, _ := json.Marshal(result)
	for _, private := range []string{"192.0.2.", "93.184.", "fixture-boot"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("result exposed private network identity")
		}
	}
}

func TestCellularProbeCertificateAndRedirectFailures(t *testing.T) {
	for _, name := range []string{"untrusted", "hostname", "redirect", "status"} {
		t.Run(name, func(t *testing.T) {
			f := newCellularProbeFixture(t)
			expected := "CELLULAR_PROBE_TLS_FAILED"
			switch name {
			case "untrusted":
				f.trust = x509.NewCertPool()
			case "hostname":
				f.certificate, f.trust = cellularProbeCertificate(t, "different.invalid")
			case "redirect":
				f.status, expected = "302 Found", "CELLULAR_PROBE_HTTP_STATUS"
			case "status":
				f.status, expected = "503 Unavailable", "CELLULAR_PROBE_HTTP_STATUS"
			}
			result, err := runCellularInternetProbe(context.Background(), f.dependencies(t))
			if err == nil || err.Error() != expected || result.Status == "passed" || result.IdentityUnchanged {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}

func TestCellularProbeRejectsIdentityChangeAndUnboundSocket(t *testing.T) {
	for _, name := range []string{"boot", "ifindex", "source", "gateway", "dns", "socket"} {
		t.Run(name, func(t *testing.T) {
			f := newCellularProbeFixture(t)
			switch name {
			case "boot":
				f.after.boot = "different-boot"
			case "ifindex":
				f.after.index++
			case "source":
				f.after.address = netip.MustParsePrefix("192.0.2.99/24")
			case "gateway":
				f.after.gateway = "192.0.2.2"
			case "dns":
				f.after.servers = []netip.Addr{netip.MustParseAddr("192.0.2.54")}
			case "socket":
				f.wrongSocket = true
			}
			result, err := runCellularInternetProbe(context.Background(), f.dependencies(t))
			if err == nil || result.Status == "passed" || result.IdentityUnchanged {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}

func TestCellularProbeRejectsNonPublicDNSBeforeDial(t *testing.T) {
	for _, value := range []string{"10.1.2.3", "100.64.1.1", "127.0.0.1", "169.254.1.1", "192.168.226.1", "192.0.2.1", "198.18.1.1", "224.0.0.1", "255.255.255.255", "::1", "::ffff:8.8.8.8"} {
		t.Run(value, func(t *testing.T) {
			f := newCellularProbeFixture(t)
			f.addresses = []netip.Addr{netip.MustParseAddr(value)}
			result, err := runCellularInternetProbe(context.Background(), f.dependencies(t))
			if err == nil || err.Error() != "CELLULAR_PROBE_DNS_INVALID" || f.dials != 0 || result.Status == "passed" {
				t.Fatalf("result=%+v error=%v dials=%d", result, err, f.dials)
			}
		})
	}
}

func TestCellularProbeMissingEvidenceNeverDials(t *testing.T) {
	for _, name := range []string{"interface", "dns", "ca", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			f := newCellularProbeFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch name {
			case "interface":
				f.id.iface = "wlan0"
			case "dns":
				f.resolveError = errors.New("private-address-must-not-leak")
			case "ca":
				f.rootError = errors.New("missing-system-roots")
			case "cancelled":
				cancel()
				f.resolveError = ctx.Err()
			}
			result, err := runCellularInternetProbe(ctx, f.dependencies(t))
			if err == nil || f.dials != 0 || result.Status == "passed" || strings.Contains(err.Error(), "private-address") {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}

func TestCellularProbeBoundDNSResolverFixtures(t *testing.T) {
	for _, mode := range []string{"valid", "wrong-id", "empty", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				query := make([]byte, 4096)
				n, peer, err := server.ReadFromUDP(query)
				if err != nil || mode == "timeout" {
					return
				}
				question, end, ok := apDNSName(query[:n], 12)
				if !ok || question != cellularProbeHost || end+4 > n {
					return
				}
				answer := append([]byte(nil), query[:end+4]...)
				answer[2], answer[3] = 0x81, 0x80
				for i := 6; i < 12; i++ {
					answer[i] = 0
				}
				if mode != "empty" {
					binary.BigEndian.PutUint16(answer[6:8], 1)
					answer = append(answer, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 30, 0, 4, 93, 184, 216, 34)
				}
				if mode == "wrong-id" {
					answer[0] ^= 1
				}
				_, _ = server.WriteToUDP(answer, peer)
			}()
			id := apDNSIdentity{iface: "fixture0", address: netip.MustParsePrefix("127.0.0.1/8"), servers: []netip.Addr{netip.MustParseAddr("192.0.2.53")}}
			dial := func(_ context.Context, iface string, source netip.Addr, target netip.AddrPort) (net.Conn, error) {
				if iface != id.iface || source != id.address.Addr() || target != netip.AddrPortFrom(id.servers[0], 53) {
					t.Error("DNS dial lost fixed identity")
				}
				return net.DialUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}, server.LocalAddr().(*net.UDPAddr))
			}
			deadline := 80 * time.Millisecond
			if mode == "valid" {
				deadline = 2 * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), deadline)
			defer cancel()
			addresses, err := resolveCellularProbeWithDial(ctx, id, dial)
			if mode == "valid" {
				if err != nil || len(addresses) != 1 || addresses[0].String() != "93.184.216.34" {
					t.Fatal("valid DNS fixture rejected", err)
				}
			} else if err == nil || len(addresses) != 0 {
				t.Fatal("invalid DNS fixture accepted")
			}
			_ = server.Close()
			<-done
		})
	}
}
