package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func apnText(value string) *string { return &value }
func apnChanges() []APNProfileChange {
	return []APNProfileChange{{ID: "profile1", Enabled: true, APN: "example.apn", Auth: 1, Username: apnText("sample-user"), Password: apnText("sample-password")}, {ID: "profile2", APN: "", Auth: 0}}
}
func TestAPNPrivateSaveAndPublicSecretRedaction(t *testing.T) {
	store := NewAPNStore(privateTempDir(t))
	before, e := store.Read()
	if e != nil || before.Revision != 0 || len(before.Profiles) != 2 || before.AppliedRevision != nil {
		t.Fatal(before, e)
	}
	value, e := store.Save(0, apnChanges())
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(value)
	if strings.Contains(string(raw), "sample-user") || strings.Contains(string(raw), "sample-password") {
		t.Fatal("public APN leaked credential")
	}
	if !value.Profiles[0].UsernameConfigured || !value.Profiles[0].PasswordConfigured || value.AppliedRevision != nil {
		t.Fatal("saving falsely implies apply")
	}
	secret, e := store.ForApply(1)
	if e != nil || secret[0].Password != "sample-password" {
		t.Fatal("private helper cannot access saved credential", e)
	}
	raw, _ = json.Marshal(secret)
	if strings.Contains(string(raw), "sample-password") {
		t.Fatal("private helper type accidentally serializes secret")
	}
	changes := apnChanges()
	changes[0].Username = nil
	changes[0].Password = apnText("")
	if _, e = store.Save(1, changes); e != nil {
		t.Fatal(e)
	}
	secret, _ = store.ForApply(2)
	if secret[0].Password != "sample-password" || secret[0].Username != "sample-user" {
		t.Fatal("blank update erased credentials")
	}
	changes[0].Password = nil
	changes[0].ClearPassword = true
	if _, e = store.Save(2, changes); e != nil {
		t.Fatal(e)
	}
	secret, _ = store.ForApply(3)
	if secret[0].Password != "" {
		t.Fatal("explicit clear failed")
	}
}
func TestAPNRevisionAndApplyAreIndependent(t *testing.T) {
	s := NewAPNStore(privateTempDir(t))
	if _, e := s.Save(0, apnChanges()); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Save(0, apnChanges()); e == nil {
		t.Fatal("stale revision accepted")
	}
	if _, e := s.ForApply(0); e == nil {
		t.Fatal("stale apply snapshot accepted")
	}
	v, e := s.MarkApplied(1)
	if e != nil || v.AppliedRevision == nil || *v.AppliedRevision != 1 {
		t.Fatal(v, e)
	}
	v, e = s.Save(1, apnChanges())
	if e != nil || v.Revision != 2 || *v.AppliedRevision != 1 {
		t.Fatal("save relabeled old apply as current", e)
	}
}
func TestAPNRejectsUnsafeNativeTokensAndMalformedProfiles(t *testing.T) {
	for _, value := range []string{"line\nbreak", "a;reboot", "$(id)", "a b", "\"quoted\"", "a\\b", "a,b", "-option", "日本語"} {
		if validAPNCredential(value) {
			t.Fatal("unsafe credential accepted")
		}
	}
	for _, name := range []string{"", "a..b", "-a", "a-", "example/apn", strings.Repeat("a", 50)} {
		if validAPNName(name) {
			t.Fatal("invalid APN accepted")
		}
	}
	s := NewAPNStore(privateTempDir(t))
	for _, change := range [][]APNProfileChange{{{ID: "profile1"}}, {{ID: "profile1"}, {ID: "profile1"}}, {{ID: "profile1", Auth: 4}, {ID: "profile2"}}, {{ID: "profile1", Enabled: true}, {ID: "profile2"}}} {
		if _, e := s.Save(0, change); e == nil {
			t.Fatal("invalid profile accepted")
		}
	}
}
func TestAPNConfigSymlinkAndMultipleJSONAreRejected(t *testing.T) {
	dir := privateTempDir(t)
	s := NewAPNStore(dir)
	if _, e := s.Save(0, apnChanges()); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(dir, "config.json")
	b, _ := os.ReadFile(p)
	if e := os.WriteFile(p, append(b, []byte(" {}")...), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Read(); e == nil {
		t.Fatal("multiple JSON accepted")
	}
	linkDir := privateTempDir(t)
	if e := os.Symlink(p, filepath.Join(linkDir, "config.json")); e == nil {
		if _, e := NewAPNStore(linkDir).Read(); e == nil {
			t.Fatal("symlink credential file accepted")
		}
	}
}
