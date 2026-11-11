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

type stockAuditTransport func(*http.Request) (*http.Response, error)

func (f stockAuditTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestStockServiceAuditPersistsFixedIDActionBeforeSubmission(t *testing.T) {
	state := t.TempDir()
	s := New(config.Config{NetworkTrust: true, StateDir: state})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile(filepath.Join(state, "audit.jsonl"))
		if err != nil || !strings.Contains(string(data), `"action":"stock-service.qmmf.stop"`) || !strings.Contains(string(data), `"result":"requested"`) {
			t.Error("request metadata was not durable before broker operation", err)
		}
		w.WriteHeader(409)
		_, _ = io.WriteString(w, `{"code":"SERVICE_DEPENDENCY_ACTIVE","reason":"fixture rejection"}`)
	}))
	defer upstream.Close()
	s.client.Transport = stockAuditTransport(func(r *http.Request) (*http.Response, error) {
		r.URL.Scheme = "http"
		r.URL.Host = strings.TrimPrefix(upstream.URL, "http://")
		return http.DefaultTransport.RoundTrip(r)
	})
	r := httptest.NewRequest("POST", "/api/v1/system/services/qmmf/actions", strings.NewReader(`{"action":"stop","expectedState":"active","expectedInvocationID":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`))
	r.SetPathValue("id", "qmmf")
	w := httptest.NewRecorder()
	s.controlStockService(w, r)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	data, _ := os.ReadFile(filepath.Join(state, "audit.jsonl"))
	if strings.Count(string(data), `"action":"stock-service.qmmf.stop"`) != 2 || !strings.Contains(string(data), `"result":"rejected"`) {
		t.Fatal("completion did not retain fixed action", string(data))
	}
}
