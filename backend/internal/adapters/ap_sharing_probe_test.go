package adapters

import "testing"

func TestAPSharingRouteProofMustMatchSelectedInterfaceAndSource(t *testing.T) {
	valid := "1.1.1.1 via 192.0.2.1 dev wlan0 src 192.0.2.5 uid 0\n cache"
	gateway, e := apParseUpstreamRoute(valid, "wlan0", "192.0.2.5")
	if e != nil || gateway != "192.0.2.1" {
		t.Fatal("valid route rejected")
	}
	for _, raw := range []string{valid, "unreachable 1.1.1.1", "local 1.1.1.1 dev lo src 192.0.2.5", "1.1.1.1 dev tailscale0 src 192.0.2.5"} {
		if _, e := apParseUpstreamRoute(raw, "usb0.1", "192.0.2.5"); e == nil {
			t.Fatal("STA or invalid route mislabeled as cellular")
		}
	}
}
func TestAPSharingResolverFiltersStubsAndOwnDNSWithoutPublishingDomains(t *testing.T) {
	values := apResolverIPv4("search private.invalid\nnameserver 127.0.0.1\nnameserver 192.168.226.1\nnameserver 192.0.2.53\nnameserver 192.0.2.53\nnameserver ::1\n")
	if len(values) != 1 || values[0] != "192.0.2.53" {
		t.Fatal("resolver contains local loop, duplicates or non-IPv4")
	}
}
