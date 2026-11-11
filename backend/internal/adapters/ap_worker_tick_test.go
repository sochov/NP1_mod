package adapters

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/config"
)

func TestAPActiveTickReloadsFileBeforeVerificationWithoutRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	current := apConfiguration{Revision: 2, Enabled: true, SSID: "Synthetic LAN", Passphrase: "synthetic-secret", StandaloneFallback: true, Preference: "cellular"}
	r := &apRuntime{state: APWorkerState{Revision: 2, Enabled: true, DHCPReady: true}, sharing: &apSharingController{preference: "cellular"}}
	verified, stopped := 0, 0
	io := apSteadyTickIO{
		reload:  func(r *apRuntime, c *apConfiguration) { apReloadPreferenceAt(r, c, path) },
		channel: func(context.Context) (int, error) { return 6, nil },
		verify: func(context.Context) error {
			verified++
			// Model the published acknowledgement at the actual verify boundary.
			r.state.Sharing.Preference = r.sharing.preference
			r.state.UpdatedAt = time.Now()
			if !apPreferenceStateMatches(r.state, current, r.state.UpdatedAt) {
				t.Fatal("active iteration verified an old revision/preference")
			}
			return nil
		},
		stop: func() error { stopped++; return nil },
	}
	// A successful preference update, its explicit same-value CAS save, and
	// the lower previous revision used by rollback all use this active path.
	for _, next := range []apConfiguration{
		{Revision: 3, Enabled: true, SSID: current.SSID, Passphrase: current.Passphrase, StandaloneFallback: true, Preference: "wifi"},
		{Revision: 4, Enabled: true, SSID: current.SSID, Passphrase: current.Passphrase, StandaloneFallback: true, Preference: "wifi"},
		{Revision: 2, Enabled: true, SSID: current.SSID, Passphrase: current.Passphrase, StandaloneFallback: true, Preference: "cellular"},
	} {
		if err := config.Atomic(path, next); err != nil {
			t.Fatal(err)
		}
		changed, err := apSteadyTick(context.Background(), r, &current, 6, io)
		if err != nil || changed || current != next || r.state.Revision != next.Revision || r.sharing.preference != next.Preference {
			t.Fatal("active hot update was not accepted", err)
		}
	}
	if verified != 3 || stopped != 0 {
		t.Fatal("preference change restarted AP or skipped verification")
	}
	// A radio/secret change must continue through the normal restart path.
	foreign := current
	foreign.Revision++
	foreign.Passphrase = "different-synthetic-secret"
	if err := config.Atomic(path, foreign); err != nil {
		t.Fatal(err)
	}
	if _, err := apSteadyTick(context.Background(), r, &current, 6, io); err != nil {
		t.Fatal(err)
	}
	if current == foreign || stopped != 0 {
		t.Fatal("active tick changed radio configuration")
	}
}

func TestAPActiveTickKeepsChannelAndVerificationFailures(t *testing.T) {
	c := apConfiguration{StandaloneFallback: true}
	r := &apRuntime{}
	verified, stopped := 0, 0
	want := errors.New("synthetic verification failure")
	io := apSteadyTickIO{reload: func(*apRuntime, *apConfiguration) {}, channel: func(context.Context) (int, error) { return 11, nil }, verify: func(context.Context) error { verified++; return want }, stop: func() error { stopped++; return nil }}
	changed, err := apSteadyTick(context.Background(), r, &c, 6, io)
	if !changed || err != nil || stopped != 1 || verified != 0 {
		t.Fatal("real channel change did not stop AP")
	}
	io.channel = func(context.Context) (int, error) { return 6, nil }
	changed, err = apSteadyTick(context.Background(), r, &c, 6, io)
	if changed || !errors.Is(err, want) || verified != 1 || stopped != 1 {
		t.Fatal("verification failure hidden")
	}
	io.channel = func(context.Context) (int, error) { return 0, errors.New("unavailable") }
	io.verify = func(context.Context) error { verified++; return nil }
	changed, err = apSteadyTick(context.Background(), r, &c, 6, io)
	if changed || err != nil || verified != 2 || stopped != 1 {
		t.Fatal("transient STA loss restarted retained AP")
	}
}
