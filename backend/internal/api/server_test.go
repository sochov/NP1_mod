package api

import (
	"bytes"
	"encoding/json"
	"github.com/np1-control-center/ncc/internal/config"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, e := url.Parse(s)
	if e != nil {
		t.Fatal(e)
	}
	return u
}

func fixture(t *testing.T) (*httptest.Server, *http.Client, string) {
	t.Helper()
	salt := []byte("test-salt-16bytes")
	c := config.Config{Username: "owner", Salt: salt, PasswordHash: config.Hash("fictional-test-password", salt), StateDir: t.TempDir()}
	s := httptest.NewTLSServer(New(c).Handler())
	t.Cleanup(s.Close)
	client := s.Client()
	client.Jar, _ = cookiejar.New(nil)
	return s, client, ""
}
func request(t *testing.T, client *http.Client, server *httptest.Server, method, path, csrf string, body any) *http.Response {
	t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	req, e := http.NewRequest(method, server.URL+"/api/v1"+path, bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Origin", server.URL)
	req.Header.Set("X-Request-ID", "test-request")
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	res, e := client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	return res
}
func login(t *testing.T, c *http.Client, s *httptest.Server) string {
	t.Helper()
	r := request(t, c, s, "POST", "/auth/login", "", map[string]string{"username": "owner", "password": "fictional-test-password"})
	defer r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("login %d", r.StatusCode)
	}
	cookies := r.Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe session cookie")
	}
	var v struct{ CSRF string }
	if e := json.NewDecoder(r.Body).Decode(&v); e != nil {
		t.Fatal(e)
	}
	return v.CSRF
}
func TestAuthenticationBoundary(t *testing.T) {
	s, c, _ := fixture(t)
	for _, p := range []string{"/system/summary", "/storage/file?path=test.mp4", "/camera/sessions/invalid/stream", "/events", "/logs", "/capabilities"} {
		r := request(t, c, s, "GET", p, "", nil)
		r.Body.Close()
		if r.StatusCode != 401 {
			t.Errorf("%s unauthenticated status=%d", p, r.StatusCode)
		}
	}
	csrf := login(t, c, s)
	r := request(t, c, s, "POST", "/auth/logout", csrf, nil)
	r.Body.Close()
	r = request(t, c, s, "GET", "/auth/session", "", nil)
	r.Body.Close()
	if r.StatusCode != 401 {
		t.Fatal("revoked session accepted")
	}
}
func TestCSRFOriginAndRevision(t *testing.T) {
	s, c, _ := fixture(t)
	csrf := login(t, c, s)
	body := map[string]any{"expectedRevision": 1, "settings": Settings{1, "dark", "ja", []string{}, "0"}}
	r := request(t, c, s, "PUT", "/settings", "", body)
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatal("missing csrf accepted")
	}
	req, _ := http.NewRequest("PUT", s.URL+"/api/v1/settings", bytes.NewBufferString(`{}`))
	req.Header.Set("Origin", "https://evil.invalid")
	req.Header.Set("X-CSRF-Token", csrf)
	r, e := c.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatal("cross-origin accepted")
	}
	r = request(t, c, s, "PUT", "/settings", csrf, body)
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("save status %d", r.StatusCode)
	}
	r = request(t, c, s, "PUT", "/settings", csrf, body)
	r.Body.Close()
	if r.StatusCode != 409 {
		t.Fatal("stale revision accepted")
	}
	r = request(t, c, s, "POST", "/backup/restore", csrf, map[string]any{"schemaVersion": 1, "expectedRevision": 2, "dryRun": true, "settings": Settings{1, "light", "ja", []string{}, "0"}})
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal("dryrun failed")
	}
	r = request(t, c, s, "GET", "/settings", "", nil)
	defer r.Body.Close()
	var actual Settings
	json.NewDecoder(r.Body).Decode(&actual)
	if actual.Theme != "dark" || actual.Revision != 2 {
		t.Fatal("dryrun changed settings")
	}
}

