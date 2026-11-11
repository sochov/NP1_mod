package modem

import (
	"reflect"
	"testing"
)

func TestNativeAPNExactManagementDefaultAllowlist(t *testing.T) {
	raw := "default via 192.0.2.1 dev wlan0 proto dhcp src 192.0.2.9 metric 600"
	want := []string{"default", "via", "192.0.2.1", "dev", "wlan0", "proto", "dhcp", "src", "192.0.2.9", "metric", "600"}
	got, e := parseNativeAPNDefault(raw, "192.0.2.9")
	if e != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, e)
	}
	for _, bad := range []string{"default dev wlan0", "default via 192.0.2.1 dev usb0", "default via 192.0.2.1 dev wlan0 table 770", "default via 192.0.2.1 dev wlan0 metric 1 metric 2", "default via 192.0.2.1 dev wlan0 proto unknown", "default via 192.0.2.1 dev wlan0 src 192.0.2.10", raw + "\n" + raw, "default via $(x) dev wlan0", "default via 192.0.2.1 dev wlan0;cmd"} {
		if _, e := parseNativeAPNDefault(bad, "192.0.2.9"); e == nil {
			t.Fatal("unsafe route accepted", bad)
		}
	}
}
func TestNativeAPNWiFiRecoveryIdentity(t *testing.T) {
	raw := "wpa_state=COMPLETED\nssid=fixture\nid=2\nip_address=192.0.2.9\n"
	before, e := parseNativeAPNWiFi(raw)
	if e != nil {
		t.Fatal(e)
	}
	if !nativeAPNWiFiUnchanged(before, before) {
		t.Fatal("same profile rejected")
	}
	for _, after := range []nativeAPNWiFiState{{"COMPLETED", "other", "2", "192.0.2.9"}, {"DISCONNECTED", "fixture", "2", "192.0.2.9"}, {"COMPLETED", "fixture", "3", "192.0.2.9"}, {"COMPLETED", "fixture", "2", "192.0.2.10"}} {
		if nativeAPNWiFiUnchanged(before, after) {
			t.Fatal("changed profile accepted")
		}
	}
	for _, bad := range []string{raw + "ssid=duplicate\n", "wpa_state=COMPLETED\nssid=f\nid=-1\nip_address=192.0.2.9", "wpa_state=COMPLETED\nssid=f\nid=1\nip_address=not-an-ip"} {
		if _, e := parseNativeAPNWiFi(bad); e == nil {
			t.Fatal("bad state accepted")
		}
	}
	if got, e := nativeAPNAddressPrefix("2: wlan0 inet 192.0.2.9/24 brd 192.0.2.255 scope global wlan0"); e != nil || got != "192.0.2.9/24" {
		t.Fatal(got, e)
	}
}
func TestNativeAPNDataLinkDoesNotTreatLinkLocalAsEgress(t *testing.T) {
	for _, sample := range []struct {
		raw   string
		valid bool
	}{{"", false}, {"3: usb0.1@usb0 inet 100.64.0.2/30 scope global usb0.1", true}, {"3: usb0.1 inet 169.254.1.2/16 scope link usb0.1", false}, {"3: usb0.1 inet 127.0.0.1/8 scope host usb0.1", false}} {
		got, e := nativeAPNHasUsableIPv4(sample.raw, "usb0.1")
		if e != nil || got != sample.valid {
			t.Fatal(got, e)
		}
	}
	if _, e := nativeAPNHasUsableIPv4("3: wlan0 inet 192.0.2.2/24 scope global wlan0", "usb0.1"); e == nil {
		t.Fatal("wrong-interface address accepted")
	}
}
