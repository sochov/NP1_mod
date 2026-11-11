package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func tailscaleFixture(host string) map[string]any {
	return map[string]any{"BackendState": "Running", "Self": map[string]any{"ID": "fixture-node", "HostName": host, "DNSName": host + ".fixture.ts.net.", "UserID": json.Number("9007199254740993"), "Online": true}, "User": map[string]any{"9007199254740993": map[string]any{"LoginName": "owner@example.invalid", "DisplayName": "Fixture Owner"}, "11": map[string]any{"LoginName": "other@example.invalid"}}}
}
func TestTailscaleIdentityUsesOnlySelfOwnerAndCanonicalMachineName(t *testing.T) {
	v := tailscaleFixture("np1-fixture")
	self := v["Self"].(map[string]any)
	self["DNSName"] = "registered-name.fixture.ts.net."
	data := tailscaleObservation(v).Data.(map[string]any)
	if data["accountLogin"] != "owner@example.invalid" || data["accountName"] != "Fixture Owner" || data["deviceName"] != "registered-name" || data["hostName"] != "np1-fixture" || data["nameWritable"] != true {
		t.Fatal(data)
	}
	delete(self, "UserID")
	data = tailscaleObservation(v).Data.(map[string]any)
	if data["accountLogin"] != "" {
		t.Fatal("guessed owner from another profile")
	}
}
func TestTailscaleRenameCASAndReadback(t *testing.T) {
	current := "old-name"
	sets := 0
	now := time.Now()
	d := tailscaleNameIO{status: func() (map[string]any, error) { return tailscaleFixture(current), nil }, set: func(_ context.Context, name string) error { sets++; current = name; return nil }, now: func() time.Time { return now }, sleep: func(d time.Duration) { now = now.Add(d) }}
	value, e := applyTailscaleName(context.Background(), tailscaleNameRequest{"old-name", "new-name"}, d)
	if e != nil || sets != 1 || value.Data.(map[string]any)["deviceName"] != "new-name" {
		t.Fatal(value, e, sets)
	}
	_, e = applyTailscaleName(context.Background(), tailscaleNameRequest{"old-name", "another-name"}, d)
	if p, ok := e.(*tailscaleNameProblem); !ok || p.status != 409 || sets != 1 {
		t.Fatal(e, sets)
	}
	_, e = applyTailscaleName(context.Background(), tailscaleNameRequest{"new-name", "new-name"}, d)
	if e != nil || sets != 1 {
		t.Fatal("unchanged name executed command")
	}
}
func TestTailscaleRenameRejectsInvalidNamesAndUnverifiedResult(t *testing.T) {
	for _, name := range []string{"", "UpperCase", "-start", "end-", "with space", "a.b", "--reset", "日本語", "a\nname", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
		if validTailscaleHostName(name) {
			t.Fatalf("invalid accepted %q", name)
		}
	}
	for _, name := range []string{"n", "np1-control", "a-1"} {
		if !validTailscaleHostName(name) {
			t.Fatal(name)
		}
	}
	now := time.Now()
	d := tailscaleNameIO{status: func() (map[string]any, error) { return tailscaleFixture("old"), nil }, set: func(context.Context, string) error { return nil }, now: func() time.Time { return now }, sleep: func(dt time.Duration) { now = now.Add(dt) }}
	_, e := applyTailscaleName(context.Background(), tailscaleNameRequest{"old", "new"}, d)
	if p, ok := e.(*tailscaleNameProblem); !ok || p.code != "TAILSCALE_RENAME_NOT_CONFIRMED" {
		t.Fatal(e)
	}
	d.set = func(context.Context, string) error { return errors.New("command failed") }
	_, e = applyTailscaleName(context.Background(), tailscaleNameRequest{"old", "new"}, d)
	if p, ok := e.(*tailscaleNameProblem); !ok || p.status != 503 {
		t.Fatal(e)
	}
}

func TestTailscaleRenameWaitsForRegisteredName(t *testing.T) {
	calls := 0
	now := time.Now()
	d := tailscaleNameIO{status: func() (map[string]any, error) {
		calls++
		if calls == 1 {
			return tailscaleFixture("old"), nil
		}
		v := tailscaleFixture("new")
		if calls < 4 {
			v["Self"].(map[string]any)["DNSName"] = "old.fixture.ts.net."
		}
		return v, nil
	}, set: func(context.Context, string) error { return nil }, now: func() time.Time { return now }, sleep: func(dt time.Duration) { now = now.Add(dt) }}
	value, e := applyTailscaleName(context.Background(), tailscaleNameRequest{"old", "new"}, d)
	if e != nil || calls != 4 || value.Data.(map[string]any)["deviceName"] != "new" {
		t.Fatal(value, e, calls)
	}
}
