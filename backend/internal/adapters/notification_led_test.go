package adapters

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/led"
)

func TestNotificationTailAfterPlaybackAndReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frame.json")
	h := &notificationLED{store: led.NewScreenStore(path)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := led.Notification{Mode: "rainbow", Brightness: 180}
	_, e := h.start(ctx, n, 10*time.Second)
	if e != nil {
		t.Fatal(e)
	}
	// Starting the next queued announcement replaces the previous tail;
	// its canceled goroutine must never clear the new notification.
	id, e := h.start(ctx, n, 10*time.Second)
	if e != nil {
		t.Fatal(e)
	}
	time.Sleep(120 * time.Millisecond)
	h.mu.Lock()
	if h.id != id {
		t.Fatal("new notification ended prematurely")
	}
	h.tail <- struct{}{}
	h.mu.Unlock()
	time.Sleep(1500 * time.Millisecond)
	if _, active, e := led.ReadScreenFrame(path, time.Now()); e != nil || !active {
		t.Fatal("light ended before the two-second tail")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		_, active, e := led.ReadScreenFrame(path, time.Now())
		if e == nil && !active {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("notification did not restore the original LED state")
}
