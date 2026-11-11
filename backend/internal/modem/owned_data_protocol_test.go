package modem

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"testing"
)

func ownedResponseFixture(op ownedDataOperation, fields map[byte][]byte) []byte {
	tx, message, _ := ownedOperationIDs(op)
	frame := []byte{2, byte(tx), byte(tx >> 8), byte(message), byte(message >> 8), 0, 0, 2, 4, 0, 0, 0, 0, 0}
	for _, kind := range []byte{1, 0x15, 0x16, 0x1e, 0x1f, 0x20, 0x21, 0x29, 0x2b} {
		if v, ok := fields[kind]; ok {
			frame = append(frame, kind, byte(len(v)), byte(len(v)>>8))
			frame = append(frame, v...)
		}
	}
	binary.LittleEndian.PutUint16(frame[5:], uint16(len(frame)-7))
	return frame
}
func ownedCurrentFixture() []byte {
	return ownedResponseFixture(ownedCurrent, map[byte][]byte{
		0x15: {53, 2, 0, 192}, 0x16: {54, 2, 0, 192},
		0x1e: {10, 2, 0, 192}, 0x1f: {0, 1}, 0x20: {1, 2, 0, 192}, 0x21: {0, 255, 255, 255},
		0x29: {0xdc, 5, 0, 0}, 0x2b: {4},
	})
}

func TestOwnedRequestsNeverAlterSavedProfileOrAutoconnect(t *testing.T) {
	for op := ownedBindMux; op <= ownedStatusAfter; op++ {
		frame, e := ownedDataRequest(op, ownedPacketHandle{0x12345678})
		if e != nil || int(binary.LittleEndian.Uint16(frame[5:])) != len(frame)-7 {
			t.Fatalf("operation %d malformed: %v", op, e)
		}
		for at := 7; at < len(frame); {
			kind := frame[at]
			n := int(binary.LittleEndian.Uint16(frame[at+1:]))
			at += 3
			if at+n > len(frame) {
				t.Fatal("bad fixed TLV bounds")
			}
			if op == ownedStart && kind != 0x19 && kind != 0x30 && kind != 0x31 {
				t.Fatalf("Start contains credential/APN/autoconnect override %x", kind)
			}
			if op == ownedStop && kind != 1 {
				t.Fatal("Stop changes autoconnect policy")
			}
			at += n
		}
		message := binary.LittleEndian.Uint16(frame[3:])
		if message == 0x27 || message == 0x28 || message == 0x51 || message == 0x89 {
			t.Fatal("profile/persistent policy mutation exposed")
		}
	}
	for _, h := range []uint32{0, 0xffffffff} {
		if _, e := ownedDataRequest(ownedStop, ownedPacketHandle{h}); e == nil {
			t.Fatal("wildcard/unknown Stop accepted")
		}
	}
	if _, e := ownedDataRequest(ownedDataOperation(255), ownedPacketHandle{}); e == nil {
		t.Fatal("arbitrary operation exposed")
	}
	b, _ := ownedDataRequest(ownedBindMux, ownedPacketHandle{})
	if b[21] != 0x82 {
		t.Fatal("binding is not fixed to owned mux")
	}
}

func TestOwnedCurrentSettingsByteOrderAndPrivacy(t *testing.T) {
	s, e := parseOwnedCurrentSettings(ownedCurrentFixture())
	if e != nil || s.address != netip.MustParseAddr("192.0.2.10") || s.gateway != netip.MustParseAddr("192.0.2.1") || s.prefixBits != 24 || s.mtu != 1500 || len(s.dns) != 2 {
		t.Fatalf("settings parse incorrect: %v", e)
	}
	b, _ := json.Marshal(s)
	if strings.Contains(string(b), "192.") || strings.Contains(fmt.Sprintf("%#v", s), "192.") {
		t.Fatal("private address leaked")
	}
	for n := 0; n < len(ownedCurrentFixture()); n++ {
		if _, e := parseOwnedCurrentSettings(ownedCurrentFixture()[:n]); e == nil {
			t.Fatalf("truncation %d accepted", n)
		}
	}
}

func TestOwnedCurrentSettingsRejectDifferentProfileFamilyAndBadMask(t *testing.T) {
	for _, test := range []struct {
		kind  byte
		value []byte
	}{
		{0x1f, []byte{0, 2}}, {0x1f, []byte{1, 1}}, {0x2b, []byte{6}},
		{0x1e, []byte{0, 0, 0, 0}}, {0x1e, []byte{1, 0, 0, 127}},
		{0x20, []byte{1, 0, 0, 224}}, {0x21, []byte{0, 0, 0, 0}}, {0x21, []byte{255, 0, 255, 255}},
		{0x29, []byte{0, 0, 0, 0}}, {0x29, []byte{0xdc, 5, 0}},
	} {
		raw := ownedCurrentFixture()
		values, _ := parseOwnedDataResponse(ownedCurrent, raw)
		values[test.kind] = test.value
		if _, e := parseOwnedCurrentSettings(ownedResponseFixture(ownedCurrent, values)); e == nil {
			t.Fatalf("bad setting kind %x accepted", test.kind)
		}
	}
}

func TestOwnedHandleAndStatusRequireCorrelatedSuccess(t *testing.T) {
	raw := ownedResponseFixture(ownedStart, map[byte][]byte{1: {0x78, 0x56, 0x34, 0x12}})
	h, e := parseOwnedPacketHandle(raw)
	if e != nil || h.value != 0x12345678 {
		t.Fatalf("own handle rejected: %v", e)
	}
	raw[1]++
	if _, e := parseOwnedPacketHandle(raw); e == nil {
		t.Fatal("foreign transaction accepted")
	}
	for _, value := range [][]byte{{}, {0, 0, 0, 0}, {255, 255, 255, 255}, {1, 0, 0}} {
		if _, e := parseOwnedPacketHandle(ownedResponseFixture(ownedStart, map[byte][]byte{1: value})); e == nil {
			t.Fatal("invalid handle accepted")
		}
	}
	for _, v := range []byte{0, 5, 255} {
		if _, e := parseOwnedConnectionStatus(ownedStatusAfter, ownedResponseFixture(ownedStatusAfter, map[byte][]byte{1: {v}})); e == nil {
			t.Fatal("unknown status converted")
		}
	}
}
