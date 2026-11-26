package stockbt

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SnoopLog is the HCI log the stock stack always writes (rotated about every
// 25 minutes into btsnoop_hci.log.<timestamp> next to it).
const SnoopLog = "/pio_log/rw/ERASABLE/btsnoop_hci.log"

// The stack writes new link keys to a file under a read-only directory, so
// they are gone after a reboot, and no stock event carries them. The HCI log
// does: HCI Link Key Notification (event 0x18) holds the peer address
// (reversed) and the key.

// LinkKey is a classic BT link key and its HCI key type.
type LinkKey struct {
	Key  [16]byte
	Type byte
}

// FindLinkKey returns the newest link key for a in the stock HCI logs.
func FindLinkKey(log string, a Addr) (LinkKey, error) {
	files := []string{log}
	if rotated, _ := filepath.Glob(log + ".*"); len(rotated) > 0 {
		sort.Sort(sort.Reverse(sort.StringSlice(rotated)))
		files = append(files, rotated[:min(len(rotated), 2)]...)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if k, ok := linkKeyInSnoop(b, a); ok {
			return k, nil
		}
	}
	return LinkKey{}, errors.New("stockbt: no link key for " + a.String() + " in the HCI log")
}

// linkKeyInSnoop scans a btsnoop file (H4 records) for the last link key
// notification of a.
func linkKeyInSnoop(b []byte, a Addr) (LinkKey, bool) {
	if len(b) < 16 || !strings.HasPrefix(string(b[:8]), "btsnoop") {
		return LinkKey{}, false
	}
	var rev Addr
	for i := range a {
		rev[i] = a[5-i]
	}
	var found LinkKey
	ok := false
	for p := 16; p+24 <= len(b); {
		n := int(binary.BigEndian.Uint32(b[p+4:]))
		p += 24
		if n < 0 || p+n > len(b) {
			break
		}
		pkt := b[p : p+n]
		p += n
		// H4 event 0x18: type 4, code 0x18, length 23, address, key, key type.
		if len(pkt) >= 26 && pkt[0] == 4 && pkt[1] == 0x18 && pkt[2] == 23 && string(pkt[3:9]) == string(rev[:]) {
			copy(found.Key[:], pkt[9:25])
			found.Type = pkt[25]
			ok = true
		}
	}
	return found, ok
}
