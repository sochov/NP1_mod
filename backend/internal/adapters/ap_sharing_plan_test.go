package adapters

import (
	"strings"
	"testing"
	"time"
)

func TestAPSharingPlanScopesNATForwardingDNSAndSourcePolicy(t *testing.T) {
	u := sharingTestUpstream(t, "wifi", "wlan0", time.Now())
	p, e := apBuildSharingPlan(strings.Repeat("a", 32), &u)
	if e != nil {
		t.Fatal(e)
	}
	text := p.restoreText()
	for _, required := range []string{"-s 192.168.226.0/24 -o wlan0 -j MASQUERADE", "--dport 53 -j ACCEPT", "--ctstate RELATED,ESTABLISHED -j ACCEPT", "-A NCC_AP_FWD_V1 -j DROP"} {
		if !strings.Contains(text, required) {
			t.Fatalf("missing scoped rule %s", required)
		}
	}
	for _, forbidden := range []string{"-F FORWARD", "-F INPUT", "-F POSTROUTING", "-P ", "usb0"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("unrelated firewall mutation %s", forbidden)
		}
	}
	if !strings.Contains(p.dns, "@wlan0") {
		t.Fatal("DNS queries do not follow selected egress")
	}
	if len(p.routes) != 3 || p.routes[0][0] != "blackhole" {
		t.Fatal("missing fail-closed private table")
	}
}
func TestAPSharingWANLossKeepsAPDNSInputButForwardsNothing(t *testing.T) {
	p, e := apBuildSharingPlan(strings.Repeat("b", 32), nil)
	if e != nil {
		t.Fatal(e)
	}
	text := p.restoreText()
	if strings.Contains(text, "MASQUERADE") || strings.Contains(text, "-o wlan0") || !strings.Contains(text, "--dport 53 -j ACCEPT") || !strings.Contains(text, "-A NCC_AP_FWD_V1 -j DROP") || len(p.routes) != 1 || p.dns != "" {
		t.Fatal("WAN loss plan leaked to another interface or removed local DNS")
	}
}
func TestAPSharingMarkerDoesNotReturnBeforeForwardingRules(t *testing.T) {
	u := sharingTestUpstream(t, "wifi", "wlan0", time.Now())
	p, e := apBuildSharingPlan(strings.Repeat("c", 32), &u)
	if e != nil {
		t.Fatal(e)
	}
	rules := p.filter[apSharingForwardChain]
	if rules[len(rules)-2][1] != "DROP" || rules[len(rules)-1][len(rules[len(rules)-1])-1] != "RETURN" {
		t.Fatal("owner marker bypasses terminal forwarding guard")
	}
	if _, e = apBuildSharingPlan("owner\nCOMMIT", &u); e == nil {
		t.Fatal("invalid owner token accepted")
	}
}
