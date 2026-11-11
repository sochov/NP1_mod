package modem

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"
)

const ownedDataJournalDirectory = "/overlay/owner-apps/np1-control/private/cellular/session-probe"
const maximumOwnedDataJournal = 32768

// This schema exists only in a root-owned 0600 private file. Never marshal it
// into a public response, print it or send it across a broker/API socket.
type privateOwnedDataJournal struct {
	Schema        int                    `json:"schema"`
	Phase         string                 `json:"phase"`
	ObservedAt    time.Time              `json:"observedAt"`
	BootID        string                 `json:"bootId"`
	ProcessID     int                    `json:"processId"`
	ProcessStart  string                 `json:"processStart"`
	PacketHandle  uint32                 `json:"packetHandle"`
	ProfileBackup string                 `json:"profileBackup"`
	Result        OwnedDataSessionResult `json:"result"`
	IPv4          string                 `json:"ipv4,omitempty"`
	PrefixBits    int                    `json:"prefixBits,omitempty"`
	Gateway       string                 `json:"gateway,omitempty"`
	DNS           []string               `json:"dns,omitempty"`
	MTU           uint32                 `json:"mtu,omitempty"`
	OwnerPID      uint32                 `json:"ownerPid"`
	OwnerStart    string                 `json:"ownerStart"`
	IfIndexes     [3]int                 `json:"ifIndexes"`
	WiFiSSID      string                 `json:"wifiSSID"`
	WiFiID        string                 `json:"wifiID"`
	WiFiIPv4      string                 `json:"wifiIPv4"`
	MainRoutes    string                 `json:"mainRoutes"`
	Rules         string                 `json:"rules"`
}

func (privateOwnedDataJournal) String() string     { return "[private owned data journal]" }
func (v privateOwnedDataJournal) GoString() string { return v.String() }

func ownedJournalReusable(raw []byte) bool {
	if len(raw) == 0 || len(raw) > maximumOwnedDataJournal {
		return false
	}
	var state privateOwnedDataJournal
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&state) != nil || decoder.Decode(new(any)) != io.EOF || state.Schema != 1 || state.ObservedAt.IsZero() {
		return false
	}
	if state.Phase != "complete" {
		return false
	}
	// A finite probe is never resumed by allocating a fresh CID and using a
	// remembered handle. CID lifetime and own-handle ownership are inseparable.
	return state.Result.CleanupVerified && state.Result.BaselinePreserved && state.Result.ProfilesPreserved
}

func writeOwnedDataJournal(root *os.Root, state privateOwnedDataJournal, owner func(os.FileInfo, os.FileMode) error) error {
	state.ObservedAt = time.Now().UTC()
	data, e := json.Marshal(state)
	if e != nil || len(data) > maximumOwnedDataJournal {
		return errors.New("OWNED_DATA_JOURNAL_INVALID")
	}
	// The directory has one exclusive flock holder, and an old temporary file
	// is not reused or silently removed after a crashed writer.
	file, e := root.OpenFile("state.next", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("OWNED_DATA_JOURNAL_CREATE_FAILED")
	}
	defer root.Remove("state.next")
	info, e := file.Stat()
	if e == nil {
		e = owner(info, 0600)
	}
	if e == nil {
		_, e = file.Write(data)
	}
	if e == nil {
		e = file.Sync()
	}
	closeErr := file.Close()
	if e != nil || closeErr != nil {
		return errors.New("OWNED_DATA_JOURNAL_WRITE_FAILED")
	}
	if info, e := root.Lstat("state.json"); e == nil {
		if !info.Mode().IsRegular() || owner(info, 0600) != nil {
			return errors.New("OWNED_DATA_JOURNAL_OWNER_INVALID")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return errors.New("OWNED_DATA_JOURNAL_UNAVAILABLE")
	}
	if e := root.Rename("state.next", "state.json"); e != nil {
		return errors.New("OWNED_DATA_JOURNAL_PUBLISH_FAILED")
	}
	if syncProfileRecoveryDirectory(root) != nil {
		return errors.New("OWNED_DATA_JOURNAL_SYNC_FAILED")
	}
	return nil
}
