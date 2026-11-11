package modem

import (
	"encoding/binary"
	"reflect"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestNativeAPNFixedReadOnlyRequest(t *testing.T) {
	raw := nativeAPNGetRequest()
	want := []byte{1, 0, 0, 0, 0, 0, 0, 0, 10, 0, 0, 0, 19, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	if !reflect.DeepEqual(raw, want) {
		t.Fatal("fixed request changed")
	}
	if signature := dbus.SignatureOf(uint32(1), uint32(123), uint32(10), []uint32{}, raw, uint32(1)).String(); signature != "uuuauayu" {
		t.Fatal(signature)
	}
	if !nativeAPNMethodAccepted([]any{[]byte{1}, []uint32{}}) {
		t.Fatal("valid acceptance rejected")
	}
	for _, body := range [][]any{nil, {[]byte{1}}, {[]byte{1, 1}, []uint32{}}, {[]byte{0}, []uint32{}}, {[]byte{1}, []uint32{1}}, {[]byte{1}, []int{}}, {[]byte{1}, []uint32{}, uint32(1)}} {
		if nativeAPNMethodAccepted(body) {
			t.Fatal("invalid acceptance")
		}
	}
}
func nativeAPNTestEvent(event uint64, size int, destination uint32) []any {
	raw := make([]byte, 12+size)
	binary.LittleEndian.PutUint64(raw, event)
	binary.LittleEndian.PutUint32(raw[8:], 12)
	// NEvent C2 stores the event ID at object+0x20; Pack copies object+8.
	// The first packed word is the base flags (1), not the event ID.
	binary.LittleEndian.PutUint64(raw[12:], 1)
	binary.LittleEndian.PutUint64(raw[12+24:], event)
	if event == 0x300 {
		binary.LittleEndian.PutUint32(raw[12+32:], 1)
		binary.LittleEndian.PutUint32(raw[12+36:], 0x13)
	}
	return []any{destination, uint32(0), raw}
}
func TestNativeAPNCorrelatedResultsAndBounds(t *testing.T) {
	body := nativeAPNTestEvent(0x300, 56, 100)
	if kind, value, e := parseNativeAPNSignal(body, 100); e != nil || kind != "result" || value[0] != 0 {
		t.Fatal(kind, value, e)
	}
	for _, dest := range []uint32{0, 101} {
		body[0] = dest
		if kind, _, e := parseNativeAPNSignal(body, 100); e != nil || kind != "" {
			t.Fatal("unrelated destination accepted")
		}
	}
	body[0] = uint32(100)
	raw := body[2].([]byte)
	for _, offset := range []int{12 + 32, 12 + 36} {
		raw[offset]++
		if kind, _, e := parseNativeAPNSignal(body, 100); e != nil || kind != "" {
			t.Fatal("unrelated sequence accepted")
		}
		raw[offset]--
	}
	raw[12+48] = 1
	if _, _, e := parseNativeAPNSignal(body, 100); e == nil {
		t.Fatal("opaque client pointer accepted")
	}
	raw[12+48] = 0
	raw[12+40] = 5
	if _, _, e := parseNativeAPNSignal(body, 100); e == nil {
		t.Fatal("unknown result accepted")
	}
	for _, size := range []int{0, 1, 12, 13, 55, 56, 67, 69, 4109} {
		bad := make([]byte, size)
		if size >= 12 {
			binary.LittleEndian.PutUint64(bad, 0x300)
			binary.LittleEndian.PutUint32(bad[8:], 12)
		}
		if _, _, e := parseNativeAPNSignal([]any{uint32(100), uint32(0), bad}, 100); e == nil {
			t.Fatal("bad size accepted", size)
		}
	}
	for _, invalid := range [][]any{nil, {uint32(1)}, {uint32(1), uint32(0), "raw"}, {uint32(1), 1, []byte{}}, {uint32(100), uint32(1), nativeAPNTestEvent(0x300, 56, 100)[2]}} {
		if _, _, e := parseNativeAPNSignal(invalid, 100); e == nil {
			t.Fatal("invalid signal accepted")
		}
	}
}
func TestNativeAPNStatesAreNotConnectivity(t *testing.T) {
	body := nativeAPNTestEvent(0x3f2, 40, 0)
	raw := body[2].([]byte)
	binary.LittleEndian.PutUint32(raw[44:], 2)
	binary.LittleEndian.PutUint32(raw[48:], 3)
	kind, values, e := parseNativeAPNSignal(body, 100)
	if e != nil || kind != "status" || values != [2]uint32{2, 3} {
		t.Fatal(kind, values, e)
	}
	raw[48] = 4
	if _, _, e := parseNativeAPNSignal(body, 100); e == nil {
		t.Fatal("unknown APN state accepted")
	}
}
func TestNativeAPNUnixTransportAllowlist(t *testing.T) {
	for input, want := range map[string]string{"unix:abstract=/tmp/fixed_bus": "@/tmp/fixed_bus", "unix:path=/run/fixed_bus,guid=0123456789abcdef0123456789ABCDEF": "/run/fixed_bus"} {
		got, e := nativeAPNUnixAddress(input)
		if e != nil || got != want {
			t.Fatal(got, e)
		}
	}
	for _, bad := range []string{"tcp:host=localhost,port=1", "autolaunch:", "unix:abstract=a;unix:path=/b", "unix:path=/a,abstract=b", "unix:path=/a,path=/a", "unix:path=/a,guid=bad", "unix:path=/a,unknown=x", "unix:abstract=", "unix:path=/../a", "unix:abstract=a%00b", "unix:path=/a\x00", "unix:path=relative", "unix:guid=0123456789abcdef0123456789abcdef"} {
		if _, e := nativeAPNUnixAddress(bad); e == nil {
			t.Fatal("unsafe address", bad)
		}
	}
}
func TestNativeAPNConnectClockExcludesEarlierBroadcastAndWraps(t *testing.T) {
	for _, sample := range []struct {
		ack, event uint32
		valid      bool
	}{{1000, 1200, true}, {1000, 999, false}, {1000, 1010, false}, {1000, 10000, false}, {0, 1000, false}, {^uint32(0) - 100, 100, true}} {
		_, valid := nativeAPNConnectClockDelay(sample.ack, sample.event)
		if valid != sample.valid {
			t.Fatal(sample, valid)
		}
	}
}
func FuzzNativeAPNSignal(f *testing.F) {
	f.Add([]byte{})
	f.Add(nativeAPNTestEvent(0x300, 56, 100)[2].([]byte))
	f.Fuzz(func(t *testing.T, raw []byte) {
		_, _, _ = parseNativeAPNSignal([]any{uint32(100), uint32(0), raw}, 100)
	})
}
