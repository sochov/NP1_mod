package stockbt

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// The stock stack keeps the vendor SPP slots (VSSPP1-5) in a table inside
// NEventWatcherCom: six 0x60-byte entries (0 is the plain Serial Port, 1-5 are
// VSSPP1-5), each with a valid byte at +0x48, the local UUID at +0x49 and the
// remote UUID at +0x59 (relative to the entry).
// Connect (profile 0x0a-0x0e) reads the remote UUID from this table when it
// searches the peer's SDP records, so an entry can be filled while BT runs.
// SetProfile, the stock way, is only accepted while BT is stopped, and a
// manual stop/start leaves the stock post-start steps (BLE advertising and
// scanning) undone until reboot.
//
// Writing the table changes nothing on disk; a reboot restores the stock one.
const (
	slotStride = 0x60
	slotValid  = 0x48
	slotCount  = 6
	slotSize   = 1 + 16 + 16
)

// iAP2 is VSSPP1 (entry 1) in the stock configuration: the anchor used to find
// the table.
var iap2Slot = append(append([]byte{1}, uuidBytes("00000000-deca-fade-deca-deafdecacaff")...), uuidBytes("00000000-deca-fade-deca-deafdecacafe")...)

func uuidBytes(s string) []byte {
	b, err := parseUUID(s)
	if err != nil {
		panic(err)
	}
	return b[:]
}

func parseUUID(s string) ([16]byte, error) {
	var u [16]byte
	h := strings.ReplaceAll(s, "-", "")
	if len(h) != 32 {
		return u, fmt.Errorf("stockbt: bad UUID %q", s)
	}
	for i := range 16 {
		v, err := strconv.ParseUint(h[2*i:2*i+2], 16, 8)
		if err != nil {
			return u, fmt.Errorf("stockbt: bad UUID %q", s)
		}
		u[i] = byte(v)
	}
	return u, nil
}

// slotEntry is the 33 bytes at +0x48 of a slot.
func slotEntry(local, remote [16]byte) []byte {
	b := []byte{1}
	b = append(b, local[:]...)
	return append(b, remote[:]...)
}

var emptySlot = append([]byte{0}, bytes.Repeat([]byte{0xff}, 32)...)

// findTable returns the table address in the stock process memory image.
// read(addr, n) reads process memory.
func findTable(regions []region, read func(addr uint64, n int) ([]byte, error)) (uint64, error) {
	var found []uint64
	for _, r := range regions {
		data, err := read(r.start, int(r.end-r.start))
		if err != nil {
			continue
		}
		for off := 0; ; {
			i := bytes.Index(data[off:], iap2Slot)
			if i < 0 {
				break
			}
			at := off + i
			off = at + 1
			base := at - slotValid - slotStride
			if base < 0 || base+slotValid+slotStride*slotCount > len(data) {
				continue
			}
			if validTable(data[base:]) {
				found = append(found, r.start+uint64(base))
			}
		}
	}
	switch len(found) {
	case 0:
		return 0, errors.New("stockbt: vendor SPP table not found")
	case 1:
		return found[0], nil
	}
	return 0, fmt.Errorf("stockbt: %d vendor SPP table candidates", len(found))
}

// validTable checks that every slot is either empty (0, all 0xff) or valid.
func validTable(t []byte) bool {
	for i := range slotCount {
		e := t[slotValid+slotStride*i:][:slotSize]
		if e[0] == 1 {
			continue
		}
		if !bytes.Equal(e, emptySlot) {
			return false
		}
	}
	return true
}

type region struct{ start, end uint64 }

// writableRegions lists the private writable mappings (heap and anonymous).
func writableRegions(pid uint32) ([]region, error) {
	f, err := os.Open(fmt.Sprintf("/proc/%d/maps", pid))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []region
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 5 || !strings.HasPrefix(fields[1], "rw") {
			continue
		}
		if len(fields) >= 6 && !strings.HasPrefix(fields[5], "[heap]") && !strings.HasPrefix(fields[5], "[anon") {
			continue
		}
		a, b, _ := strings.Cut(fields[0], "-")
		s, e1 := strconv.ParseUint(a, 16, 64)
		e, e2 := strconv.ParseUint(b, 16, 64)
		if e1 != nil || e2 != nil || e-s > 64<<20 {
			continue
		}
		out = append(out, region{s, e})
	}
	return out, sc.Err()
}

// SlotState is what a vendor SPP slot currently holds.
type SlotState struct {
	Slot   int
	Valid  bool
	Local  [16]byte
	Remote [16]byte
}

// SetVendorSlot fills vendor SPP slot n (VSSPPn, 1-5) of the stock
// process pid with local/remote UUIDs while BT keeps running. It refuses to
// overwrite a slot holding other UUIDs. It needs root (ptrace access to pid).
func SetVendorSlot(pid uint32, slot int, local, remote string) (SlotState, error) {
	if slot < 1 || slot >= slotCount {
		return SlotState{}, fmt.Errorf("stockbt: slot %d out of range", slot)
	}
	lu, err := parseUUID(local)
	if err != nil {
		return SlotState{}, err
	}
	ru, err := parseUUID(remote)
	if err != nil {
		return SlotState{}, err
	}
	mem, err := os.OpenFile(fmt.Sprintf("/proc/%d/mem", pid), os.O_RDWR, 0)
	if err != nil {
		return SlotState{}, fmt.Errorf("stockbt: stock process memory: %w", err)
	}
	defer mem.Close()
	read := func(addr uint64, n int) ([]byte, error) {
		b := make([]byte, n)
		_, err := mem.ReadAt(b, int64(addr))
		return b, err
	}
	regions, err := writableRegions(pid)
	if err != nil {
		return SlotState{}, err
	}
	table, err := findTable(regions, read)
	if err != nil {
		return SlotState{}, err
	}
	at := table + slotValid + uint64(slotStride*slot)
	cur, err := read(at, slotSize)
	if err != nil {
		return SlotState{}, err
	}
	want := slotEntry(lu, ru)
	switch {
	case bytes.Equal(cur, want):
	case bytes.Equal(cur, emptySlot):
		// UUIDs first, the valid byte last, so a reader never sees a valid
		// slot with a half-written UUID.
		if _, err = mem.WriteAt(want[1:], int64(at+1)); err != nil {
			return SlotState{}, fmt.Errorf("stockbt: write slot: %w", err)
		}
		if _, err = mem.WriteAt(want[:1], int64(at)); err != nil {
			return SlotState{}, fmt.Errorf("stockbt: write slot: %w", err)
		}
	default:
		return SlotState{}, fmt.Errorf("stockbt: slot %d is in use (%x)", slot, cur)
	}
	got, err := read(at, slotSize)
	if err != nil || !bytes.Equal(got, want) {
		return SlotState{}, fmt.Errorf("stockbt: slot %d did not take the UUIDs", slot)
	}
	st := SlotState{Slot: slot, Valid: true}
	copy(st.Local[:], got[1:17])
	copy(st.Remote[:], got[17:33])
	return st, nil
}
