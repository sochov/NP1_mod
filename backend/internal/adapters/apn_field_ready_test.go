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

func TestAPNFieldReadyRequiresPrivateExplicitValidationReceipt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "field-ready.json")
	now := time.Now().UTC()
	if apnFieldReadyAt(path, now) {
		t.Fatal("missing receipt treated as ready")
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("readiness check created a receipt")
	}
	valid := func() apnFieldReceipt {
		return apnFieldReceipt{Version: 1, Protocol: "np1-native-apn-profile1-v1", VerifiedAt: now, DriverVersion: modem.GobiNetDriverVersion, ModuleSHA256: modem.GobiNetModuleSHA256, Profile1Only: true}
	}
	write := func(v apnFieldReceipt) {
		t.Helper()
		raw, _ := json.Marshal(v)
		if e := os.WriteFile(path, raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write(valid())
	if !apnFieldReadyAt(path, now) {
		t.Fatal("valid receipt rejected")
	}
	for _, change := range []func(*apnFieldReceipt){
		func(v *apnFieldReceipt) { v.Version = 2 }, func(v *apnFieldReceipt) { v.Protocol = "unknown" },
		func(v *apnFieldReceipt) { v.DriverVersion = "unknown" }, func(v *apnFieldReceipt) { v.ModuleSHA256 = strings.Repeat("0", 64) },
		func(v *apnFieldReceipt) { v.VerifiedAt = now.Add(2 * time.Minute) }, func(v *apnFieldReceipt) { v.VerifiedAt = time.Time{} }, func(v *apnFieldReceipt) { v.Profile1Only = false },
	} {
		v := valid()
		change(&v)
		write(v)
		if apnFieldReadyAt(path, now) {
			t.Fatal("invalid receipt accepted")
		}
	}
	for _, raw := range []string{`{"version":1,"unexpected":true}`, strings.Repeat("x", 1025), `{} {}`} {
		os.WriteFile(path, []byte(raw), 0600)
		if apnFieldReadyAt(path, now) {
			t.Fatal("malformed receipt accepted")
		}
	}
	write(valid())
	link := filepath.Join(dir, "link.json")
	if os.Symlink(path, link) == nil && apnFieldReadyAt(link, now) {
		t.Fatal("symlink receipt accepted")
	}
}
