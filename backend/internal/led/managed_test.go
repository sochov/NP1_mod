package led

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type managedWrite struct{ name, value string }

func TestManagedScreenOverridesOffAndRestoresWithoutPersistentEdit(t *testing.T) {
	f := newManagedFixture()
	f.settings.Mode = "off"
	f.settings.Brightness = 1
	d := f.dependencies()
	var screenMu sync.Mutex
	active := true
	frame := ScreenFrame{ScreenUpdate: ScreenUpdate{Sequence: 1, Zones: [5][3]int{{255, 0, 0}, {0, 255, 0}, {0, 0, 255}, {255, 255, 255}, {80, 40, 20}}}}
	d.screen = func(time.Time) (ScreenFrame, bool, error) {
		screenMu.Lock()
		defer screenMu.Unlock()
		return frame, active, nil
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
	s := f.wait(t, func(s State) bool { return s.Mode == "screen" && s.ScreenSequence == 1 })
	if len(s.Zones) != 5 || s.Zones[0] != frame.Zones[0] {
		t.Fatalf("screen RGB altered by saved brightness: %v", s.Zones)
	}
	screenMu.Lock()
	active = false
	screenMu.Unlock()
	f.wait(t, func(s State) bool { return s.Mode == "off" && !s.Running && s.ScreenSequence == 0 })
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.settings.Mode != "off" || f.settings.Revision != 1 {
		t.Fatal("screen changed persistent settings")
	}
	found := false
	for _, w := range f.writes {
		if w.name == "led_value" && w.value == "1 255 0 0\n" {
			found = true
		}
	}
	if !found {
		t.Fatal("five-zone color command missing")
	}
}

type managedFixture struct {
	mu        sync.Mutex
	settings  Settings
	loadError error
	writes    []managedWrite
	states    []State
	changed   chan struct{}
}

func newManagedFixture() *managedFixture {
	c := Default()
	c.Mode = "solid"
	return &managedFixture{settings: c, changed: make(chan struct{}, 1)}
}
func (f *managedFixture) dependencies() managedDependencies {
	return managedDependencies{load: func() (Settings, error) { f.mu.Lock(); defer f.mu.Unlock(); return f.settings, f.loadError }, write: func(name, value string) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.writes = append(f.writes, managedWrite{name, value})
		return nil
	}, publish: func(state State) error {
		f.mu.Lock()
		f.states = append(f.states, state)
		f.mu.Unlock()
		select {
		case f.changed <- struct{}{}:
		default:
		}
		return nil
	}, now: time.Now}
}
func (f *managedFixture) set(c Settings, e error) {
	f.mu.Lock()
	f.settings = c
	f.loadError = e
	f.mu.Unlock()
}
func (f *managedFixture) wait(t *testing.T, predicate func(State) bool) State {
	t.Helper()
	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	for {
		f.mu.Lock()
		if len(f.states) > 0 {
			state := f.states[len(f.states)-1]
			if predicate(state) {
				f.mu.Unlock()
				return state
			}
		}
		f.mu.Unlock()
		select {
		case <-f.changed:
		case <-timeout.C:
			t.Fatal("managed LED state did not reach expected command")
		}
	}
}
func (f *managedFixture) commands() []managedWrite {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]managedWrite(nil), f.writes...)
}
func managedStillAlive(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case e := <-done:
		t.Fatalf("worker exited during live configuration: %v", e)
	default:
	}
}
func managedStop(t *testing.T, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	cancel()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("managed worker did not cancel")
	}
}
func assertManagedFiveZonesOff(t *testing.T, writes []managedWrite) {
	t.Helper()
	status := -1
	for index, command := range writes {
		if command.name == "led_status" && strings.TrimSpace(command.value) == "0" {
			status = index
		}
	}
	if status < 0 {
		t.Fatal("cancel did not disable LED status")
	}
	zones := map[int]bool{}
	for _, command := range writes[status+1:] {
		if command.name != "led_value" {
			continue
		}
		fields := strings.Fields(command.value)
		if len(fields) != 4 || fields[1] != "0" || fields[2] != "0" || fields[3] != "0" {
			t.Fatal("non-dark zone after cancellation")
		}
		zone, e := strconv.Atoi(fields[0])
		if e != nil || zone < 1 || zone > 5 {
			t.Fatal("invalid off zone")
		}
		zones[zone] = true
	}
	if len(zones) != 5 {
		t.Fatalf("only %d zones were cleared", len(zones))
	}
}

