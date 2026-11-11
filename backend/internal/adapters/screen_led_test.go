package adapters

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/led"
)

func TestScreenLEDHTTPValidationAndLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "screen.json")
	store := led.NewScreenStore(path)
	prepared := 0
	mux := http.NewServeMux()
	registerScreenLEDHandlers(mux, &screenLEDHandler{store, func() error { prepared++; return nil }})
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	for _, body := range []string{"", `null`, `{"unexpected":1}`, `{} {}`, strings.Repeat(" ", 1025)} {
		if w := call("POST", "/led/screen/sessions", body); w.Code != 400 {
			t.Fatalf("body %q accepted: %d", body[:min(20, len(body))], w.Code)
		}
	}
	if prepared != 0 {
		t.Fatal("invalid input reached worker")
	}
	w := call("POST", "/led/screen/sessions", "{}")
	if w.Code != 200 {
		t.Fatalf("begin %d %s", w.Code, w.Body)
	}
	var session led.ScreenSession
	if e := json.Unmarshal(w.Body.Bytes(), &session); e != nil {
		t.Fatal(e)
	}
	if w = call("POST", "/led/screen/sessions", "{}"); w.Code != 409 {
		t.Fatal("duplicate begin accepted")
	}
	target := "/led/screen/sessions/" + session.ID
	valid := `{"sequence":1,"zones":[[255,0,0],[0,255,0],[0,0,255],[255,255,255],[255,128,0]]}`
	for _, body := range []string{`{}`, strings.Replace(valid, `[255,0,0]`, `[255,0]`, 1), strings.Replace(valid, `"sequence":1`, `"sequence":1,"sequence":2`, 1)} {
		if w = call("PUT", target, body); w.Code != 400 {
			t.Fatalf("invalid frame accepted %d", w.Code)
		}
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("invalid frames created a file")
	}
	if w = call("PUT", target, valid); w.Code != 200 || !strings.Contains(w.Body.String(), `"accepted":true`) {
		t.Fatalf("frame %d %s", w.Code, w.Body)
	}
	if w = call("PUT", target, valid); w.Code != 409 {
		t.Fatal("replay accepted")
	}
	if w = call("DELETE", target, "{}"); w.Code != 400 {
		t.Fatal("unexpected delete payload accepted")
	}
	if w = call("DELETE", target, ""); w.Code != 200 {
		t.Fatalf("delete %d %s", w.Code, w.Body)
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("delete left active file")
	}
	if w = call("PUT", target, valid); w.Code != 404 {
		t.Fatal("revoked session accepted")
	}
	if w = call("GET", target, ""); w.Code != 405 {
		t.Fatal("unexpected method accepted")
	}
	if w = call("PUT", "/led/screen/sessions/not-a-token", valid); w.Code != 400 {
		t.Fatal("malformed token accepted")
	}
	if w = call("POST", "/led/screen/sessions?path=/other", "{}"); w.Code != 400 {
		t.Fatal("caller runtime path accepted")
	}
}

func TestScreenWorkerAlreadySharingStillReturnsBusyFromLease(t *testing.T) {
	now := time.Now()
	settings := led.Default()
	state := led.State{Revision: settings.Revision, Mode: "screen", Running: true, AppliedAt: now}
	if !screenWorkerReady(state, settings, now) {
		t.Fatal("active screen worker should reach existing-lease conflict")
	}
	state.Error = "synthetic failure"
	if screenWorkerReady(state, settings, now) {
		t.Fatal("failed screen worker accepted")
	}
	state.Error = ""
	state.AppliedAt = now.Add(-4 * time.Second)
	if screenWorkerReady(state, settings, now) {
		t.Fatal("stale worker accepted")
	}
}
func TestScreenLEDPreparationFailureDoesNotCreateLease(t *testing.T) {
	store := led.NewScreenStore(filepath.Join(t.TempDir(), "frame.json"))
	mux := http.NewServeMux()
	registerScreenLEDHandlers(mux, &screenLEDHandler{store, func() error {
		return &led.ScreenError{Status: 503, Code: "LED_WORKER_NOT_CONFIRMED", Reason: "synthetic failure"}
	}})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/led/screen/sessions", strings.NewReader("{}")))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
	if _, e := store.Begin(); e != nil {
		t.Fatal("failed preparation acquired a capability", e)
	}
}
