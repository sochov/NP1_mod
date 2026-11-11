package adapters

import (
	"errors"
	"github.com/np1-control-center/ncc/internal/led"
	"github.com/np1-control-center/ncc/internal/model"
	"strings"
	"testing"
	"time"
)

type ledApplyFixture struct {
	current        led.Settings
	states         map[string]string
	controls       []string
	saves, markers []led.Settings
	ack            bool
	failAction     string
	now            time.Time
}

func newLEDApplyFixture() *ledApplyFixture {
	return &ledApplyFixture{current: led.Default(), states: map[string]string{"np1-control-led.service": "active"}, ack: true, now: time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)}
}
func (f *ledApplyFixture) dependencies() ledApplyDependencies {
	return ledApplyDependencies{load: func() (led.Settings, error) { return f.current, nil }, save: func(c led.Settings) error { f.current = c; f.saves = append(f.saves, c); return nil }, marker: func(c led.Settings) error { f.markers = append(f.markers, c); return nil }, serviceState: func(unit string) (string, error) {
		if value, ok := f.states[unit]; ok {
			return value, nil
		}
		return "inactive", nil
	}, control: func(action string) error {
		f.controls = append(f.controls, action)
		if action == f.failAction {
			return errors.New("exit status 1")
		}
		if action == "start" {
			f.states["np1-control-led.service"] = "active"
		}
		if action == "reset-failed" {
			f.states["np1-control-led.service"] = "inactive"
		}
		return nil
	}, readState: func() (led.State, error) {
		if !f.ack {
			return led.State{Revision: -1}, nil
		}
		return led.State{Revision: f.current.Revision, Mode: f.current.Mode, Running: f.current.Mode != "off"}, nil
	}, observe: func() model.Observation {
		return model.Value("synthetic LED apply", map[string]any{"settings": f.current})
	}, now: func() time.Time { return f.now }, sleep: func(d time.Duration) { f.now = f.now.Add(d) }}
}
func assertLEDFailure(t *testing.T, e error, status int, code string) {
	t.Helper()
	if e == nil {
		t.Fatal("expected typed LED rejection")
	}
	got, gotCode, reason := LEDErrorStatus(e)
	if got != status || gotCode != code || reason == "" || strings.Contains(reason, "exit status") {
		t.Fatalf("unexpected LED error status=%d code=%q reason=%q", got, gotCode, reason)
	}
}
func TestLEDApplyEightLiveCommandsNeverControlActiveService(t *testing.T) {
	f := newLEDApplyFixture()
	for _, mode := range []string{"solid", "rainbow", "off", "solid", "off", "rainbow", "solid", "off"} {
		next := f.current
		next.Mode = mode
		next.Autostart = mode != "off"
		expected := f.current.Revision
		result, e := applyLED(f.dependencies(), expected, next)
		if e != nil || result.Status != "available" || f.current.Revision != expected+1 {
			t.Fatalf("live change mode=%s: %v", mode, e)
		}
	}
	if len(f.controls) != 0 {
		t.Fatalf("active edits reached systemd: %v", f.controls)
	}
	if len(f.saves) != 8 || len(f.markers) != 8 {
		t.Fatal("live settings or marker changes missing")
	}
}
func TestLEDApplyBusyConflictAndInvalidSettingsDoNotMutate(t *testing.T) {
	f := newLEDApplyFixture()
	ledApplyMu.Lock()
	_, e := ApplyLED(1, led.Default())
	ledApplyMu.Unlock()
	assertLEDFailure(t, e, 409, "LED_BUSY")
	next := led.Default()
	_, e = applyLED(f.dependencies(), 99, next)
	assertLEDFailure(t, e, 409, "REVISION_CONFLICT")
	f.states["np1-front-led-rainbow.service"] = "active"
	_, e = applyLED(f.dependencies(), 1, next)
	assertLEDFailure(t, e, 409, "LED_IN_USE")
	next.Mode = "invalid"
	_, e = applyLED(f.dependencies(), 1, next)
	assertLEDFailure(t, e, 400, "INVALID_LED_SETTINGS")
	if len(f.saves) != 0 || len(f.markers) != 0 || len(f.controls) != 0 {
		t.Fatal("rejected requests caused side effects")
	}
}
func TestLEDFailedServiceResetsFailureThenStartsOnlyOnce(t *testing.T) {
	f := newLEDApplyFixture()
	f.states["np1-control-led.service"] = "failed"
	next := led.Default()
	next.Mode = "solid"
	if _, e := applyLED(f.dependencies(), 1, next); e != nil {
		t.Fatal(e)
	}
	if strings.Join(f.controls, ",") != "reset-failed,start" {
		t.Fatalf("failed service recovery %v", f.controls)
	}
}
func TestLEDStartFailureIs503AndRestoresPreviousSettings(t *testing.T) {
	f := newLEDApplyFixture()
	previous := f.current
	f.states["np1-control-led.service"] = "failed"
	f.failAction = "start"
	next := led.Default()
	next.Mode = "rainbow"
	next.Autostart = true
	_, e := applyLED(f.dependencies(), 1, next)
	assertLEDFailure(t, e, 503, "LED_START_FAILED")
	if f.current != previous || f.markers[len(f.markers)-1] != previous {
		t.Fatal("start failure did not restore settings and boot marker")
	}
	if strings.Contains(strings.Join(f.controls, ","), "restart") {
		t.Fatal("start failure counted as another service restart")
	}
}
func TestLEDUnconfirmedReloadRestoresConfigWithoutRestartingActiveWorker(t *testing.T) {
	f := newLEDApplyFixture()
	previous := f.current
	f.ack = false
	next := led.Default()
	next.Mode = "solid"
	_, e := applyLED(f.dependencies(), 1, next)
	assertLEDFailure(t, e, 503, "LED_COMMAND_NOT_CONFIRMED")
	if f.current != previous || len(f.controls) != 0 {
		t.Fatal("unconfirmed reload did not preserve the active worker and restore config")
	}
}
