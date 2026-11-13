package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOwnerBackupRoundTrip(t *testing.T) {
	root := t.TempDir()
	defer func(old string) { ownerPrivateRoot = old }(ownerPrivateRoot)
	ownerPrivateRoot = root
	os.MkdirAll(filepath.Join(root, "led"), 0o700)
	os.MkdirAll(filepath.Join(root, "ap"), 0o700)
	os.WriteFile(filepath.Join(root, "led", "config.json"), []byte(`{"mode":"rainbow"}`), 0o600)
	os.WriteFile(filepath.Join(root, "ap", "autostart"), []byte("enabled\n"), 0o600)
	files, e := OwnerBackupExport()
	if e != nil || len(files) != 2 {
		t.Fatalf("export %v %v", files, e)
	}
	os.WriteFile(filepath.Join(root, "led", "config.json"), []byte(`{"mode":"off"}`), 0o600)
	changed, units, e := OwnerBackupRestore(files, true)
	if e != nil || len(changed) != 1 || changed[0] != "led/config.json" || len(units) != 1 {
		t.Fatalf("dry run %v %v %v", changed, units, e)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "led", "config.json")); string(b) != `{"mode":"off"}` {
		t.Fatal("dry run wrote a file")
	}
	if _, _, e = OwnerBackupRestore(files, false); e != nil {
		t.Fatal(e)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "led", "config.json")); string(b) != `{"mode":"rainbow"}` {
		t.Fatalf("not restored: %s", b)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "led", "config.json.before-restore")); string(b) != `{"mode":"off"}` {
		t.Fatal("previous copy not kept")
	}
}

func TestOwnerBackupRejectsUnknownOrInvalidFiles(t *testing.T) {
	root := t.TempDir()
	defer func(old string) { ownerPrivateRoot = old }(ownerPrivateRoot)
	ownerPrivateRoot = root
	for name, raw := range map[string]string{"../etc/passwd": `{}`, "led/config.json": `[1]`, "ap/autostart": `"yes"`} {
		if _, _, e := OwnerBackupRestore(map[string]json.RawMessage{name: json.RawMessage(raw)}, true); e == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
