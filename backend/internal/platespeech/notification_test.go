package platespeech

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/led"
)

type orderedPlayer struct{ events *[]string }

func (p orderedPlayer) Available() error { return nil }
func (p orderedPlayer) Play(context.Context, []byte, int) error {
	*p.events = append(*p.events, "play")
	return nil
}

type orderedLight struct {
	events *[]string
	fail   bool
}

func (n orderedLight) Start(context.Context, led.Notification, time.Duration) (func() error, error) {
	*n.events = append(*n.events, "light-start")
	if n.fail {
		return nil, errors.New("light unavailable")
	}
	return func() error { *n.events = append(*n.events, "light-tail"); return nil }, nil
}
func TestSpeechAndNotificationSharePreviewPlaybackLifecycle(t *testing.T) {
	var events []string
	m := NewManager(t.TempDir(), &fakeSynth{}, orderedPlayer{&events})
	m.Notifier = orderedLight{events: &events}
	part := Part{Text: "test"}
	if e := writeAtomic(m.clipPath(1, part.Text), []byte{1, 0}, 0600); e != nil {
		t.Fatal(e)
	}
	light := led.Notification{Mode: "solid", Effect: "flow", Brightness: 180}
	if e := m.playNotification(context.Background(), []Part{part}, 70, light); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(events, []string{"light-start", "play", "light-tail"}) {
		t.Fatal(events)
	}
	events = nil
	m.Notifier = orderedLight{events: &events, fail: true}
	if e := m.playNotification(context.Background(), []Part{part}, 70, light); e == nil {
		t.Fatal("LED failure was concealed")
	}
	if !reflect.DeepEqual(events, []string{"light-start", "play"}) {
		t.Fatal("LED failure suppressed audio", events)
	}
	events = nil
	m.Notifier = orderedLight{events: &events}
	if e := m.playNotification(context.Background(), nil, 70, light); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(events, []string{"light-start", "light-tail"}) {
		t.Fatal("LED-only synthesized or played audio", events)
	}
}
func TestOldClipsArePreservedButNeverReusedAfterSpeakerFix(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, &fakeSynth{}, &fakePlayer{})
	old := filepath.Join(dir, "clips", "1", hashName("test"))
	if e := writeAtomic(old, []byte{9, 0}, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(m.clipPath(1, "test")); !os.IsNotExist(e) {
		t.Fatal("old voice reused")
	}
	if _, e := os.Stat(old); e != nil {
		t.Fatal("rollback clip removed")
	}
}
