package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/np1-control-center/ncc/internal/config"
)

type screenBrokerTransport struct {
	requests []*http.Request
	bodies   []string
}

func (t *screenBrokerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.requests = append(t.requests, r.Clone(r.Context()))
	data, _ := io.ReadAll(r.Body)
	t.bodies = append(t.bodies, string(data))
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"accepted":true,"sequence":1}`))}, nil
}
func TestScreenLEDRoutesBoundTargetsAndAuditOnlyLifecycle(t *testing.T) {
	transport := &screenBrokerTransport{}
	s := &Server{client: &http.Client{Transport: transport}, mux: http.NewServeMux(), cfg: config.Config{StateDir: t.TempDir()}}
	s.registerScreenLEDRoutes()
	id := strings.Repeat("a", 64)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}
	if w := call("POST", "/api/v1/device/led/screen/sessions", "{}"); w.Code != 200 {
		t.Fatal(w.Code)
	}
	for i := 0; i < 5; i++ {
		if w := call("PUT", "/api/v1/device/led/screen/sessions/"+id, `{"sequence":1,"zones":[]}`); w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	if w := call("DELETE", "/api/v1/device/led/screen/sessions/"+id, ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	audit, e := os.ReadFile(filepath.Join(s.cfg.StateDir, "audit.jsonl"))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Count(strings.TrimSpace(string(audit)), "\n") != 1 || strings.Contains(string(audit), id) || strings.Contains(string(audit), "zones") {
		t.Fatalf("frame/token leaked into audit: %s", audit)
	}
	if len(transport.requests) != 7 || transport.requests[0].URL.Path != "/led/screen/sessions" || transport.requests[1].URL.Path != "/led/screen/sessions/"+id || transport.requests[1].Method != "PUT" {
		t.Fatal("incorrect broker target")
	}
	count := len(transport.requests)
	for _, path := range []string{"/api/v1/device/led/screen/sessions/invalid", "/api/v1/device/led/screen/sessions/" + id + "?path=/other"} {
		if w := call("PUT", path, "{}"); w.Code != 400 {
			t.Fatalf("invalid route accepted %s %d", path, w.Code)
		}
	}
	if len(transport.requests) != count {
		t.Fatal("invalid input reached broker")
	}
}
func TestScreenLEDUsesExistingCSRFBoundary(t *testing.T) {
	s, c, _ := fixture(t)
	csrf := login(t, c, s)
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		path := "/device/led/screen/sessions"
		if method != "POST" {
			path += "/" + strings.Repeat("a", 64)
		}
		res := request(t, c, s, method, path, "", map[string]any{})
		res.Body.Close()
		if res.StatusCode != 403 {
			t.Fatalf("%s bypassed CSRF: %d", method, res.StatusCode)
		}
	}
	res := request(t, c, s, "PUT", "/device/led/screen/sessions/invalid", csrf, map[string]any{})
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatal("valid session did not reach screen route", res.StatusCode)
	}
}
