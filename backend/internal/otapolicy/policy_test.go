package otapolicy

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixture(t *testing.T) store {
	t.Helper()
	base := t.TempDir()
	flag := filepath.Join(base, "stock", "SYSTEM", "Update", "no_ota_download")
	private := filepath.Join(base, "owner", "private")
	for _, p := range []string{filepath.Dir(flag), private} {
		if e := os.MkdirAll(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	info, err := os.Stat(base)
	if err != nil {
		t.Fatal(err)
	}
	var owner snapshot
	identify(info, &owner)
	return store{flag: flag, state: filepath.Join(private, "ota-policy", "ledger.json"), verify: func() bool { return true }, uid: owner.UID, gid: owner.GID}
}
func TestInstallDisablesAndUpgradePreservesExplicitEnable(t *testing.T) {
	s := fixture(t)
	v, e := s.run("install", "", false)
	if e != nil || !v.Disabled || !v.Writable {
		t.Fatalf("install: %+v %v", v, e)
	}
	v, e = s.run("set", v.Token, false)
	if e != nil || v.Disabled {
		t.Fatalf("enable: %+v %v", v, e)
	}
	v, e = s.run("install", "", false)
	if e != nil || v.Disabled {
		t.Fatalf("upgrade overwrote choice: %+v %v", v, e)
	}
	v, e = s.run("uninstall", "", false)
	if e != nil || v.Disabled || v.Managed {
		t.Fatalf("uninstall: %+v %v", v, e)
	}
	if _, e = os.Stat(s.flag); !os.IsNotExist(e) {
		t.Fatal("original absence not restored")
	}
}
func TestOriginalFileBackedUpAndRestoredWithoutLosingContents(t *testing.T) {
	s := fixture(t)
	original := []byte("owner-supplied flag\n")
	if e := os.WriteFile(s.flag, original, 0644); e != nil {
		t.Fatal(e)
	}
	before, _ := capture(s.flag)
	v, e := s.run("install", "", false)
	if e != nil || !v.Disabled {
		t.Fatal(e)
	}
	unchanged, _ := capture(s.flag)
	if !equal(before, unchanged) {
		t.Fatal("install modified existing flag")
	}
	if _, e = s.run("set", v.Token, false); e != nil {
		t.Fatal(e)
	}
	v, e = s.run("rollback", "", false)
	if e != nil || !v.Disabled {
		t.Fatal(e)
	}
	after, _ := capture(s.flag)
	if string(after.Data) != string(original) || after.Mode != before.Mode || after.UID != before.UID || after.GID != before.GID {
		t.Fatal("original data/metadata not restored")
	}
}
func TestForeignChangeAndStaleTokenFailClosed(t *testing.T) {
	s := fixture(t)
	v, e := s.run("install", "", false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.run("set", "stale", false); !errors.Is(e, ErrConflict) {
		t.Fatal("stale token accepted")
	}
	if e = os.WriteFile(s.flag, []byte("changed-by-another-actor"), 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = s.run("set", v.Token, false); !errors.Is(e, ErrConflict) {
		t.Fatal("foreign change deleted")
	}
	if _, e = s.run("uninstall", "", false); !errors.Is(e, ErrConflict) {
		t.Fatal("uninstall removed foreign flag")
	}
	b, _ := os.ReadFile(s.flag)
	if string(b) != "changed-by-another-actor" {
		t.Fatal("foreign flag altered")
	}
}
func TestNoMarkerOrUnknownFirmwareCannotEnable(t *testing.T) {
	s := fixture(t)
	if e := os.WriteFile(s.flag, nil, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e := s.run("enable", "", false); !errors.Is(e, ErrUnmanaged) {
		t.Fatal("unmarked flag removed")
	}
	s.verify = func() bool { return false }
	if _, e := s.run("install", "", false); e == nil {
		t.Fatal("unknown firmware accepted")
	}
	if _, e := os.Stat(s.flag); e != nil {
		t.Fatal("original flag removed")
	}
}

// protectedFixture records the immutable attribute in memory, as the device ioctl would.
func protectedFixture(t *testing.T) (store, map[string]bool) {
	s := fixture(t)
	marks := map[string]bool{}
	s.protect = func(p string) error { marks[p] = true; return nil }
	s.unprotect = func(p string) error { delete(marks, p); return nil }
	s.protected = func(p string) bool { return marks[p] }
	return s, marks
}
func TestCreatedFlagIsProtectedAndLiftedOnlyToEnable(t *testing.T) {
	s, marks := protectedFixture(t)
	v, e := s.run("install", "", false)
	if e != nil || !v.Disabled || !v.Protected || !marks[s.flag] {
		t.Fatalf("install should protect the new flag: %+v %v", v, e)
	}
	v, e = s.run("set", v.Token, false)
	if e != nil || v.Disabled || v.Protected || marks[s.flag] {
		t.Fatalf("enable should lift protection and remove the flag: %+v %v", v, e)
	}
	v, e = s.run("set", v.Token, true)
	if e != nil || !v.Disabled || !v.Protected {
		t.Fatalf("disable again should protect: %+v %v", v, e)
	}
}
func TestEnforceRecreatesADeletedFlagAndCountsIt(t *testing.T) {
	s, marks := protectedFixture(t)
	at := time.Date(2026, 11, 13, 5, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return at }
	if _, e := s.run("install", "", false); e != nil {
		t.Fatal(e)
	}
	// Simulate the stock Update user unlinking the flag in its world-writable directory.
	delete(marks, s.flag)
	if e := os.Remove(s.flag); e != nil {
		t.Fatal(e)
	}
	v, e := s.run("status", "", false)
	if e != nil || v.Disabled || v.Managed {
		t.Fatalf("a deleted flag must show as not stopping downloads: %+v %v", v, e)
	}
	v, e = s.run("enforce", "", false)
	if e != nil || !v.Disabled || !v.Managed || !v.Protected || v.Heals != 1 || v.LastHeal == nil || !v.LastHeal.Equal(at) {
		t.Fatalf("enforce should recreate and protect the flag: %+v %v", v, e)
	}
	v, e = s.run("enforce", "", false)
	if e != nil || v.Heals != 1 {
		t.Fatalf("a present flag needs no heal: %+v %v", v, e)
	}
}
func TestEnforceRespectsAnExplicitEnableAndForeignFlags(t *testing.T) {
	s, marks := protectedFixture(t)
	v, e := s.run("install", "", false)
	if e != nil {
		t.Fatal(e)
	}
	if v, e = s.run("set", v.Token, false); e != nil || v.Disabled {
		t.Fatal(e)
	}
	if v, e = s.run("enforce", "", false); e != nil || v.Disabled || v.Heals != 0 {
		t.Fatalf("enforce must not undo the owner's re-enable: %+v %v", v, e)
	}
	// Re-stop, then let someone replace the flag with their own file: it still stops
	// downloads, so enforce neither deletes nor claims it.
	if v, e = s.run("set", v.Token, true); e != nil {
		t.Fatal(e)
	}
	delete(marks, s.flag)
	os.Remove(s.flag)
	if e = os.WriteFile(s.flag, []byte("other\n"), 0644); e != nil {
		t.Fatal(e)
	}
	v, e = s.run("enforce", "", false)
	if e != nil || !v.Disabled || v.Managed || v.Heals != 0 || marks[s.flag] {
		t.Fatalf("a foreign flag should be left alone: %+v %v", v, e)
	}
	if b, _ := os.ReadFile(s.flag); string(b) != "other\n" {
		t.Fatal("foreign flag changed")
	}
}
func TestEnforceOnUnmanagedOrUnknownFirmwareChangesNothing(t *testing.T) {
	s, _ := protectedFixture(t)
	if _, e := s.run("enforce", "", false); !errors.Is(e, ErrUnmanaged) {
		t.Fatalf("unmanaged enforce: %v", e)
	}
	if _, e := s.run("install", "", false); e != nil {
		t.Fatal(e)
	}
	os.Remove(s.flag)
	s.verify = func() bool { return false }
	if _, e := s.run("enforce", "", false); e == nil {
		t.Fatal("unknown firmware must not be modified")
	}
	if _, e := os.Stat(s.flag); !os.IsNotExist(e) {
		t.Fatal("flag written on unknown firmware")
	}
}
