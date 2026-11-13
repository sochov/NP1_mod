package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/np1-control-center/ncc/internal/config"
	"github.com/np1-control-center/ncc/internal/model"
)

func TestTailnetNamesAreAcceptedButOtherNamesAreNot(t *testing.T) {
	var h tailnetHosts
	h.set(tailnetDNSName(model.Value("tailscale", map[string]any{"dnsName": "np1-control.tailcf2e47.ts.net"})))
	for _, host := range []string{"np1-control.tailcf2e47.ts.net", "NP1-Control.tailcf2e47.ts.net.", "np1-control"} {
		if !h.allowed(host) {
			t.Errorf("%s should be allowed", host)
		}
	}
	for _, host := range []string{"evil.example", "np1-control.example", "tailcf2e47.ts.net", "other.tailcf2e47.ts.net"} {
		if h.allowed(host) {
			t.Errorf("%s must be refused", host)
		}
	}
	for _, name := range []string{"", "np1-control.example.com", "ts.net", "-bad.tail.ts.net"} {
		h.set(name)
		if h.allowed("np1-control") || len(h.names) != 0 {
			t.Errorf("%q must not produce trusted names", name)
		}
	}
	if tailnetDNSName(model.Missing("tailscale", "offline")) != "" {
		t.Error("missing status must not yield a name")
	}
}
func TestHostCheckUsesTailnetNames(t *testing.T) {
	srv := New(config.Config{NetworkTrust: true, StateDir: t.TempDir()})
	srv.tailnet.set("np1-control.tailcf2e47.ts.net")
	for host, want := range map[string]int{"np1-control.tailcf2e47.ts.net": 200, "np1-control:80": 200, "evil.example": 403} {
		req := httptest.NewRequest("GET", "/healthz", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if (rec.Code == http.StatusForbidden) != (want == 403) {
			t.Errorf("%s: status %d", host, rec.Code)
		}
	}
}
