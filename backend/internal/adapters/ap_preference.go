package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Both confirmation and rollback fit inside the broker's 20-second HTTP
// timeout, with room for atomic persistence and the surrounding response.
const apPreferenceApplyWait = 8 * time.Second
const apPreferenceRollbackWait = 6 * time.Second

func apValidPreference(value string) bool { return value == "cellular" || value == "wifi" }
func apDefaultPreference(value string) string {
	if value == "" {
		return "cellular"
	}
	return value
}

func apNextConfiguration(previous apConfiguration, expected int, change APChange) (apConfiguration, error) {
	if previous.Revision != expected {
		return previous, errors.New("REVISION_CONFLICT")
	}
	if expected < 0 || expected == int(^uint(0)>>1) {
		return previous, errors.New("AP_REVISION_INVALID")
	}
	if change.Enabled == nil && change.SSID == nil && change.Passphrase == "" && change.Preference == nil {
		return previous, errors.New("AP_CHANGE_EMPTY")
	}
	next := previous
	next.Revision++
	next.Preference = apDefaultPreference(previous.Preference)
	if change.Enabled != nil {
		next.Enabled = *change.Enabled
	}
	if change.SSID != nil {
		next.SSID = *change.SSID
	}
	if change.Passphrase != "" {
		next.Passphrase = change.Passphrase
	}
	if change.Preference != nil {
		next.Preference = *change.Preference
	}
	if !apValidPreference(next.Preference) {
		return previous, errors.New("AP_UPSTREAM_PREFERENCE_INVALID")
	}
	if e := apValidate(next.SSID, next.Passphrase, next.Enabled); e != nil {
		return previous, e
	}
	return next, nil
}

func apSameRadioConfiguration(a, b apConfiguration) bool {
	return a.Enabled == b.Enabled && a.SSID == b.SSID && a.Passphrase == b.Passphrase && a.StandaloneFallback == b.StandaloneFallback
}
func apPreferenceOnlyChange(a, b apConfiguration) bool {
	// An explicit save of the same preference still advances the revision and
	// must never restart an otherwise unchanged AP.
	return apSameRadioConfiguration(a, b) && apValidPreference(apDefaultPreference(b.Preference))
}
func apAcceptHotConfiguration(previous, next apConfiguration) bool {
	return next.Revision != previous.Revision && apValidPreference(next.Preference) && apSameRadioConfiguration(previous, next)
}
func apReloadPreference(r *apRuntime, c *apConfiguration) {
	apReloadPreferenceAt(r, c, filepath.Join(APDirectory, "config.json"))
}

func apReloadPreferenceAt(r *apRuntime, c *apConfiguration, path string) {
	next, e := apLoadConfig(path)
	if e != nil || !apAcceptHotConfiguration(*c, next) {
		return
	}
	*c = next
	r.state.Revision = next.Revision
	if r.sharing != nil {
		r.sharing.preference = next.Preference
	}
}
func apPreferenceStateMatches(state APWorkerState, c apConfiguration, now time.Time) bool {
	return state.Revision == c.Revision && state.Enabled && state.DHCPReady && state.Sharing.Preference == apDefaultPreference(c.Preference) && !state.UpdatedAt.After(now) && now.Sub(state.UpdatedAt) < 7*time.Second
}
func apWaitPreference(c apConfiguration, maximum time.Duration) bool {
	until := time.Now().Add(maximum)
	for time.Now().Before(until) {
		var state APWorkerState
		raw, e := os.ReadFile(filepath.Join(APRuntimeDirectory, "status.json"))
		if e == nil && len(raw) <= 65536 && json.Unmarshal(raw, &state) == nil && apPreferenceStateMatches(state, c, time.Now()) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// The selection order changes; the proof requirements and fallback remain the
// same. Do not even query a slower fallback when the preferred proof is ready.
func readAPPreferredUpstream(ctx context.Context, preference string, cellular, wifi func(context.Context) (APSharingUpstream, error), now func() time.Time) (APSharingUpstream, error) {
	preference = apDefaultPreference(preference)
	if !apValidPreference(preference) {
		return APSharingUpstream{}, errors.New("AP_UPSTREAM_PREFERENCE_INVALID")
	}
	var cell, sta APSharingUpstream
	if preference == "wifi" {
		sta, _ = wifi(ctx)
		if sta.kind != "wifi" || !sta.usable(now()) {
			cell, _ = cellular(ctx)
		}
	} else {
		cell, _ = cellular(ctx)
		if cell.kind != "cellular" || !cell.usable(now()) {
			sta, _ = wifi(ctx)
		}
	}
	return chooseAPSharingUpstreamPreferred(preference, cell, sta, now())
}
