package modem

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func ownedRMNetFixture() []byte {
	raw := make([]byte, ownedRMNetInfoSize)
	for i, value := range []uint32{164, 4096, 2, 4, 2, 5, 0} {
		binary.LittleEndian.PutUint32(raw[i*4:], value)
	}
	copy(raw[28:], "usb0.1")
	copy(raw[44:], "usb0.2")
	raw[156], raw[157] = 0x81, 0x82
	return raw
}

func TestOwnedQMAPReadOnlyABI(t *testing.T) {
	for _, version := range []byte{5, 9} {
		raw := ownedRMNetFixture()
		raw[20] = version
		got, e := parseOwnedRMNetInfo(raw)
		if e != nil || got.interfaces[0] != "usb0.1" || got.mux[0] != 0x81 || got.mux[1] != 0x82 {
			t.Fatalf("supported QMAP ABI rejected: %v", e)
		}
		b, _ := json.Marshal(got)
		if strings.Contains(string(b), "usb0") || strings.Contains(fmt.Sprintf("%#v", got), "usb0") {
			t.Fatal("private binding leaked")
		}
	}
}

func TestOwnedQMAPRejectsChangedOrTruncatedABI(t *testing.T) {
	for _, offset := range []int{0, 4, 8, 12, 16, 20, 24, 28, 34, 44, 50, 60, 155, 156, 157} {
		raw := ownedRMNetFixture()
		raw[offset] ^= 1
		if _, e := parseOwnedRMNetInfo(raw); e == nil {
			t.Fatalf("changed byte %d accepted", offset)
		}
	}
	for n := 0; n < ownedRMNetInfoSize; n++ {
		if _, e := parseOwnedRMNetInfo(ownedRMNetFixture()[:n]); e == nil {
			t.Fatalf("length %d accepted", n)
		}
	}
	if _, e := parseOwnedRMNetInfo(append(ownedRMNetFixture(), 0)); e == nil {
		t.Fatal("oversize accepted")
	}
}
