package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/np1-control-center/ncc/internal/config"
)

func TestNativeAudioRoutesRequireSessionAndKeepFixedTargets(t *testing.T) {
	s := New(config.Config{NetworkTrust: true, StateDir: t.TempDir()})
	var called []string
	s.client.Transport = stockAuditTransport(func(r *http.Request) (*http.Response, error) {
		called = append(called, r.Method+" "+r.URL.Path)
		if r.URL.Host != "broker" {
			t.Error("unfixed broker target")
		}
		if r.Method == "PUT" {
			body, _ := io.ReadAll(r.Body)
			if string(body) != `{"expectedValue":0,"value":1}` {
				t.Error("request body changed", string(body))
			}
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":"available","data":{}}`))}, nil
	})
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	client := server.Client()
	client.Jar, _ = cookiejar.New(nil)
	r := request(t, client, server, "GET", "/auth/session", "", nil)
	var session struct{ CSRF string }
	if json.NewDecoder(r.Body).Decode(&session) != nil || session.CSRF == "" {
		t.Fatal("no session")
	}
	r.Body.Close()
	body := map[string]int{"expectedValue": 0, "value": 1}
	r = request(t, client, server, "PUT", "/device/native-audio/mute", "", body)
	r.Body.Close()
	if r.StatusCode != 403 || len(called) != 0 {
		t.Fatal("missing session token reached broker")
	}
	for _, id := range []string{"master-volume", "unknown", "mute%2Falexa"} {
		r = request(t, client, server, "PUT", "/device/native-audio/"+id, session.CSRF, body)
		r.Body.Close()
		if r.StatusCode != 404 || len(called) != 0 {
			t.Fatal("unknown audio target reached broker", id, r.StatusCode)
		}
	}
	for _, id := range []string{"mute", "volume-correction"} {
		r = request(t, client, server, "PUT", "/device/native-audio/"+id, session.CSRF, body)
		r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatal("fixed update failed", r.StatusCode)
		}
	}
	r = request(t, client, server, "GET", "/device/native-audio", "", nil)
	r.Body.Close()
	if strings.Join(called, "|") != "PUT /native-audio/mute|PUT /native-audio/volume-correction|GET /native-audio" {
		t.Fatal("unexpected audio routing", called)
	}
}
