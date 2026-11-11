package adapters

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/modem"
)

// These addresses are existing system resolver configuration, not claimed to
// have been distributed by the carrier. Only an actual interface/source-bound
// DNS answer makes one usable for that cellular Linux path.
type apDNSIdentity struct {
	iface, boot string
	index       int
	address     netip.Prefix
	gateway     string
	servers     []netip.Addr
}
type apDNSDependencies struct {
	snapshot func(context.Context) (apDNSIdentity, error)
	probe    func(context.Context, apDNSIdentity, netip.Addr) error
	now      func() time.Time
}
type apDNSVerifier struct {
	mu               sync.Mutex
	d                apDNSDependencies
	identity         *apDNSIdentity
	verified         []string
	expires, retryAt time.Time
}

var apCellularDNSVerifier = apDNSVerifier{d: apDNSDependencies{snapshot: apCellularDNSSnapshot, probe: func(ctx context.Context, id apDNSIdentity, server netip.Addr) error {
	return apBoundDNSQuery(ctx, apDialBoundDNS, id.iface, id.address.Addr(), netip.AddrPortFrom(server, 53))
}, now: time.Now}}

func verifiedAPCellularDNS(ctx context.Context, proof apCellularRouteProof) ([]string, error) {
	return apCellularDNSVerifier.verify(ctx, proof)
}
func (v *apDNSVerifier) invalidate() {
	v.identity = nil
	v.verified = nil
	v.expires = time.Time{}
	v.retryAt = time.Time{}
}
func (v *apDNSVerifier) verify(parent context.Context, expected apCellularRouteProof) ([]string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	// The full verification, including Linux identity re-read, has one budget.
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	before, e := v.d.snapshot(ctx)
	if e != nil || before.iface != expected.iface || before.address != expected.address || before.gateway != expected.gateway {
		v.invalidate()
		return nil, errors.New("AP_CELLULAR_DNS_IDENTITY_UNVERIFIED")
	}
	now := v.d.now()
	same := v.identity != nil && reflect.DeepEqual(*v.identity, before)
	if same && now.Before(v.expires) && len(v.verified) > 0 {
		return append([]string(nil), v.verified...), nil
	}
	if same && now.Before(v.retryAt) {
		return nil, errors.New("AP_CELLULAR_SYSTEM_DNS_UNREACHABLE")
	}
	v.invalidate()
	if len(before.servers) == 0 || len(before.servers) > 4 {
		return nil, errors.New("AP_CELLULAR_SYSTEM_DNS_UNAVAILABLE")
	}
	// Existing resolver failover is retained without allowing N sequential
	// one-second stalls. Buffered results let timed-out workers exit safely.
	results := make(chan netip.Addr, len(before.servers))
	for _, server := range before.servers {
		go func(server netip.Addr) {
			if v.d.probe(ctx, before, server) == nil {
				results <- server
			} else {
				results <- netip.Addr{}
			}
		}(server)
	}
	var chosen netip.Addr
	for remaining := len(before.servers); remaining > 0; remaining-- {
		select {
		case server := <-results:
			if server.IsValid() {
				chosen = server
				remaining = 0
			}
		case <-ctx.Done():
			remaining = 0
		}
	}
	if !chosen.IsValid() || ctx.Err() != nil {
		copyIdentity := before
		v.identity = &copyIdentity
		v.retryAt = v.d.now().Add(5 * time.Second)
		return nil, errors.New("AP_CELLULAR_SYSTEM_DNS_UNREACHABLE")
	}
	after, e := v.d.snapshot(ctx)
	if e != nil || !reflect.DeepEqual(before, after) {
		v.invalidate()
		return nil, errors.New("AP_CELLULAR_DNS_IDENTITY_CHANGED")
	}
	copyIdentity := after
	v.identity = &copyIdentity
	v.verified = []string{chosen.String()}
	v.expires = v.d.now().Add(60 * time.Second)
	return append([]string(nil), v.verified...), nil
}

