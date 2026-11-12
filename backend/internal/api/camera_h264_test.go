package api

import (
	"context"
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

func TestCameraH264HTTP2SurvivesRecorderHandoffBeyondWriteTimeout(t *testing.T) {
	t.Parallel()
	s := New(config.Config{NetworkTrust: true, StateDir: t.TempDir()})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer := io.Pipe()
	defer reader.Close()
	s.client.Transport = stockAuditTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"video/mp4"}}, Body: reader}, nil
	})
	server := httptest.NewUnstartedServer(s.Handler())
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	client := server.Client()
	client.Timeout = 20 * time.Second
	client.Jar, _ = cookiejar.New(nil)
	r := request(t, client, server, "GET", "/auth/session", "", nil)
	var session struct{ CSRF string }
	json.NewDecoder(r.Body).Decode(&session)
	r.Body.Close()
	r = request(t, client, server, "POST", "/camera/sessions", session.CSRF, map[string]string{"source": "front"})
	var camera struct{ ID string }
	json.NewDecoder(r.Body).Decode(&camera)
	r.Body.Close()
	go func() {
		defer writer.Close()
		if _, err := io.WriteString(writer, "one"); err != nil {
			return
		}
		timer := time.NewTimer(11 * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			io.WriteString(writer, "two")
		}
	}()
	r = request(t, client, server, "GET", "/camera/sessions/"+camera.ID+"/h264", "", nil)
	defer r.Body.Close()
	data, err := io.ReadAll(r.Body)
	if r.ProtoMajor != 2 || err != nil || string(data) != "onetwo" {
		t.Fatalf("idle HTTP/2 stream expired during handoff: protocol=%s data=%q error=%v", r.Proto, data, err)
	}
}

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

func TestMapTilesReceiveAnOriginReferrer(t *testing.T) {
	s := New(config.Config{NetworkTrust: true, StateDir: t.TempDir()})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1/", nil))
	if w.Header().Get("Referrer-Policy") != "strict-origin-when-cross-origin" {
		t.Fatal("map provider requires an accurate Referer; do not strip it")
	}
}
