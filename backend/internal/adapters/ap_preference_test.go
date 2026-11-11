package adapters

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/config"
)

func TestAPPreferenceLegacyFileDefaultsAndRejectsUnknownPreference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	legacy := `{"revision":4,"enabled":true,"ssid":"Synthetic LAN","passphrase":"synthetic-password","standaloneFallback":true}`
	if e := os.WriteFile(path, []byte(legacy), 0600); e != nil {
		t.Fatal(e)
	}
	previous, e := apLoadConfig(path)
	if e != nil || previous.Preference != "cellular" {
		t.Fatal("legacy default", e)
	}
	for _, value := range []string{"", "auto", "mobile", "WIFI", "wlan0"} {
		raw := strings.TrimSuffix(legacy, "}") + `,"preference":"` + value + `"}`
		if e := os.WriteFile(path, []byte(raw), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e := apLoadConfig(path); e == nil {
			t.Fatal("unknown preference accepted")
		}
	}
	if e := os.WriteFile(path, []byte(legacy+" {}"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := apLoadConfig(path); e == nil {
		t.Fatal("trailing config accepted")
	}
}

func TestAPPreferencePartialUpdatePreservesRadioAndSecretAndCAS(t *testing.T) {
	previous := apConfiguration{Revision: 4, Enabled: true, SSID: "Synthetic LAN", Passphrase: "synthetic-secret", StandaloneFallback: true, Preference: "cellular"}
	var change APChange
	if e := json.Unmarshal([]byte(`{"preference":"wifi"}`), &change); e != nil {
		t.Fatal(e)
	}
	next, e := apNextConfiguration(previous, 4, change)
	if e != nil || next.Revision != 5 || next.Preference != "wifi" || !apSameRadioConfiguration(previous, next) || next.Passphrase != previous.Passphrase {
		t.Fatal("partial settings changed radio/secret", e)
	}
	if !apPreferenceOnlyChange(previous, next) {
		t.Fatal("preference change would require AP restart")
	}
	unchanged := previous
	unchanged.Revision++
	if !apPreferenceOnlyChange(previous, unchanged) {
		t.Fatal("same preference save would restart AP")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if e := config.Atomic(path, next); e != nil {
		t.Fatal(e)
	}
	readback, e := apLoadConfig(path)
	if e != nil || readback != next {
		t.Fatal("private persistence/readback", e)
	}
	public, _ := json.Marshal(apPublic(readback))
	if strings.Contains(string(public), previous.Passphrase) || !strings.Contains(string(public), `"preference":"wifi"`) {
		t.Fatal("secret exposure or missing preference")
	}
	if _, e := apNextConfiguration(previous, 3, change); e == nil || e.Error() != "REVISION_CONFLICT" {
		t.Fatal("stale preference save accepted")
	}
	for _, raw := range []string{`{}`, `{"preference":""}`, `{"preference":"other"}`} {
		var input APChange
		json.Unmarshal([]byte(raw), &input)
		if _, e := apNextConfiguration(previous, 4, input); e == nil {
			t.Fatal("invalid preference change accepted")
		}
	}
}

func TestAPPreferenceLegacyFullRequestsAndExplicitDisableStillWork(t *testing.T) {
	previous := apConfiguration{Revision: 1, Enabled: true, SSID: "Synthetic LAN", Passphrase: "synthetic-secret", StandaloneFallback: true, Preference: "wifi"}
	for _, raw := range []string{`{"enabled":true,"ssid":"New Synthetic LAN"}`, `{"enabled":true,"ssid":"New Synthetic LAN","passphrase":""}`} {
		var input APChange
		json.Unmarshal([]byte(raw), &input)
		next, e := apNextConfiguration(previous, 1, input)
		if e != nil || !next.Enabled || next.SSID != "New Synthetic LAN" || next.Passphrase != previous.Passphrase || next.Preference != "wifi" || apPreferenceOnlyChange(previous, next) {
			t.Fatal("legacy full save failed", e)
		}
	}
	var input APChange
	json.Unmarshal([]byte(`{"enabled":false}`), &input)
	next, e := apNextConfiguration(previous, 1, input)
	if e != nil || next.Enabled || next.SSID != previous.SSID || next.Passphrase != previous.Passphrase {
		t.Fatal("partial disable failed", e)
	}
}

func TestAPPreferenceHotReloadAndAckDoNotAcceptRadioChangesOrStaleWorker(t *testing.T) {
	now := time.Now()
	previous := apConfiguration{Revision: 4, Enabled: true, SSID: "Synthetic LAN", Passphrase: "synthetic-secret", StandaloneFallback: true, Preference: "cellular"}
	next := previous
	next.Revision = 5
	next.Preference = "wifi"
	if !apAcceptHotConfiguration(previous, next) {
		t.Fatal("hot preference rejected")
	}
	radio := next
	radio.SSID = "Changed"
	if apAcceptHotConfiguration(previous, radio) {
		t.Fatal("radio configuration hot-swapped")
	}
	state := APWorkerState{Revision: 5, Enabled: true, DHCPReady: true, UpdatedAt: now, Sharing: APSharingState{Preference: "wifi", State: "waiting-upstream"}}
	if !apPreferenceStateMatches(state, next, now) {
		t.Fatal("saved policy requires WAN readiness; it should accept truthful waiting state")
	}
	for _, mutate := range []func(*APWorkerState){func(v *APWorkerState) { v.Revision = 4 }, func(v *APWorkerState) { v.DHCPReady = false }, func(v *APWorkerState) { v.Sharing.Preference = "cellular" }, func(v *APWorkerState) { v.UpdatedAt = now.Add(-8 * time.Second) }} {
		bad := state
		mutate(&bad)
		if apPreferenceStateMatches(bad, next, now) {
			t.Fatal("unconfirmed worker ack accepted")
		}
	}
	rollback := previous
	if !apAcceptHotConfiguration(next, rollback) {
		t.Fatal("exact previous revision cannot be hot-restored")
	}
}

func TestAPPreferredUplinkHonorsOrderAndRequiresProofForFallback(t *testing.T) {
	now := time.Now()
	cell := sharingTestUpstream(t, "cellular", "usb0.1", now)
	wifi := sharingTestUpstream(t, "wifi", "wlan0", now)
	for _, preference := range []string{"wifi", "cellular", ""} {
		var calls []string
		readCell := func(context.Context) (APSharingUpstream, error) { calls = append(calls, "cellular"); return cell, nil }
		readWifi := func(context.Context) (APSharingUpstream, error) { calls = append(calls, "wifi"); return wifi, nil }
		chosen, e := readAPPreferredUpstream(context.Background(), preference, readCell, readWifi, func() time.Time { return now })
		if e != nil || chosen.kind != apDefaultPreference(preference) || len(calls) != 1 || calls[0] != chosen.kind {
			t.Fatal("preferred source/order", preference, e, calls)
		}
	}
	for _, preference := range []string{"wifi", "cellular"} {
		staleCell, staleWifi := cell, wifi
		if preference == "wifi" {
			staleWifi.observedAt = now.Add(-11 * time.Second)
		} else {
			staleCell.observedAt = now.Add(-11 * time.Second)
		}
		chosen, e := chooseAPSharingUpstreamPreferred(preference, staleCell, staleWifi, now)
		if e != nil || chosen.kind == preference {
			t.Fatal("stale preferred route did not fall back", e)
		}
		var calls []string
		chosen, e = readAPPreferredUpstream(context.Background(), preference, func(context.Context) (APSharingUpstream, error) {
			calls = append(calls, "cellular")
			return staleCell, nil
		}, func(context.Context) (APSharingUpstream, error) { calls = append(calls, "wifi"); return staleWifi, nil }, func() time.Time { return now })
		if e != nil || len(calls) != 2 || calls[0] != preference || calls[1] != chosen.kind {
			t.Fatal("fallback proof was not obtained in preference order", calls, e)
		}
	}
	wifi.dns = nil
	cell.dns = nil
	if _, e := chooseAPSharingUpstreamPreferred("wifi", cell, wifi, now); e == nil {
		t.Fatal("DNS-unverified source selected instead of blackhole")
	}
	if _, e := chooseAPSharingUpstreamPreferred("unknown", cell, wifi, now); e == nil {
		t.Fatal("invalid preference silently defaulted")
	}
}
