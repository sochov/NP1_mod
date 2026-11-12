package adapters

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/led"
)

type notificationLED struct {
	mu      sync.Mutex
	store   *led.ScreenStore
	prepare func() error
	id      string
	cancel  context.CancelFunc
	tail    chan struct{}
	failure error
	quiet   func() error
}

func RegisterNotificationLEDHandlers(mux *http.ServeMux, ctx context.Context) {
	h := &notificationLED{store: led.DefaultScreenStore, prepare: prepareScreenLED, quiet: led.QuietAfterNotification}
	mux.HandleFunc("POST /led/notifications", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Settings   led.Notification `json:"settings"`
			DurationMS int              `json:"durationMs"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		d.DisallowUnknownFields()
		if e := d.Decode(&body); e != nil || body.DurationMS < 1 || body.DurationMS > 120000 {
			screenLEDJSON(w, 400, map[string]string{"reason": "LED通知の入力を確認してください"})
			return
		}
		settings, e := body.Settings.Normalize()
		if e != nil || !settings.Enabled() {
			screenLEDJSON(w, 400, map[string]string{"reason": "LED通知の色・動作を確認してください"})
			return
		}
		if !ledApplyMu.TryLock() {
			screenLEDJSON(w, 409, map[string]string{"reason": "LEDの変更を確認中です"})
			return
		}
		defer ledApplyMu.Unlock()
		if e = h.prepare(); e != nil {
			screenLEDFailure(w, e)
			return
		}
		id, e := h.start(ctx, settings, time.Duration(body.DurationMS)*time.Millisecond)
		if e != nil {
			screenLEDFailure(w, e)
			return
		}
		screenLEDJSON(w, 200, map[string]string{"id": id})
	})
	mux.HandleFunc("POST /led/notifications/{id}/finish", func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		if r.PathValue("id") != h.id || h.id == "" {
			screenLEDJSON(w, 404, map[string]string{"reason": "LED通知は終了しています"})
			return
		}
		if h.failure != nil {
			screenLEDFailure(w, h.failure)
			return
		}
		select {
		case h.tail <- struct{}{}:
		default:
		}
		screenLEDJSON(w, 200, map[string]bool{"accepted": true})
	})
	mux.HandleFunc("POST /led/notifications/stop", func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.cancel != nil {
			h.cancel()
		}
		h.id = ""
		if e := h.quiet(); e != nil {
			screenLEDFailure(w, e)
			return
		}
		if e := h.store.Reset(); e != nil {
			screenLEDFailure(w, e)
			return
		}
		screenLEDJSON(w, 200, map[string]bool{"accepted": true})
	})
}

func (h *notificationLED) start(ctx context.Context, n led.Notification, duration time.Duration) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var token [32]byte
	if _, e := rand.Read(token[:]); e != nil {
		return "", e
	}
	if e := h.store.SetNotification(led.NotificationZones(n, 0)); e != nil {
		return "", e
	}
	if h.cancel != nil {
		h.cancel()
	}
	ctx, cancel := context.WithCancel(ctx)
	h.cancel = cancel
	h.id = hex.EncodeToString(token[:])
	h.failure = nil
	h.tail = make(chan struct{}, 1)
	id, tail := h.id, h.tail
	go h.run(ctx, id, tail, n, duration)
	return id, nil
}

func (h *notificationLED) run(ctx context.Context, id string, tail <-chan struct{}, n led.Notification, duration time.Duration) {
	started := time.Now()
	deadline := started.Add(duration)
	ticker := time.NewTicker(led.ScreenFrameInterval)
	defer ticker.Stop()
	tailing := false
	defer func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.id == id {
			if h.quiet != nil {
				if e := h.quiet(); e != nil {
					h.failure = e
				}
			}
			if e := h.store.EndNotification(); e != nil {
				h.failure = e
			}
			h.id = ""
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tail:
			if !tailing {
				deadline = time.Now().Add(2 * time.Second)
				tailing = true
			}
		case now := <-ticker.C:
			h.mu.Lock()
			if h.id != id {
				h.mu.Unlock()
				return
			}
			if !now.Before(deadline) {
				h.mu.Unlock()
				return
			}
			e := h.store.SetNotification(led.NotificationZones(n, now.Sub(started)))
			if e != nil {
				h.failure = errors.New("LED通知の更新に失敗しました")
			}
			h.mu.Unlock()
			if e != nil {
				return
			}
		}
	}
}
