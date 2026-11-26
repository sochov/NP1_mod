package stockbt

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestConnectRequestMatchesStock(t *testing.T) {
	// Captured from the request that made NP1 connect VSSPP3 on 2026-11-26.
	want, _ := hex.DecodeString("50000000000000001900000018000000010000000000000064497de0db420000" +
		"010000000c000000ffff00000000000000000000102700000100000000000000")
	c := &Client{}
	c.seq.Store(0x4f)
	a, err := ParseAddr("64:49:7D:E0:DB:42")
	if err != nil {
		t.Fatal(err)
	}
	p := c.connectRequest(0x19, a, []uint32{ProfileVSSPP3})
	if len(p) != 0x2e0 {
		t.Fatalf("length %#x", len(p))
	}
	if !bytes.Equal(p[:len(want)], want) || bytes.ContainsFunc(p[len(want):], func(r rune) bool { return r != 0 }) {
		t.Fatalf("request\n%x\nwant\n%x", p[:len(want)], want)
	}
}

func TestSetLinkkeyLayout(t *testing.T) {
	c := &Client{}
	a, _ := ParseAddr("64:49:7D:E0:DB:42")
	var key [16]byte
	for i := range key {
		key[i] = byte(i + 1)
	}
	p := c.managerRequest(0x10, 0x30)
	copy(p[0x18:], a[:])
	copy(p[0x1e:], key[:])
	if binary.LittleEndian.Uint32(p[8:]) != 0x10 || binary.LittleEndian.Uint32(p[12:]) != 0x0f {
		t.Fatalf("header %x", p[:0x18])
	}
	if !bytes.Equal(p[0x18:0x1e], a[:]) || !bytes.Equal(p[0x1e:0x2e], key[:]) {
		t.Fatalf("arguments %x", p[0x18:])
	}
}

func TestFindTable(t *testing.T) {
	alexa := append(append([]byte{1}, uuidBytes("931c7e8a-540f-4686-b798-e8df0a2ad9f7")...), uuidBytes("931c7e8a-540f-4686-b798-e8df0a2ad9f7")...)
	mem := make([]byte, 0x1000)
	const base = 0x230
	spp := append(append([]byte{1}, uuidBytes("00001101-0000-1000-8000-00805f9b34fb")...), uuidBytes("00001101-0000-1000-8000-00805f9b34fb")...)
	copy(mem[base+slotValid:], spp)
	copy(mem[base+slotValid+slotStride:], iap2Slot)
	copy(mem[base+slotValid+2*slotStride:], alexa)
	for i := 3; i < slotCount; i++ {
		copy(mem[base+slotValid+slotStride*i:], emptySlot)
	}
	// A stray copy of the iAP2 UUIDs (a request buffer) is not a table.
	copy(mem[0xd00:], iap2Slot)
	read := func(addr uint64, n int) ([]byte, error) { return mem[addr-0x10000 : addr-0x10000+uint64(n)], nil }
	got, err := findTable([]region{{0x10000, 0x10000 + uint64(len(mem))}}, read)
	if err != nil {
		t.Fatal(err)
	}
	if got != 0x10000+base {
		t.Fatalf("table at %#x", got)
	}
	copy(mem[base+slotValid+3*slotStride:], []byte{0, 1, 2})
	if _, err = findTable([]region{{0x10000, 0x10000 + uint64(len(mem))}}, read); err == nil {
		t.Fatal("a damaged table was accepted")
	}
}

func TestParseSPPData(t *testing.T) {
	// Evt045 body captured on 2026-11-26 (WifiStartRequest from the head unit).
	d, _ := hex.DecodeString("11020000000000002d00000001000000010000002d0000007f0000009c790d000000000011020000000000001500000000000000106f00747f0000000300000000000000106f00747f000000001100010a0c3139322e3136382e34332e3110a8290000")
	got, ok := ParseSPPData(Event{ID: eventSPP, Data: d})
	if !ok || got.Slot != 3 || hex.EncodeToString(got.Data) != "001100010a0c3139322e3136382e34332e3110a829" {
		t.Fatalf("%+v %v", got, ok)
	}
}

func TestLinkKeyInSnoop(t *testing.T) {
	a, _ := ParseAddr("64:49:7D:E0:DB:42")
	rec := func(pkt []byte) []byte {
		h := make([]byte, 24)
		binary.BigEndian.PutUint32(h[0:], uint32(len(pkt)))
		binary.BigEndian.PutUint32(h[4:], uint32(len(pkt)))
		return append(h, pkt...)
	}
	notify := func(addr []byte, key byte) []byte {
		p := []byte{4, 0x18, 23}
		p = append(p, addr...)
		p = append(p, bytes.Repeat([]byte{key}, 16)...)
		return append(p, 8)
	}
	f := append([]byte("btsnoop\x00"), 0, 0, 0, 1, 0, 0, 0x03, 0xea)
	f = append(f, rec(notify([]byte{0x42, 0xdb, 0xe0, 0x7d, 0x49, 0x64}, 0x11))...)
	f = append(f, rec([]byte{4, 0x0e, 4, 1, 0x0b, 0x04, 0})...)
	f = append(f, rec(notify([]byte{1, 2, 3, 4, 5, 6}, 0x33))...)
	f = append(f, rec(notify([]byte{0x42, 0xdb, 0xe0, 0x7d, 0x49, 0x64}, 0x22))...)
	k, ok := linkKeyInSnoop(f, a)
	if !ok || k.Key[0] != 0x22 || k.Type != 8 {
		t.Fatalf("%+v %v", k, ok)
	}
	if _, ok = linkKeyInSnoop(f, Addr{9, 9, 9, 9, 9, 9}); ok {
		t.Fatal("found a key for an unknown address")
	}
}
