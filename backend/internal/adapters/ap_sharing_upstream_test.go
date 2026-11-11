package adapters

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func sharingTestUpstream(t *testing.T, kind, iface string, at time.Time) APSharingUpstream {
	t.Helper()
	u, e := newAPSharingUpstream(kind, iface, "192.0.2.5", 24, "192.0.2.1", []string{"192.0.2.53"}, "synthetic-route-proof", at)
	if e != nil {
		t.Fatal(e)
	}
	return u
}
func TestAPSharingAUTORequiresFreshProofAndAccuratelyLabelsWiFi(t *testing.T) {
	now := time.Now()
	wifi := sharingTestUpstream(t, "wifi", "wlan0", now)
	cell := sharingTestUpstream(t, "cellular", "usb0.1", now)
	got, e := chooseAPSharingUpstream(cell, wifi, now)
	if e != nil || got.kind != "cellular" {
		t.Fatal("verified cellular was not preferred")
	}
	cell.observedAt = now.Add(-11 * time.Second)
	got, e = chooseAPSharingUpstream(cell, wifi, now)
	if e != nil || got.kind != "wifi" || got.iface != "wlan0" {
		t.Fatal("STA fallback was mislabeled or rejected")
	}
	wifi.observedAt = now.Add(-11 * time.Second)
	if _, e = chooseAPSharingUpstream(cell, wifi, now); e == nil {
		t.Fatal("stale upstream accepted")
	}
}
func TestAPSharingProofRejectsUnsafeInterfaceLoopDNSAndMissingDNS(t *testing.T) {
	now := time.Now()
	for _, test := range []struct {
		iface, ip, gw string
		dns           []string
	}{{"wlan0;cmd", "192.0.2.5", "192.0.2.1", []string{"192.0.2.53"}}, {"wlan0", "127.0.0.1", "", []string{"192.0.2.53"}}, {"wlan0", "192.0.2.5", "198.51.100.1", []string{"192.0.2.53"}}, {"wlan0", "192.0.2.5", "192.0.2.1", nil}, {"wlan0", "192.0.2.5", "192.0.2.1", []string{"192.168.226.1"}}} {
		if _, e := newAPSharingUpstream("wifi", test.iface, test.ip, 24, test.gw, test.dns, "synthetic", now); e == nil {
			t.Fatal("invalid proof accepted")
		}
	}
}
func TestAPSharingProofFormattingDoesNotExposeAddresses(t *testing.T) {
	u := sharingTestUpstream(t, "cellular", "usb0.1", time.Now())
	raw, _ := json.Marshal(u)
	for _, value := range []string{string(raw), fmt.Sprint(u)} {
		if strings.Contains(value, "192.0.2.") {
			t.Fatal("route/DNS address escaped opaque proof")
		}
	}
}
