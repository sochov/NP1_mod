package adapters

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNASHTTPPoliciesAreIndependentAndCredentialsNeverReturned(t *testing.T) {
	c := newNASBackupController(privateTempDir(t))
	mux := http.NewServeMux()
	registerNASBackupHandlers(mux, c)
	body := `{"expectedRevision":0,"policies":{"video":{"enabled":true,"route":"wifi","timing":"ssid","ssid":"fixture-home","targets":{"wifi":{"protocol":"smb","host":"nas.example.invalid","share":"clips","directory":"np1","username":"fixture-user","password":"fixture-secret"},"tailscale":{"protocol":"sftp","host":"nas.fixture.ts.net","directory":"np1","username":"other-user","password":"other-secret","hostKeySHA256":"SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}}}}}`
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("PUT", "/nas-backup/policies", strings.NewReader(body)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, secret := range []string{"fixture-user", "fixture-secret", "other-user", "other-secret"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("credential returned")
		}
	}
	p, err := c.policies()
	if err != nil {
		t.Fatal(err)
	}
	if !p.View().Policies["video"].Enabled || p.View().Policies["event"].Enabled || p.View().Policies["video"].Targets["tailscale"].Protocol != "sftp" {
		t.Fatal("independent policies lost")
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("PUT", "/nas-backup/policies", strings.NewReader(body)))
	if w.Code != 409 {
		t.Fatal("stale update accepted", w.Code)
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/nas-backup", nil))
	var observed struct {
		Data NASBackupData `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &observed) != nil || observed.Data.Settings.Revision != 1 {
		t.Fatal("status unavailable")
	}
}

func TestNASHTTPInvalidRequestsCannotStartNetworkOrJobs(t *testing.T) {
	c := newNASBackupController(privateTempDir(t))
	mux := http.NewServeMux()
	registerNASBackupHandlers(mux, c)
	for _, test := range []struct{ method, path, body string }{
		{"PUT", "/nas-backup/policies", `{"policies":{"event":{"enabled":true}}}`},
		{"PUT", "/nas-backup/policies", `{"expectedRevision":0,"policies":{"other":{}}}`},
		{"PUT", "/nas-backup/policies", `{"expectedRevision":0,"policies":{},"command":"x"}`},
		{"POST", "/nas-backup/test", `{"expectedRevision":0,"kind":"video","route":"cellular"}`},
		{"POST", "/nas-backup/jobs", `{"expectedRevision":0,"recordingID":""}`},
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(test.method, test.path, strings.NewReader(test.body)))
		if w.Code != 400 {
			t.Fatal(test.path, w.Code, w.Body.String())
		}
	}
	if c.job != nil || c.testing {
		t.Fatal("invalid input created work")
	}
}
