package modem

import (
	"encoding/binary"
	"errors"
	"testing"
)

func FuzzNASParserBounds(f *testing.F) {
	f.Add([]byte{2, 1, 0, 0x4f, 0, 0, 0})
	f.Add(response([]byte{2, 4, 0, 0, 0, 0, 0}))
	f.Fuzz(func(t *testing.T, frame []byte) { _, _ = parseSignalResponse(frame, 1) })
}

func response(payload []byte) []byte {
	frame := []byte{2, 1, 0, 0x4f, 0, 0, 0}
	binary.LittleEndian.PutUint16(frame[5:], uint16(len(payload)))
	return append(frame, payload...)
}
func TestNASRequestHasNoQMUXOrConfiguration(t *testing.T) {
	want := []byte{0, 0x34, 0x12, 0x4f, 0, 0, 0}
	got := signalRequest(0x1234)
	for i := range want {
		if got[i] != want[i] {
			t.Fatal(got)
		}
	}
}
func TestNASSignalStrictTypesAndScale(t *testing.T) {
	frame := response([]byte{2, 4, 0, 0, 0, 0, 0, 0x14, 6, 0, 0xcd, 0xf5, 0xae, 0xff, 0x9e, 0})
	signal, e := parseSignalResponse(frame, 1)
	if e != nil || *signal.RSSI != -51 || *signal.RSRQ != -11 || *signal.RSRP != -82 || *signal.SNR != 15.8 {
		t.Fatal(signal, e)
	}
}
func TestNASUnknownNeverBecomesZero(t *testing.T) {
	frame := response([]byte{2, 4, 0, 0, 0, 0, 0, 0x14, 6, 0, 127, 127, 255, 127, 255, 127})
	signal, e := parseSignalResponse(frame, 1)
	if e != nil || signal.RSSI != nil || signal.RSRQ != nil || signal.RSRP != nil || signal.SNR != nil {
		t.Fatal(signal, e)
	}
}
func TestNASAbsentLTEIsMissing(t *testing.T) {
	signal, e := parseSignalResponse(response([]byte{2, 4, 0, 0, 0, 0, 0}), 1)
	if e != nil || signal.RSRP != nil || signal.SNR != nil {
		t.Fatal(signal, e)
	}
}
func TestNASMalformedAndFailure(t *testing.T) {
	cases := [][]byte{nil, {2, 1}, {2, 1, 0, 0x4f, 0, 1, 0}, {2, 4, 0, 1, 0, 4, 0}, {2, 4, 0, 0, 0, 0, 0, 0x14, 8, 0, 1}, {0x14, 6, 0, 0xcd, 0xf5, 0xae, 0xff, 0x9e, 0}, {2, 4, 0, 0, 0, 0, 0, 2, 4, 0, 0, 0, 0, 0}}
	for _, payload := range cases {
		if _, e := parseSignalResponse(response(payload), 1); e == nil {
			t.Fatalf("malformed accepted: %x", payload)
		}
	}
	frame := response([]byte{2, 4, 0, 0, 0, 0, 0})
	frame[1] = 2
	if _, e := parseSignalResponse(frame, 1); !errors.Is(e, errUnrelatedQMI) {
		t.Fatal(e)
	}
}
