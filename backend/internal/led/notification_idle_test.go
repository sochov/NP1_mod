package led

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/config"
)

func TestNotificationIdleIsBoundToSavedRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idle.json")
	if on, e := ReadNotificationIdle(path, 1); e != nil || on {
		t.Fatal("missing marker suppressed normal LEDs")
	}
	if e := config.Atomic(path, map[string]int{"revision": 1}); e != nil {
		t.Fatal(e)
	}
	if on, e := ReadNotificationIdle(path, 1); e != nil || !on {
		t.Fatal("notification did not suppress its saved rainbow preset")
	}
	if on, e := ReadNotificationIdle(path, 2); e != nil || on {
		t.Fatal("a new explicit LED setting was suppressed")
	}
}

func TestNotificationEndsOffEvenWhenSavedPresetIsRainbow(t *testing.T) {
	f := newManagedFixture()
	f.settings.Mode = "rainbow"
	d := f.dependencies()
	var mu sync.Mutex
	active, quiet := true, false
	d.idle = func(revision int) (bool, error) { mu.Lock(); defer mu.Unlock(); return quiet && revision == 1, nil }
	d.screen = func(time.Time) (ScreenFrame, bool, error) {
		mu.Lock()
		defer mu.Unlock()
		return ScreenFrame{ScreenUpdate: ScreenUpdate{Sequence: 1, Zones: [5][3]int{{255, 0, 0}}}}, active, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runManaged(ctx, d) }()
	t.Cleanup(func() {
		cancel()
		if e := <-done; e != nil {
			t.Error(e)
		}
	})
	f.wait(t, func(s State) bool { return s.Mode == "screen" })
	mu.Lock()
	quiet = true
	active = false
	mu.Unlock()
	state := f.wait(t, func(s State) bool { return s.Mode == "off" && !s.Running && s.NotificationIdle })
	time.Sleep(300 * time.Millisecond)
	f.mu.Lock()
	if f.states[len(f.states)-1].Frames != state.Frames {
		t.Error("rainbow continued after announcement")
	}
	if f.settings.Mode != "rainbow" || f.settings.Revision != 1 {
		t.Error("saved preset was overwritten")
	}
	f.settings.Revision = 2
	f.settings.Mode = "solid"
	f.mu.Unlock()
	f.wait(t, func(s State) bool { return s.Revision == 2 && s.Mode == "solid" && s.Running && !s.NotificationIdle })
}
