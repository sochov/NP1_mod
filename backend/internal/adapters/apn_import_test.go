package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/modem"
)

func apnSeedFixture(action func(modem.ConfiguredAPNProfile1, string, string) error) error {
	return action(modem.ConfiguredAPNProfile1{Status: "available", Source: "gobinet-wds-saved-profile", Current: false, ObservedAt: time.Now().UTC(), APN: "fixture.example", Auth: 3, UsernameConfigured: true, PasswordConfigured: true, Profile2Known: true, Profile2Absent: true}, "synthetic-user", "synthetic-secret")
}
func TestAPNSeedPrivateCredentialsPreservedAndPublicBooleansOnly(t *testing.T) {
	s := NewAPNStore(privateTempDir(t))
	seeded, e := s.seedFromRecovery(apnSeedFixture)
	if e != nil || !seeded {
		t.Fatal(seeded, e)
	}
	view, e := s.Read()
	if e != nil || view.Revision != 1 || !view.Profiles[0].Enabled || view.Profiles[0].APN != "fixture.example" || !view.Profiles[0].UsernameConfigured || !view.Profiles[0].PasswordConfigured || view.Profiles[1].Enabled || view.AppliedRevision != nil {
		t.Fatal(view, e)
	}
	public, _ := json.Marshal(view)
	if strings.Contains(string(public), "synthetic-user") || strings.Contains(string(public), "synthetic-secret") {
		t.Fatal("seed credential leaked")
	}
	changes := []APNProfileChange{{ID: "profile1", Enabled: true, APN: "edited.example", Auth: 3}, {ID: "profile2"}}
	if _, e = s.Save(1, changes); e != nil {
		t.Fatal(e)
	}
	secret, e := s.ForApply(2)
	if e != nil || secret[0].Username != "synthetic-user" || secret[0].Password != "synthetic-secret" {
		t.Fatal("editing APN discarded preserved credential", e)
	}
	seeded, e = s.seedFromRecovery(func(func(modem.ConfiguredAPNProfile1, string, string) error) error {
		t.Fatal("existing settings read recovery again")
		return nil
	})
	if e != nil || seeded {
		t.Fatal("existing owner config replaced", e)
	}
}
func TestAPNSeedNeverOverwritesRevisionZeroOrSeedsWithoutProof(t *testing.T) {
	s := NewAPNStore(privateTempDir(t))
	if e := s.write(apnDefaults()); e != nil {
		t.Fatal(e)
	}
	seeded, e := s.seedFromRecovery(func(func(modem.ConfiguredAPNProfile1, string, string) error) error {
		t.Fatal("revision zero owner config overwritten")
		return nil
	})
	if e != nil || seeded {
		t.Fatal(seeded, e)
	}
	for _, change := range []func(*modem.ConfiguredAPNProfile1){func(v *modem.ConfiguredAPNProfile1) { v.Current = true }, func(v *modem.ConfiguredAPNProfile1) { v.Profile2Known = false }, func(v *modem.ConfiguredAPNProfile1) { v.Profile2Absent = false }, func(v *modem.ConfiguredAPNProfile1) { v.Status = "stale" }} {
		s := NewAPNStore(privateTempDir(t))
		seeded, e = s.seedFromRecovery(func(action func(modem.ConfiguredAPNProfile1, string, string) error) error {
			return apnSeedFixture(func(v modem.ConfiguredAPNProfile1, user, password string) error {
				change(&v)
				return action(v, user, password)
			})
		})
		if e == nil || seeded {
			t.Fatal("unverified profile seeded")
		}
		if _, e = os.Stat(filepath.Join(s.directory, "config.json")); !os.IsNotExist(e) {
			t.Fatal("failed seed created config")
		}
	}
}
