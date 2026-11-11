package modem

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestProfileInventoryFixedTypeAndIndicesWithoutNames(t *testing.T) {
	want := []byte{0, 7, 0, 0x2a, 0, 4, 0, 0x10, 1, 0, 0}
	request, e := fixedQMIRequest(7, 0x2a)
	if e != nil || !bytes.Equal(request, want) {
		t.Fatal(request, e)
	}
	list := []byte{2, 0, 2, 12}
	list = append(list, []byte("private-name")...)
	list = append(list, 0, 1, 0)
	value, e := parseProfileInventory(statusFrame(7, 0x2a, map[byte][]byte{1: list}))
	if e != nil || !value.Profile1Present || !value.Profile2Present || len(value.Indices) != 2 || value.Indices[0] != 1 || value.Current {
		t.Fatal(value, e)
	}
	raw, _ := json.Marshal(value)
	if strings.Contains(string(raw), "private-name") {
		t.Fatal("profile name escaped")
	}
	value, e = parseProfileInventory(statusFrame(7, 0x2a, map[byte][]byte{1: {1, 0, 1, 0}}))
	if e != nil || !value.Profile1Present || value.Profile2Present {
		t.Fatal(value, e)
	}
	value, e = parseProfileInventory(statusFrame(7, 0x2a, map[byte][]byte{1: {0}}))
	if e != nil || len(value.Indices) != 0 || value.Profile1Present {
		t.Fatal(value, e)
	}
}
func TestProfileInventoryRejectsMalformedArrayAndWrongFamily(t *testing.T) {
	for _, list := range [][]byte{nil, {1}, {1, 0, 1}, {1, 0, 1, 5, 'a'}, {2, 0, 1, 0, 0, 1, 0}, {1, 1, 1, 0}, {0, 0}} {
		if _, e := parseProfileInventory(statusFrame(7, 0x2a, map[byte][]byte{1: list})); e == nil {
			t.Fatal("malformed profile list accepted", list)
		}
	}
	if _, e := parseProfileInventory(statusFrame(7, 0x2a, nil)); e == nil {
		t.Fatal("missing list accepted as empty")
	}
}
func FuzzProfileInventoryBounds(f *testing.F) {
	f.Add(statusFrame(7, 0x2a, map[byte][]byte{1: {1, 0, 1, 0}}))
	f.Fuzz(func(t *testing.T, frame []byte) { _, _ = parseProfileInventory(frame) })
}
