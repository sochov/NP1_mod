package led

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestManagedScreenReadFailureRestoresSolidAndCancelClearsCommandMetadata(t *testing.T) {
	f := newManagedFixture()
	f.settings.RGB = [3]int{120, 80, 40}
	f.settings.Brightness = 255
	d := f.dependencies()
	var mu sync.Mutex
	fail := false
	d.screen = func(time.Time) (ScreenFrame, bool, error) {
		mu.Lock()
		defer mu.Unlock()
		if fail {
			return ScreenFrame{}, true, errors.New("synthetic corrupt RAM frame")
		}
		return ScreenFrame{ScreenUpdate: ScreenUpdate{Sequence: 8, Zones: [5][3]int{{255, 0, 0}, {0, 255, 0}, {0, 0, 255}, {255, 255, 255}, {20, 40, 80}}}}, true, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runManaged(ctx, d) }()
	f.wait(t, func(state State) bool { return state.Mode == "screen" && state.ScreenSequence == 8 })
	mu.Lock()
	fail = true
	mu.Unlock()
	restored := f.wait(t, func(state State) bool { return state.Mode == "solid" && state.ScreenSequence == 0 })
	if len(restored.Zones) != 0 {
		t.Fatal("screen colors persisted in normal command state")
	}
	mu.Lock()
	fail = false
	mu.Unlock()
	f.wait(t, func(state State) bool { return state.Mode == "screen" && state.ScreenSequence == 8 })
	managedStop(t, cancel, done)
	assertManagedFiveZonesOff(t, f.commands())
	final := f.wait(t, func(state State) bool { return state.Mode == "off" && !state.Running })
	if len(final.Zones) != 0 || final.ScreenSequence != 0 {
		t.Fatal("shutdown left stale screen command metadata")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.settings.RGB != [3]int{120, 80, 40} || f.settings.Mode != "solid" || f.settings.Revision != 1 {
		t.Fatal("screen override modified normal saved settings")
	}
}
