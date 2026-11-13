package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/model"
)

// tailnetHosts holds this device's own Tailscale MagicDNS names (the full ts.net name and
// its short form). The host check accepts them in addition to IP addresses: they resolve
// only inside the owner's tailnet and only to this device, so they do not reopen the
// DNS-rebinding hole that the check closes for arbitrary names.
type tailnetHosts struct {
	mu    sync.RWMutex
	names map[string]bool
}

func (t *tailnetHosts) allowed(host string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.names[strings.ToLower(strings.TrimSuffix(host, "."))]
}

func (t *tailnetHosts) set(dnsName string) {
	names := map[string]bool{}
	dnsName = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(dnsName), "."))
	// Only an actual tailnet name is accepted: <host>.<tailnet>.ts.net.
	if parts := strings.Split(dnsName, "."); len(parts) >= 4 && strings.HasSuffix(dnsName, ".ts.net") && validLabel(parts[0]) {
		names[dnsName] = true
		names[parts[0]] = true
	}
	t.mu.Lock()
	t.names = names
	t.mu.Unlock()
}

func validLabel(label string) bool {
	if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
		return false
	}
	for _, r := range label {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// tailnetDNSName extracts dnsName from the broker's tailscale observation.
func tailnetDNSName(o model.Observation) string {
	if o.Status != "available" {
		return ""
	}
	b, _ := json.Marshal(o.Data)
	var v struct {
		DNSName string `json:"dnsName"`
	}
	if json.Unmarshal(b, &v) != nil {
		return ""
	}
	return v.DNSName
}

// collectTailnetHosts refreshes the names every minute. A failed refresh keeps the last
// known names; a successful one without a name clears them.
func (s *Server) collectTailnetHosts(ctx context.Context) {
	refresh := func() {
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://broker/observe", nil)
		resp, e := s.client.Do(req)
		if e != nil {
			return
		}
		defer resp.Body.Close()
		var m map[string]model.Observation
		if json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&m) != nil {
			return
		}
		s.tailnet.set(tailnetDNSName(m["tailscale"]))
	}
	refresh()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refresh()
		}
	}
}