func apCellularDNSSnapshot(ctx context.Context) (apDNSIdentity, error) {
	dep := apCellularProofDeps{modem.VerifyCellularInterfaces, os.ReadFile, apCellularIPCommand, time.Now}
	proof, e := readAPCellularRouteProof(ctx, dep)
	if e != nil {
		return apDNSIdentity{}, e
	}
	raw, e := os.ReadFile("/sys/class/net/" + proof.iface + "/ifindex")
	if e != nil {
		return apDNSIdentity{}, e
	}
	index, e := strconv.Atoi(strings.TrimSpace(string(raw)))
	if e != nil || index <= 0 {
		return apDNSIdentity{}, errors.New("AP_CELLULAR_INDEX_INVALID")
	}
	boot := read("/proc/sys/kernel/random/boot_id")
	if boot == "" || len(boot) > 64 {
		return apDNSIdentity{}, errors.New("AP_CELLULAR_BOOT_UNVERIFIED")
	}
	f, e := os.Open("/etc/resolv.conf")
	if e != nil {
		return apDNSIdentity{}, errors.New("AP_CELLULAR_SYSTEM_DNS_UNAVAILABLE")
	}
	resolver, e := io.ReadAll(io.LimitReader(f, 16385))
	_ = f.Close()
	if e != nil || len(resolver) > 16384 {
		return apDNSIdentity{}, errors.New("AP_CELLULAR_SYSTEM_DNS_UNAVAILABLE")
	}
	id := apDNSIdentity{iface: proof.iface, boot: boot, index: index, address: proof.address, gateway: proof.gateway}
	for _, server := range apResolverIPv4(string(resolver)) {
		route, e := apCellularIPCommand(ctx, "-4", "route", "get", server, "oif", proof.iface, "from", proof.address.Addr().String())
		if e != nil {
			continue
		}
		if _, e = apParseUpstreamRoute(route, proof.iface, proof.address.Addr().String()); e != nil {
			continue
		}
		address, _ := netip.ParseAddr(server)
		id.servers = append(id.servers, address)
	}
	if len(id.servers) == 0 {
		return apDNSIdentity{}, errors.New("AP_CELLULAR_SYSTEM_DNS_UNAVAILABLE")
	}
	return id, nil
}

type apDNSDial func(context.Context, string, netip.Addr, netip.AddrPort) (*net.UDPConn, error)

func apBoundDNSQuery(ctx context.Context, dial apDNSDial, iface string, source netip.Addr, server netip.AddrPort) error {
	connection, e := dial(ctx, iface, source, server)
	if e != nil {
		return errors.New("AP_CELLULAR_DNS_CONNECT_FAILED")
	}
	defer connection.Close()
	local, ok := connection.LocalAddr().(*net.UDPAddr)
	if !ok || local.AddrPort().Addr().Unmap() != source.Unmap() {
		return errors.New("AP_CELLULAR_DNS_SOURCE_MISMATCH")
	}
	deadline := time.Now().Add(time.Second)
	if parent, ok := ctx.Deadline(); ok && parent.Before(deadline) {
		deadline = parent
	}
	_ = connection.SetDeadline(deadline)
	var token [2]byte
	if _, e = rand.Read(token[:]); e != nil {
		return errors.New("AP_CELLULAR_DNS_TOKEN_FAILED")
	}
	request := []byte{token[0], token[1], 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0, 0, 1, 0, 1}
	if _, e = connection.Write(request); e != nil {
		return errors.New("AP_CELLULAR_DNS_SEND_FAILED")
	}
	response := make([]byte, 4097)
	n, peer, e := connection.ReadFromUDPAddrPort(response)
	if e != nil {
		return errors.New("AP_CELLULAR_DNS_TIMEOUT")
	}
	if peer.Addr().Unmap() != server.Addr().Unmap() || peer.Port() != server.Port() {
		return errors.New("AP_CELLULAR_DNS_PEER_MISMATCH")
	}
	if n > 4096 || !apDNSAResponse(response[:n], binary.BigEndian.Uint16(token[:])) {
		return errors.New("AP_CELLULAR_DNS_RESPONSE_INVALID")
	}
	return nil
}

