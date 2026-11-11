package modem

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

func TestOwnedJournalOnlyFullyCleanedProbeCanBeRepeated(t *testing.T) {
	s := privateOwnedDataJournal{Schema: 1, Phase: "complete", ObservedAt: time.Now().UTC(), Result: OwnedDataSessionResult{CleanupVerified: true, BaselinePreserved: true, ProfilesPreserved: true}}
	raw, _ := json.Marshal(s)
	if !ownedJournalReusable(raw) {
		t.Fatal("completed finite session rejected")
	}
	for _, phase := range []string{"prepared", "start-pending", "started", "settings", "stopped", "incomplete", ""} {
		changed := s
		changed.Phase = phase
		b, _ := json.Marshal(changed)
		if ownedJournalReusable(b) {
			t.Fatalf("incomplete phase %s permits a new Start", phase)
		}
	}
	for i := 0; i < 3; i++ {
		changed := s
		switch i {
		case 0:
			changed.Result.CleanupVerified = false
		case 1:
			changed.Result.BaselinePreserved = false
		case 2:
			changed.Result.ProfilesPreserved = false
		}
		b, _ := json.Marshal(changed)
		if ownedJournalReusable(b) {
			t.Fatal("missing recovery proof permits another session")
		}
	}
	if ownedJournalReusable(append(raw, []byte(` {"schema":1}`)...)) || ownedJournalReusable([]byte(`{"schema":1,"phase":"complete","unknown":true}`)) {
		t.Fatal("trailing/unknown schema accepted")
	}
}

func TestOwnedJournalAtomicReplacementAndOwnerRejection(t *testing.T) {
	root, e := os.OpenRoot(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	owner := func(os.FileInfo, os.FileMode) error { return nil }
	state := privateOwnedDataJournal{Schema: 1, Phase: "start-pending", ProcessID: 123, PacketHandle: 0, WiFiSSID: "private-fixture"}
	if e := writeOwnedDataJournal(root, state, owner); e != nil {
		t.Fatal(e)
	}
	state.Phase, state.PacketHandle = "started", 12345
	if e := writeOwnedDataJournal(root, state, owner); e != nil {
		t.Fatal(e)
	}
	raw, e := root.ReadFile("state.json")
	if e != nil {
		t.Fatal(e)
	}
	var got privateOwnedDataJournal
	if json.Unmarshal(raw, &got) != nil || got.Phase != "started" || got.PacketHandle != 12345 || got.WiFiSSID != "private-fixture" {
		t.Fatal("private durable state lost")
	}
	if _, e := root.Stat("state.next"); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("temporary file left after publication")
	}
	if e := writeOwnedDataJournal(root, state, func(os.FileInfo, os.FileMode) error { return errors.New("bad owner") }); e == nil {
		t.Fatal("untrusted file owner accepted")
	}
	after, _ := root.ReadFile("state.json")
	if string(after) != string(raw) {
		t.Fatal("failed write replaced valid prior state")
	}
	if e := root.WriteFile("state.next", []byte("old crashed writer"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := writeOwnedDataJournal(root, state, owner); e == nil {
		t.Fatal("crashed temporary writer silently reused")
	}
}

func TestOwnedProfilesAllFieldsAndInventoryMustStayIdentical(t *testing.T) {
	before := syntheticRecovery(t)
	before.inventoryFrame = []byte("same inventory fixture")
	after := before
	if !ownedProfilesIdentical(before, after) {
		t.Fatal("identical capture rejected")
	}
	after.profiles[0].raw = append([]byte(nil), before.profiles[0].raw...)
	after.profiles[0].raw[len(after.profiles[0].raw)-1] ^= 1
	if ownedProfilesIdentical(before, after) {
		t.Fatal("changed original TLV ignored")
	}
	after = before
	after.inventoryFrame = []byte("different")
	if ownedProfilesIdentical(before, after) {
		t.Fatal("inventory creation/deletion ignored")
	}
	after = before
	after.profiles[1].status = "failed"
	if ownedProfilesIdentical(before, after) {
		t.Fatal("profile presence change ignored")
	}
}
