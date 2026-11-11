package adapters

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/nasbackup"
)

func automaticController(t *testing.T, kinds ...string) *nasBackupController {
	t.Helper()
	c := newNASBackupController(t.TempDir())
	on := true
	host, share, dir, protocol := "nas.example.invalid", "clips", "np1", "smb"
	changes := map[string]nasbackup.PolicyChange{}
	for _, kind := range kinds {
		changes[kind] = nasbackup.PolicyChange{Enabled: &on, Targets: map[string]nasbackup.TargetChange{"wifi": {Protocol: &protocol, Host: &host, Share: &share, Directory: &dir, Username: "fixture", Password: "fixture"}}}
	}
	if len(changes) > 0 {
		if _, err := c.savePolicies(0, nasbackup.PoliciesChange{Policies: changes}); err != nil {
			t.Fatal(err)
		}
	}
	c.version = func(string) (string, error) { return "fixture-version", nil }
	return c
}
func TestNASAutomaticDisabledOrWrongSSIDNeverOpensRecording(t *testing.T) {
	c := automaticController(t)
	c.catalog = func(context.Context) (RecordingCatalog, error) {
		t.Fatal("disabled policies scanned SD")
		return RecordingCatalog{}, nil
	}
	c.automaticOnce(context.Background())
	c = automaticController(t, "video")
	c.catalog = func(context.Context) (RecordingCatalog, error) {
		return RecordingCatalog{Clips: []RecordingClip{{ID: "v", Kind: "normal"}}}, nil
	}
	c.prepare = func(context.Context, nasRouteRule, nasbackup.TargetView) (*nasRouteBinding, error) {
		return nil, errors.New("NAS_SSID_NOT_CONNECTED")
	}
	c.open = func(context.Context, string) (nasRecording, error) {
		t.Fatal("wrong SSID opened recording")
		return nasRecording{}, nil
	}
	c.automaticOnce(context.Background())
	if c.automatic["video"].Code != "NAS_SSID_NOT_CONNECTED" {
		t.Fatal(c.automatic)
	}
}
func TestNASAutomaticPrioritizesEventsAndPreservesCatalog(t *testing.T) {
	c := automaticController(t, "video", "event")
	clips := []RecordingClip{{ID: "old-video", Kind: "normal", StartAt: time.Unix(1, 0)}, {ID: "urgent-event", Kind: "event", StartAt: time.Unix(0, 0)}, {ID: "new-video", Kind: "normal", StartAt: time.Unix(2, 0)}}
	c.catalog = func(context.Context) (RecordingCatalog, error) { return RecordingCatalog{Clips: clips}, nil }
	id := nasRouteIdentity{iface: "wlan0", index: 1, source: netip.MustParseAddr("192.0.2.2"), boot: "fixture", ssid: "home", id: "1"}
	c.prepare = func(ctx context.Context, rule nasRouteRule, target nasbackup.TargetView) (*nasRouteBinding, error) {
		return &nasRouteBinding{rule: rule, identity: id, dependencies: nasRouteDependencies{snapshot: func(context.Context, nasRouteRule, netip.Addr) (nasRouteIdentity, error) { return id, nil }}}, nil
	}
	var opened []string
	c.open = func(_ context.Context, id string) (nasRecording, error) {
		opened = append(opened, id)
		return nasRecording{}, errors.New("fixture-no-file")
	}
	c.automaticOnce(context.Background())
	if !reflect.DeepEqual(opened, []string{"urgent-event", "new-video", "old-video"}) {
		t.Fatal(opened)
	}
	if clips[0].ID != "old-video" {
		t.Fatal("shared playback catalog was reordered")
	}
}
func TestNASPolicySaveResumesOnlyChangedKind(t *testing.T) {
	c := automaticController(t, "video", "event")
	c.paused["video"], c.paused["event"] = true, true
	c.nextTry["video"] = time.Now().Add(time.Hour)
	off := false
	if _, err := c.savePolicies(1, nasbackup.PoliciesChange{Policies: map[string]nasbackup.PolicyChange{"video": {Enabled: &off}}}); err != nil {
		t.Fatal(err)
	}
	if c.paused["video"] || !c.paused["event"] || !c.nextTry["video"].IsZero() {
		t.Fatal("changed kind not resumed independently")
	}
}