// An exported backup restores every field it carries: theme, locale, favourites and the 4G monthly limit.
func TestBackupRoundTripRestoresEveryField(t *testing.T) {
	s, c, _ := fixture(t)
	csrf := login(t, c, s)
	read := func() Settings {
		r := request(t, c, s, "GET", "/settings", "", nil)
		defer r.Body.Close()
		var v Settings
		json.NewDecoder(r.Body).Decode(&v)
		return v
	}
	save := func(v Settings) {
		r := request(t, c, s, "PUT", "/settings", csrf, map[string]any{"expectedRevision": read().Revision, "settings": v})
		r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatalf("save status %d", r.StatusCode)
		}
	}
	saved := Settings{0, "dark", "en", []string{"camera", "trips"}, "30000000000"}
	save(saved)
	// Files written by earlier versions (schema 1, display settings only) still restore.
	backup := struct{ Settings Settings }{read()}
	save(Settings{0, "light", "ja", []string{}, "0"})
	current := read()
	r := request(t, c, s, "POST", "/backup/restore", csrf, map[string]any{"schemaVersion": 1, "expectedRevision": current.Revision, "dryRun": false, "settings": backup.Settings})
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("restore status %d", r.StatusCode)
	}
	restored := read()
	if restored.Theme != saved.Theme || restored.Locale != saved.Locale || strings.Join(restored.Favorites, ",") != "camera,trips" || restored.CellularLimitBytes != saved.CellularLimitBytes {
		t.Fatalf("restore lost a field: %+v", restored)
	}
	if restored.Revision != current.Revision+1 {
		t.Fatalf("restore revision %d after %d", restored.Revision, current.Revision)
	}
}
func TestCellularLimitValidation(t *testing.T) {
	for _, value := range []string{"0", "30000000000", "100000000000000", ""} {
		if !validateSettings(Settings{1, "system", "ja", nil, value}) {
			t.Errorf("valid limit rejected: %q", value)
		}
	}
	for _, value := range []string{"-1", "1.5", "030000000000", "100000000000001", "18446744073709551616", "NaN"} {
		if validateSettings(Settings{1, "system", "ja", nil, value}) {
			t.Errorf("invalid limit accepted: %q", value)
		}
	}
}
func TestCameraSessionAndBackupRedaction(t *testing.T) {
	s, c, _ := fixture(t)
	csrf := login(t, c, s)
	r := request(t, c, s, "POST", "/camera/sessions", csrf, map[string]any{"source": "in"})
	r.Body.Close()
	if r.StatusCode != 201 {
		t.Fatal("camera should start without a permission prompt")
	}
	r = request(t, c, s, "POST", "/camera/sessions", csrf, map[string]any{"source": "front"})
	defer r.Body.Close()
	if r.StatusCode != 201 {
		t.Fatal("front camera should start directly")
	}
	r = request(t, c, s, "POST", "/backup/export", csrf, nil)
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	for _, secret := range []string{"fictional-test-password", "passwordHash", "salt", "test-salt"} {
		if bytes.Contains(b, []byte(secret)) {
			t.Fatalf("backup leaked %s", secret)
		}
	}
}
func TestLoginRateLimit(t *testing.T) {
	s, c, _ := fixture(t)
	for i := 0; i < 6; i++ {
		r := request(t, c, s, "POST", "/auth/login", "", map[string]string{"username": "owner", "password": "wrong"})
		r.Body.Close()
		if r.StatusCode != 429 {
			t.Fatalf("attempt %d accepted", i)
		}
	}
	r := request(t, c, s, "POST", "/auth/login", "", map[string]string{"username": "owner", "password": "fictional-test-password"})
	r.Body.Close()
	if r.StatusCode != 429 {
		t.Fatal("rate limit bypassed")
	}
}

func TestNetworkTrustHTTPRequiresNoPasswordButKeepsCSRF(t *testing.T) {
	s := httptest.NewServer(New(config.Config{NetworkTrust: true, StateDir: t.TempDir()}).Handler())
	defer s.Close()
	c := s.Client()
	c.Jar, _ = cookiejar.New(nil)
	r := request(t, c, s, "GET", "/settings", "", nil)
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal("Web password required on network-trust mode")
	}
	r = request(t, c, s, "GET", "/auth/session", "", nil)
	var session struct{ CSRF string }
	json.NewDecoder(r.Body).Decode(&session)
	r.Body.Close()
	if session.CSRF == "" {
		t.Fatal("no CSRF session")
	}
	cookies := c.Jar.Cookies(mustURL(t, s.URL))
	if len(cookies) != 1 || cookies[0].Name != "ncc_http_session" {
		t.Fatal("HTTP session cookie missing")
	}
	body := map[string]any{"expectedRevision": 1, "settings": Settings{1, "dark", "ja", []string{}, "0"}}
	r = request(t, c, s, "PUT", "/settings", "", body)
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatal("CSRF bypass")
	}
	r = request(t, c, s, "PUT", "/settings", session.CSRF, body)
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal("HTTP update failed")
	}
	req, _ := http.NewRequest("GET", s.URL+"/api/v1/settings", nil)
	req.Host = "attacker.example"
	r, e := c.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatal("DNS rebinding Host accepted")
	}
}
