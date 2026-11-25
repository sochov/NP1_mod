package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/androidauto"
	"github.com/np1-control-center/ncc/internal/config"
)

// The Android Auto projection runs as its own service. The Web process only
// keeps its settings file and relays the status the service publishes on its
// socket.

const androidAutoSettingsLimit = 4096

type androidAutoService struct {
	dir    string
	client *http.Client
	mu     sync.Mutex
}

func newAndroidAutoService(stateDir string) *androidAutoService {
	return &androidAutoService{dir: filepath.Join(stateDir, "androidauto"), client: unixClient(androidauto.Socket)}
}

func (a *androidAutoService) settings() androidauto.Settings {
	return androidauto.ReadSettings(filepath.Dir(a.dir))
}

func (a *androidAutoService) status(ctx context.Context) (json.RawMessage, bool) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://androidauto/status", nil)
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil || resp.StatusCode != http.StatusOK || !json.Valid(b) {
		return nil, false
	}
	return b, true
}

func (a *androidAutoService) nudge() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://androidauto/reload", nil)
	if resp, err := a.client.Do(req); err == nil {
		resp.Body.Close()
	}
}

func (s *Server) registerAndroidAuto() {
	a := s.androidAuto
	s.mux.HandleFunc("GET /api/v1/android-auto", func(w http.ResponseWriter, r *http.Request) {
		out := map[string]any{"settings": a.settings(), "service": "unavailable"}
		if _, err := os.Stat("/etc/systemd/system/np1-control-android-auto.service"); err != nil {
			out["service"] = "not-installed"
		}
		if st, ok := a.status(r.Context()); ok {
			out["service"], out["status"] = "running", st
		}
		jsonOut(w, 200, out)
	})
	s.mux.HandleFunc("PUT /api/v1/android-auto/settings", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ExpectedRevision int                  `json:"expectedRevision"`
			Settings         androidauto.Settings `json:"settings"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, androidAutoSettingsLimit)
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if d.Decode(&body) != nil || d.Decode(&struct{}{}) != io.EOF {
			fail(w, 400, "INVALID_INPUT", "入力を確認してください")
			return
		}
		next := body.Settings.Normalize()
		if next != body.Settings {
			fail(w, 400, "INVALID_ANDROID_AUTO_SETTINGS", "対応していない値が含まれています")
			return
		}
		a.mu.Lock()
		cur := a.settings()
		if body.ExpectedRevision != cur.Revision {
			a.mu.Unlock()
			fail(w, 409, "REVISION_CONFLICT", "別の画面で設定が変更されました。再読み込みしてください")
			return
		}
		next.Revision = cur.Revision + 1
		if err := os.MkdirAll(a.dir, 0700); err != nil {
			a.mu.Unlock()
			fail(w, 503, "STORAGE_UNAVAILABLE", "設定を保存できませんでした")
			return
		}
		if err := config.Atomic(filepath.Join(a.dir, "settings.json"), next); err != nil {
			a.mu.Unlock()
			s.audit("android-auto.settings.update", "storage-failed")
			fail(w, 503, "STORAGE_UNAVAILABLE", "設定を保存できませんでした")
			return
		}
		a.mu.Unlock()
		a.nudge()
		s.audit("android-auto.settings.update", "accepted")
		jsonOut(w, 200, next)
	})
}
