package modem

import (
	"encoding/binary"
	"testing"
)

func TestNativeAPNProfile1OnlyPayload(t *testing.T) {
	p := syntheticRecovery(t).profiles[0]
	raw, e := nativeAPNProfile1Request(p)
	if e != nil {
		t.Fatal(e)
	}
	if len(raw) != 640 || binary.LittleEndian.Uint32(raw[8:]) != 11 || binary.LittleEndian.Uint32(raw[12:]) != 20 || raw[24] != 1 {
		t.Fatal("connect header invalid")
	}
	for _, b := range raw[16:24] {
		if b != 0 {
			t.Fatal("Client pointer must be NULL")
		}
	}
	for _, b := range raw[332:] {
		if b != 0 {
			t.Fatal("profile2 must be omitted")
		}
	}
	for _, field := range []struct {
		offset int
		value  string
	}{{26, *p.apn}, {126, *p.username}, {226, *p.password}} {
		for i, c := range []byte(field.value) {
			if binary.LittleEndian.Uint16(raw[field.offset+2*i:]) != uint16(c) {
				t.Fatal("UTF16 field mismatch")
			}
		}
		for _, b := range raw[field.offset+2*len(field.value) : field.offset+100] {
			if b != 0 {
				t.Fatal("string not zero terminated")
			}
		}
	}
	if binary.LittleEndian.Uint32(raw[328:]) != uint32(*p.authentication) {
		t.Fatal("authentication changed")
	}
	p.index = 2
	if _, e = nativeAPNProfile1Request(p); e == nil {
		t.Fatal("arbitrary profile accepted")
	}
	p.index = 1
	p.password = nil
	if _, e = nativeAPNProfile1Request(p); e == nil {
		t.Fatal("missing credential coerced empty")
	}
}
func TestNativeAPNConnectCompletionSeparateFromAcceptance(t *testing.T) {
	body := nativeAPNTestEvent(0x300, 56, 100)
	raw := body[2].([]byte)
	binary.LittleEndian.PutUint32(raw[44:], 7)
	binary.LittleEndian.PutUint32(raw[48:], 20)
	if kind, _, e := parseNativeAPNReply(body, 100, 7, true); e != nil || kind != "result" {
		t.Fatal(kind, e)
	}
	if kind, _, e := parseNativeAPNReply(body, 100, 7, false); e != nil || kind != "" {
		t.Fatal("connect result accepted by status request")
	}
	body = nativeAPNTestEvent(0x3f3, 40, 0)
	raw = body[2].([]byte)
	raw[44] = 1
	if kind, values, e := parseNativeAPNReply(body, 100, 7, true); e != nil || kind != "status" || values != [2]uint32{1, 0} {
		t.Fatal(kind, values, e)
	}
	if kind, _, e := parseNativeAPNReply(body, 100, 7, false); e != nil || kind != "" {
		t.Fatal("connect event accepted as status")
	}
	raw[44] = 3
	if _, _, e := parseNativeAPNReply(body, 100, 7, true); e == nil {
		t.Fatal("unknown completion enum accepted")
	}
}
func TestNativeAPNReadbackRequiresEveryOriginalField(t *testing.T) {
	a := syntheticRecovery(t).profiles[0]
	b := syntheticRecovery(t).profiles[0]
	if !sameNativeProfile1(a, b) {
		t.Fatal("equal profile mismatch")
	}
	changed := "other.example"
	b.apn = &changed
	if sameNativeProfile1(a, b) {
		t.Fatal("APN mismatch overlooked")
	}
	b = a
	b.username = nil
	if sameNativeProfile1(a, b) {
		t.Fatal("missing credential overlooked")
	}
}
