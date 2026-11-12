package otapolicy

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
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
