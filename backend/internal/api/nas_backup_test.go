package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/np1-control-center/ncc/internal/config"
)

func TestNASRoutesKeepFixedTargetsAndSecretsOutOfAudit(t *testing.T) {
	s := New(config.Config{NetworkTrust: true, StateDir: t.TempDir()})
	var called []string
	s.client.Transport = stockAuditTransport(func(r *http.Request) (*http.Response, error) {
		called = append(called, r.Method+" "+r.URL.Path)
		if r.URL.Host != "broker" {
			t.Fatal("unfixed destination")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":"available"}`))}, nil
	})
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	client := server.Client()
	client.Jar, _ = cookiejar.New(nil)
	r := request(t, client, server, "GET", "/auth/session", "", nil)
	var session struct{ CSRF string }
	_ = json.NewDecoder(r.Body).Decode(&session)
	r.Body.Close()
	body := map[string]any{"expectedRevision": 0, "password": "fixture-credential"}
	r = request(t, client, server, "PUT", "/nas-backup/policies", "", body)
	r.Body.Close()
	if r.StatusCode != 403 || len(called) != 0 {
		t.Fatal("unprotected setting change")
	}
	for _, v := range []struct{ method, path string }{{"GET", "/nas-backup"}, {"PUT", "/nas-backup/policies"}, {"POST", "/nas-backup/test"}, {"POST", "/nas-backup/jobs"}, {"DELETE", "/nas-backup/jobs/" + strings.Repeat("a", 32)}} {
		r = request(t, client, server, v.method, v.path, session.CSRF, body)
		r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatal(v.path, r.StatusCode)
		}
	}
	before := len(called)
	r = request(t, client, server, "DELETE", "/nas-backup/jobs/malformed", session.CSRF, nil)
	r.Body.Close()
	if r.StatusCode != 404 || len(called) != before {
		t.Fatal("unfixed job ID forwarded")
	}
	if strings.Join(called, "|") != "GET /nas-backup|PUT /nas-backup/policies|POST /nas-backup/test|POST /nas-backup/jobs|DELETE /nas-backup/jobs/"+strings.Repeat("a", 32) {
		t.Fatal(called)
	}
	audit, err := os.ReadFile(filepath.Join(s.cfg.StateDir, "audit.jsonl"))
	if err != nil || strings.Contains(string(audit), "fixture-credential") {
		t.Fatal("audit missing or contains credentials")
	}
}
