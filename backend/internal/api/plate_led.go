package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/np1-control-center/ncc/internal/led"
)

type plateLEDNotifier struct{ client *http.Client }

func (s *Server) registerLEDTestRoutes() {
	s.mux.HandleFunc("POST /api/v1/device/led/test", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Settings   led.Notification `json:"settings"`
			DurationMS int              `json:"durationMs"`
		}
		if !decode(w, r, &body) {
			return
		}
		n, e := body.Settings.Normalize()
		if e != nil || !n.Enabled() || body.DurationMS < 1000 || body.DurationMS > 30000 {
			fail(w, 400, "INVALID_LED_TEST", "色と点灯時間を確認してください")
			return
		}
		if _, e = (plateLEDNotifier{client: s.client}).call(r.Context(), "", body); e != nil {
			fail(w, 503, "LED_TEST_FAILED", e.Error())
			return
		}
		s.audit("led.test", "accepted")
		jsonOut(w, 200, map[string]any{"accepted": true, "durationMs": body.DurationMS})
	})
	s.mux.HandleFunc("POST /api/v1/device/led/test/stop", func(w http.ResponseWriter, r *http.Request) {
		if _, e := (plateLEDNotifier{client: s.client}).call(r.Context(), "/stop", struct{}{}); e != nil {
			fail(w, 503, "LED_STOP_FAILED", e.Error())
			return
		}
		s.audit("led.test.stop", "accepted")
		jsonOut(w, 200, map[string]bool{"accepted": true})
	})
}

func (n plateLEDNotifier) call(ctx context.Context, path string, body any) (string, error) {
	data, _ := json.Marshal(body)
	req, e := http.NewRequestWithContext(ctx, "POST", "http://broker/led/notifications"+path, bytes.NewReader(data))
	if e != nil {
		return "", e
	}
	req.Header.Set("Content-Type", "application/json")
	r, e := n.client.Do(req)
	if e != nil {
		return "", errors.New("LED通知へ接続できません")
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return "", errors.New("LED通知を開始・終了できませんでした")
	}
	var value struct {
		ID string `json:"id"`
	}
	if e = json.NewDecoder(r.Body).Decode(&value); e != nil {
		return "", e
	}
	return value.ID, nil
}

func (n plateLEDNotifier) Start(ctx context.Context, settings led.Notification, duration time.Duration) (func() error, error) {
	id, e := n.call(ctx, "", map[string]any{"settings": settings, "durationMs": duration.Milliseconds()})
	if e != nil {
		return nil, e
	}
	if !led.ValidScreenID(id) {
		return nil, errors.New("LED通知の応答が不正です")
	}
	return func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, e := n.call(ctx, "/"+id+"/finish", struct{}{})
		return e
	}, nil
}
