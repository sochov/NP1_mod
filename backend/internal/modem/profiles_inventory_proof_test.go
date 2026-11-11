package modem

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func profile2ErrorFixture(t *testing.T, extended uint16) ProfileRecovery {
	t.Helper()
	r := syntheticRecovery(t)
	frame := statusFrame(6, 0x2b, map[byte][]byte{0xe0: {byte(extended), byte(extended >> 8)}})
	for pos := 7; pos < len(frame); {
		size := int(binary.LittleEndian.Uint16(frame[pos+1 : pos+3]))
		if frame[pos] == 2 {
			binary.LittleEndian.PutUint16(frame[pos+3:], 1)
			binary.LittleEndian.PutUint16(frame[pos+5:], 81)
		}
		pos += 3 + size
	}
	p, err := parseFailedSavedProfile(frame, 2)
	if err != nil {
		t.Fatal(err)
	}
	r.profiles[1] = p
	return r
}

func TestProfile2InvalidNumberRequiresSameCIDListAndSurvivesPrivateReadback(t *testing.T) {
	r := profile2ErrorFixture(t, 5)
	if r.Summary().Profiles[1].AbsenceConfirmed {
		t.Fatal("invalid profile number alone became absence")
	}
	list := append([]byte{1, 0, 1, 20}, []byte("private-profile-name")...)
	frame := statusFrame(7, 0x2a, map[byte][]byte{1: list})
	r, err := r.withSameCIDInventory(frame)
	if err != nil {
		t.Fatal(err)
	}
	summary := r.Summary()
	if !summary.InventoryVerified || !summary.Profiles[1].AbsenceConfirmed || summary.Profiles[1].AbsenceSource != "profile-get-and-same-cid-inventory" || summary.NativeFieldsComplete || !summary.Profiles[0].NativeFieldsComplete {
		t.Fatal("combined absence evidence missing or invented native profile")
	}
	bundle, err := r.bundle()
	if err != nil || bundle.SchemaVersion != 2 || !bytes.Equal(bundle.InventoryFrame, frame) {
		t.Fatal("private schema did not retain raw proof", err)
	}
	directory := t.TempDir()
	owner := func(os.FileInfo, os.FileMode) error { return nil }
	saved, err := saveProfileRecoveryAt(directory, r, time.Now().UTC(), bytes.NewReader(make([]byte, 8)), owner)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadProfileRecoveryAt(directory, saved.BackupFile, time.Now().UTC(), owner)
	if err != nil || !reflect.DeepEqual(loaded.Summary(), summary) || !bytes.Equal(loaded.inventoryFrame, frame) {
		t.Fatal("private schema readback changed proof", err)
	}
	public, _ := json.Marshal(r)
	text := string(public) + fmt.Sprintf("%+v %#v", r, r)
	for _, secret := range []string{"private.example", "secret-user", "secret-password", "private-profile-name", "sameCIDInventoryFrame"} {
		if strings.Contains(text, secret) {
			t.Fatal("private payload escaped summary")
		}
	}
	var wire bytes.Buffer
	if r.writeNativeProfiles(&wire) == nil || wire.Len() != 0 {
		t.Fatal("absent profile2 invented as restorable native profile")
	}
}

func TestProfile2ListPresenceContradictionsAndDifferentErrorsRemainUnknown(t *testing.T) {
	for _, tc := range []struct {
		extended uint16
		list     []byte
		absent   bool
	}{
		{5, []byte{1, 0, 1, 0}, true},
		{5, []byte{2, 0, 1, 0, 0, 2, 0}, false},
		{5, []byte{2, 0, 1, 0, 0, 3, 0}, false},
		{5, []byte{0}, false},
		{1002, []byte{1, 0, 1, 0}, false},
		{1003, []byte{2, 0, 1, 0, 0, 2, 0}, false},
	} {
		r, err := profile2ErrorFixture(t, tc.extended).withSameCIDInventory(statusFrame(7, 0x2a, map[byte][]byte{1: tc.list}))
		if err != nil || r.Summary().Profiles[1].AbsenceConfirmed != tc.absent {
			t.Fatal("inventory contradiction or generic error accepted", err)
		}
	}
	r := profile2ErrorFixture(t, 5)
	for _, frame := range [][]byte{nil, statusFrame(7, 0x2a, map[byte][]byte{1: {1, 1, 1, 0}}), statusFrame(6, 0x2a, map[byte][]byte{1: {1, 0, 1, 0}})} {
		if _, err := r.withSameCIDInventory(frame); err == nil {
			t.Fatal("missing/wrong-family/wrong-transaction proof accepted")
		}
	}
}

func TestProfileInventoryPrivateSchemaRejectsMissingMixedOrMalformedProof(t *testing.T) {
	r, err := profile2ErrorFixture(t, 5).withSameCIDInventory(statusFrame(7, 0x2a, map[byte][]byte{1: {1, 0, 1, 0}}))
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := r.bundle()
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*privateProfileBundle){
		func(v *privateProfileBundle) { v.SchemaVersion = 1 },
		func(v *privateProfileBundle) { v.InventoryFrame = nil },
		func(v *privateProfileBundle) { v.InventoryFrame = []byte{2, 7, 0} },
		func(v *privateProfileBundle) { v.SchemaVersion = 3 },
	} {
		bad := bundle
		mutate(&bad)
		raw, _ := json.Marshal(bad)
		if _, err := decodeProfileRecovery(raw, time.Now().UTC()); err == nil {
			t.Fatal("mixed or malformed inventory schema accepted")
		}
	}
	legacy, err := profile2ErrorFixture(t, 5).bundle()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(legacy)
	loaded, err := decodeProfileRecovery(raw, time.Now().UTC())
	if err != nil || legacy.SchemaVersion != 1 || loaded.Summary().InventoryVerified || loaded.Summary().Profiles[1].AbsenceConfirmed {
		t.Fatal("legacy compatibility or conservative absence lost", err)
	}
}