// DNS compression is bounded by packet length, pointer visits and 255 name
// bytes. It never treats an unrelated additional A record as a valid answer.
func apDNSName(packet []byte, start int) (string, int, bool) {
	offset, next, total := start, -1, 0
	labels := []string{}
	visited := map[int]bool{}
	for steps := 0; steps < 64; steps++ {
		if offset < 0 || offset >= len(packet) || visited[offset] {
			return "", 0, false
		}
		visited[offset] = true
		length := int(packet[offset])
		offset++
		if length&0xc0 == 0xc0 {
			if offset >= len(packet) {
				return "", 0, false
			}
			pointer := (length&0x3f)<<8 | int(packet[offset])
			if next < 0 {
				next = offset + 1
			}
			offset = pointer
			continue
		}
		if length&0xc0 != 0 || length > 63 {
			return "", 0, false
		}
		if length == 0 {
			if next < 0 {
				next = offset
			}
			return strings.ToLower(strings.Join(labels, ".")), next, true
		}
		if offset+length > len(packet) {
			return "", 0, false
		}
		label := packet[offset : offset+length]
		for _, c := range label {
			if c < 33 || c > 126 || c == '.' {
				return "", 0, false
			}
		}
		total += length + 1
		if total > 255 {
			return "", 0, false
		}
		labels = append(labels, string(label))
		offset += length
	}
	return "", 0, false
}
func apDNSAResponse(packet []byte, token uint16) bool {
	if len(packet) < 12 || binary.BigEndian.Uint16(packet[:2]) != token {
		return false
	}
	flags := binary.BigEndian.Uint16(packet[2:4])
	if flags&0x8000 == 0 || flags&0x7800 != 0 || flags&0x0200 != 0 || flags&15 != 0 || binary.BigEndian.Uint16(packet[4:6]) != 1 {
		return false
	}
	answers := int(binary.BigEndian.Uint16(packet[6:8]))
	if answers < 1 || answers > 64 {
		return false
	}
	question, offset, ok := apDNSName(packet, 12)
	if !ok || question != "example.com" || offset+4 > len(packet) || binary.BigEndian.Uint16(packet[offset:offset+2]) != 1 || binary.BigEndian.Uint16(packet[offset+2:offset+4]) != 1 {
		return false
	}
	offset += 4
	aliases := map[string]string{}
	addresses := map[string]bool{}
	for i := 0; i < answers; i++ {
		owner, next, ok := apDNSName(packet, offset)
		if !ok || next+10 > len(packet) {
			return false
		}
		kind, class := binary.BigEndian.Uint16(packet[next:next+2]), binary.BigEndian.Uint16(packet[next+2:next+4])
		length := int(binary.BigEndian.Uint16(packet[next+8 : next+10]))
		begin := next + 10
		end := begin + length
		if end > len(packet) {
			return false
		}
		if class == 1 && kind == 1 && length == 4 {
			var raw [4]byte
			copy(raw[:], packet[begin:end])
			address := netip.AddrFrom4(raw)
			if address.IsGlobalUnicast() && !address.IsPrivate() {
				addresses[owner] = true
			}
		}
		if class == 1 && kind == 5 {
			name, following, ok := apDNSName(packet, begin)
			if !ok || following != end {
				return false
			}
			if _, exists := aliases[owner]; exists {
				return false
			}
			aliases[owner] = name
		}
		offset = end
	}
	for steps := 0; steps < 16; steps++ {
		if addresses[question] {
			return true
		}
		next, ok := aliases[question]
		if !ok {
			return false
		}
		question = next
	}
	return false
}
