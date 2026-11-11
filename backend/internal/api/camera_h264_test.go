package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/config"
)

func TestCameraH264KeepsSessionOwnerAndFixedSource(t *testing.T) {
	s := New(config.Config{NetworkTrust: true, StateDir: t.TempDir()})
	calls := 0
	s.client.Transport = stockAuditTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "broker" || r.URL.Path != "/camera/h264" || r.URL.RawQuery != "source=front" {
			t.Error("nonfixed camera target", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"video/mp4"}, "X-Camera-Codec": []string{"avc1.42c028"}}, Body: io.NopCloser(strings.NewReader("synthetic-fragment"))}, nil
	})
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	client := server.Client()
	client.Jar, _ = cookiejar.New(nil)
	r := request(t, client, server, "GET", "/auth/session", "", nil)
	var session struct{ CSRF string }
	json.NewDecoder(r.Body).Decode(&session)
	r.Body.Close()
	r = request(t, client, server, "POST", "/camera/sessions", session.CSRF, map[string]string{"source": "front"})
	var camera struct{ ID string }
	json.NewDecoder(r.Body).Decode(&camera)
	r.Body.Close()
	if camera.ID == "" {
		t.Fatal("no session")
	}
	foreign := &http.Client{Transport: server.Client().Transport}
	foreign.Jar, _ = cookiejar.New(nil)
	r = request(t, foreign, server, "GET", "/auth/session", "", nil)
	r.Body.Close()
	r = request(t, foreign, server, "GET", "/camera/sessions/"+camera.ID+"/h264", "", nil)
	r.Body.Close()
	if r.StatusCode != 403 || calls != 0 {
		t.Fatal("foreign session consumed stream")
	}
	r = request(t, client, server, "GET", "/camera/sessions/"+camera.ID+"/h264", "", nil)
	data, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != 200 || r.Header.Get("X-Camera-Codec") != "avc1.42c028" || string(data) != "synthetic-fragment" || calls != 1 {
		t.Fatal("MSE stream metadata/body not forwarded", r.StatusCode)
	}
	s.mu.Lock()
	active := s.cameras[camera.ID].Active
	s.mu.Unlock()
	if active != 0 {
		t.Fatal("finished stream retained active slot")
	}
	// Simulate two already-active responses; a third must never reach the broker.
	s.mu.Lock()
	value := s.cameras[camera.ID]
	value.Active = 2
	s.cameras["other"] = value
	s.mu.Unlock()
	r = request(t, client, server, "GET", "/camera/sessions/"+camera.ID+"/h264", "", nil)
	r.Body.Close()
	if r.StatusCode != 429 || calls != 1 {
		t.Fatal("stream budget bypassed")
	}
	s.mu.Lock()
	delete(s.cameras, "other")
	value = s.cameras[camera.ID]
	value.Expires = time.Now().Add(-time.Second)
	s.cameras[camera.ID] = value
	s.mu.Unlock()
	r = request(t, client, server, "GET", "/camera/sessions/"+camera.ID+"/h264", "", nil)
	r.Body.Close()
	if r.StatusCode != 403 || calls != 1 {
		t.Fatal("expired session consumed stream")
	}
}

func TestCameraMSECanUseSameOriginBlobMedia(t *testing.T) {
	s := New(config.Config{NetworkTrust: true, StateDir: t.TempDir()})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1/", nil))
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "media-src 'self' blob:") {
		t.Fatal("MSE object URL blocked by media policy")
	}
}