func TestManagedEightLiveChangesKeepOneWorkerIncludingOff(t *testing.T) {
	f := newManagedFixture()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runManaged(ctx, f.dependencies()) }()
	t.Cleanup(cancel)
	f.wait(t, func(s State) bool { return s.Revision == 1 && s.Mode == "solid" && s.Frames > 0 })
	c := Default()
	for index, mode := range []string{"rainbow", "off", "solid", "rainbow", "off", "solid", "rainbow", "off"} {
		c.Revision = index + 2
		c.Mode = mode
		c.Brightness = 100 + index
		f.set(c, nil)
		f.wait(t, func(s State) bool { return s.Revision == c.Revision && s.Mode == mode && s.Error == "" })
		managedStillAlive(t, done)
	}
	before := len(f.commands())
	time.Sleep(250 * time.Millisecond)
	managedStillAlive(t, done)
	if len(f.commands()) != before {
		t.Fatal("off idle repeatedly writes hardware instead of waiting for configuration")
	}
	managedStop(t, cancel, done)
	assertManagedFiveZonesOff(t, f.commands())
}
func TestManagedInvalidReloadKeepsPreviousModeThenRecovers(t *testing.T) {
	f := newManagedFixture()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runManaged(ctx, f.dependencies()) }()
	t.Cleanup(cancel)
	f.wait(t, func(s State) bool { return s.Revision == 1 && s.Mode == "solid" && s.Error == "" })
	before := len(f.commands())
	bad := Default()
	bad.Revision = 2
	bad.Mode = "invalid"
	f.set(bad, nil)
	state := f.wait(t, func(s State) bool { return s.Error != "" })
	if state.Revision != 1 || state.Mode != "solid" || !state.Running {
		t.Fatal("invalid reload replaced the last valid command")
	}
	for _, command := range f.commands()[before:] {
		if command.name == "led_status" && strings.TrimSpace(command.value) == "0" {
			t.Fatal("invalid reload darkened the previous valid output")
		}
	}
	managedStillAlive(t, done)
	good := Default()
	good.Revision = 3
	good.Mode = "rainbow"
	f.set(good, nil)
	f.wait(t, func(s State) bool { return s.Revision == 3 && s.Mode == "rainbow" && s.Error == "" })
	f.set(good, errors.New("synthetic invalid JSON"))
	f.wait(t, func(s State) bool { return s.Revision == 3 && s.Error != "" })
	f.set(good, nil)
	f.wait(t, func(s State) bool { return s.Revision == 3 && s.Mode == "rainbow" && s.Error == "" })
	managedStop(t, cancel, done)
	assertManagedFiveZonesOff(t, f.commands())
}
func TestManagedAcceptsRollbackToPreviousRevisionWithoutRestart(t *testing.T) {
	f := newManagedFixture()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runManaged(ctx, f.dependencies()) }()
	t.Cleanup(cancel)
	f.wait(t, func(s State) bool { return s.Revision == 1 && s.Mode == "solid" })
	next := Default()
	next.Revision = 2
	next.Mode = "rainbow"
	f.set(next, nil)
	f.wait(t, func(s State) bool { return s.Revision == 2 && s.Mode == "rainbow" })
	previous := Default()
	previous.Mode = "solid"
	f.set(previous, nil)
	f.wait(t, func(s State) bool { return s.Revision == 1 && s.Mode == "solid" })
	managedStillAlive(t, done)
	managedStop(t, cancel, done)
	assertManagedFiveZonesOff(t, f.commands())
}
