package nasbackup

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func pointer[T any](value T) *T { return &value }
func targetPatch() TargetChange {
	v := testInput()
	return TargetChange{Protocol: pointer("smb"), Host: &v.Host, Share: &v.Share, Directory: &v.Directory, Username: v.Username, Password: v.Password}
}
func TestPoliciesKeepKindsRoutesAndCredentialsIndependent(t *testing.T) {
	p := DefaultPolicies()
	if p.View().Policies["video"].Enabled || p.View().Policies["event"].Targets["tailscale"].Configured {
		t.Fatal("unconfigured policy enabled")
	}
	p, err := p.Changed(0, PoliciesChange{map[string]PolicyChange{"video": {Enabled: pointer(true), Route: pointer("wifi"), Timing: pointer("ssid"), SSID: pointer("Fixture LAN"), Targets: map[string]TargetChange{"wifi": targetPatch()}}}})
	if err != nil {
		t.Fatal(err)
	}
	if p.View().Policies["event"].Enabled || p.View().Policies["event"].Targets["wifi"].Configured || p.View().Policies["video"].Targets["tailscale"].Configured {
		t.Fatal("kind/route settings leaked into another target")
	}
	p, err = p.Changed(1, PoliciesChange{map[string]PolicyChange{"video": {Enabled: pointer(false), Route: pointer("tailscale"), Targets: map[string]TargetChange{"tailscale": targetPatch()}}}})
	if err != nil {
		t.Fatal(err)
	}
	p, err = p.Changed(2, PoliciesChange{map[string]PolicyChange{"video": {Route: pointer("wifi"), Targets: map[string]TargetChange{"wifi": {Directory: pointer("private/new-folder"), Username: "", Password: ""}}}}})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := p.Target("video", "wifi")
	if err != nil || cfg.username != "fixture-user" || cfg.password != "fixture-secret" || cfg.directory != "private/new-folder" {
		t.Fatal("empty credential input did not preserve secrets", err)
	}
	other, _ := p.Target("video", "tailscale")
	if other.directory != "private/ncc" {
		t.Fatal("switching routes lost target configuration")
	}
	filename := filepath.Join(t.TempDir(), "policies.json")
	if err = SavePolicies(filename, p); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPolicies(filename)
	if err != nil || loaded.Revision() != 3 {
		t.Fatal("private policy roundtrip", err)
	}
	raw, _ := json.Marshal(loaded)
	text := string(raw) + fmt.Sprintf("%v %#v", loaded, loaded)
	for _, secret := range []string{"fixture-secret", "fixture-user"} {
		if strings.Contains(text, secret) {
			t.Fatal("policy credentials escaped")
		}
	}
}
func TestPoliciesRejectIncompleteSelectedTargetsAndBadRevision(t *testing.T) {
	p := DefaultPolicies()
	for _, change := range []PoliciesChange{{map[string]PolicyChange{"video": {Enabled: pointer(true)}}}, {map[string]PolicyChange{"parking": {Enabled: pointer(true)}}}, {map[string]PolicyChange{"event": {Route: pointer("cellular")}}}, {map[string]PolicyChange{"event": {Timing: pointer("other")}}}, {map[string]PolicyChange{"event": {Targets: map[string]TargetChange{"wifi": {Protocol: pointer("ftp")}}}}}} {
		if _, err := p.Changed(0, change); err == nil {
			t.Fatal("invalid/incomplete policy accepted")
		}
	}
	if _, err := p.Changed(1, PoliciesChange{map[string]PolicyChange{"video": {Enabled: pointer(false)}}}); err == nil || err.Error() != "NAS_REVISION_CONFLICT" {
		t.Fatal("stale revision accepted")
	}
	if _, err := p.Changed(0, PoliciesChange{map[string]PolicyChange{"video": {Enabled: pointer(true), Timing: pointer("ssid"), Targets: map[string]TargetChange{"wifi": targetPatch()}}}}); err == nil || err.Error() != "NAS_SSID_REQUIRED" {
		t.Fatal("empty SSID condition accepted")
	}
}
func TestLegacyTargetsRemainDisabledAndTailnetUnconfigured(t *testing.T) {
	cfg, err := NewConfig(testInput())
	if err != nil {
		t.Fatal(err)
	}
	p := LegacyPolicies(cfg)
	for _, kind := range []string{"video", "event"} {
		v := p.View().Policies[kind]
		if v.Enabled || !v.Targets["wifi"].Configured || v.Targets["tailscale"].Configured {
			t.Fatal("legacy migration invented automatic tailnet configuration")
		}
	}
}
