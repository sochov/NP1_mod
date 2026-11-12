package adapters

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/np1-control-center/ncc/internal/led"
)

type screenLEDHandler struct {
	store   *led.ScreenStore
	prepare func() error
}

func RegisterScreenLEDHandlers(mux *http.ServeMux) error {
	if e := led.DefaultScreenStore.Reset(); e != nil {
		return e
	}
	registerScreenLEDHandlers(mux, &screenLEDHandler{led.DefaultScreenStore, prepareScreenLED})
	return nil
}
func registerScreenLEDHandlers(mux *http.ServeMux, h *screenLEDHandler) {
	mux.HandleFunc("POST /led/screen/sessions", h.begin)
	mux.HandleFunc("PUT /led/screen/sessions/{id}", h.update)
	mux.HandleFunc("DELETE /led/screen/sessions/{id}", h.end)
}
func screenLEDJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func screenLEDFailure(w http.ResponseWriter, e error) {
	status, code, reason := led.ScreenErrorStatus(e)
	screenLEDJSON(w, status, map[string]string{"status": "error", "code": code, "reason": reason})
}
func screenLEDBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if len(r.URL.Query()) != 0 {
		screenLEDJSON(w, 400, map[string]string{"code": "INVALID_SCREEN_REQUEST", "reason": "画面連動のURLを確認してください"})
		return nil, false
	}
	data, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
	if e != nil {
		screenLEDJSON(w, 400, map[string]string{"code": "INVALID_SCREEN_REQUEST", "reason": "画面連動の入力を確認してください"})
		return nil, false
	}
	return data, true
}
func (h *screenLEDHandler) begin(w http.ResponseWriter, r *http.Request) {
	data, ok := screenLEDBody(w, r)
	if !ok {
		return
	}
	if _, e := led.DecodeScreenObject(data); e != nil {
		screenLEDJSON(w, 400, map[string]string{"code": "INVALID_SCREEN_REQUEST", "reason": "開始の入力を確認してください"})
		return
	}
	if !ledApplyMu.TryLock() {
		screenLEDFailure(w, &led.ScreenError{Status: 409, Code: "LED_BUSY", Reason: "LEDの変更を確認中です"})
		return
	}
	defer ledApplyMu.Unlock()
	if e := h.prepare(); e != nil {
		screenLEDFailure(w, e)
		return
	}
	session, e := h.store.Begin()
	if e != nil {
		screenLEDFailure(w, e)
		return
	}
	screenLEDJSON(w, 200, session)
}
func (h *screenLEDHandler) update(w http.ResponseWriter, r *http.Request) {
	data, ok := screenLEDBody(w, r)
	if !ok {
		return
	}
	if !led.ValidScreenID(r.PathValue("id")) {
		screenLEDFailure(w, &led.ScreenError{Status: 400, Code: "INVALID_SCREEN_ID", Reason: "画面連動セッションを確認してください"})
		return
	}
	var value led.ScreenUpdate
	if e := json.Unmarshal(data, &value); e != nil {
		screenLEDJSON(w, 400, map[string]string{"code": "INVALID_SCREEN_FRAME", "reason": "RGB値5組と連番を確認してください"})
		return
	}
	if e := h.store.Update(r.Context(), r.PathValue("id"), value); e != nil {
		screenLEDFailure(w, e)
		return
	}
	screenLEDJSON(w, 200, map[string]any{"accepted": true, "sequence": value.Sequence})
}
func (h *screenLEDHandler) end(w http.ResponseWriter, r *http.Request) {
	data, ok := screenLEDBody(w, r)
	if !ok {
		return
	}
	if len(data) != 0 || !led.ValidScreenID(r.PathValue("id")) {
		screenLEDJSON(w, 400, map[string]string{"code": "INVALID_SCREEN_REQUEST", "reason": "停止の入力を確認してください"})
		return
	}
	if e := h.store.End(r.PathValue("id")); e != nil {
		screenLEDFailure(w, e)
		return
	}
	screenLEDJSON(w, 200, map[string]bool{"restored": true})
}

// Start only the owner-managed worker. No settings revision, boot marker or stock service is modified.
func prepareScreenLED() error {
	settings, e := led.Load()
	if e != nil {
		return errors.New("LEDの保存設定を取得できません")
	}
	if legacy, _ := command("systemctl", "is-active", "np1-front-led-rainbow.service"); legacy == "active" {
		return &led.ScreenError{Status: 409, Code: "LED_IN_USE", Reason: "別のLED制御サービスが動作しています"}
	}
	running, _ := command("systemctl", "is-active", "np1-control-led.service")
	if running != "active" {
		if running == "failed" {
			if _, e = command("systemctl", "reset-failed", "np1-control-led.service"); e != nil {
				return e
			}
		}
		if _, e = command("systemctl", "start", "np1-control-led.service"); e != nil {
			return e
		}
	}
	until := time.Now().Add(2 * time.Second)
	for time.Now().Before(until) {
		var state led.State
		data, e := os.ReadFile(led.RuntimeState)
		if e == nil {
			e = json.Unmarshal(data, &state)
		}
		now := time.Now()
		if e == nil && screenWorkerReady(state, settings, now) {
			return nil
		}
		time.Sleep(30 * time.Millisecond)
	}
	return &led.ScreenError{Status: 503, Code: "LED_WORKER_NOT_CONFIRMED", Reason: "LED制御の準備を確認できません。状態を更新してください"}
}

func screenWorkerReady(state led.State, settings led.Settings, now time.Time) bool {
	modeReady := state.Mode == settings.Mode && (state.Running || settings.Mode == "off") || state.Mode == "screen" && state.Running || state.NotificationIdle && state.Mode == "off" && !state.Running
	return state.Revision == settings.Revision && modeReady && state.Error == "" && !state.AppliedAt.After(now) && now.Sub(state.AppliedAt) < 3*time.Second
}
