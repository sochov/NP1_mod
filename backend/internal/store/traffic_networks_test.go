package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func trafficItems(cellRX, cellTX, wifiRX, wifiTX string) []map[string]string {
	return []map[string]string{{"interface": "usb0", "rxBytes": cellRX, "txBytes": cellTX}, {"interface": "wlan0", "rxBytes": wifiRX, "txBytes": wifiTX}, {"interface": "usb0.1", "rxBytes": "999999", "txBytes": "999999"}, {"interface": "bridge0", "rxBytes": "999999", "txBytes": "999999"}, {"interface": "wlan1", "rxBytes": "999999", "txBytes": "999999"}, {"interface": "tailscale0", "rxBytes": "999999", "txBytes": "999999"}}
}

var networkRoles = NetworkInterfaces{Cellular: "usb0", WiFi: "wlan0"}

func network(t *testing.T, s *Traffic, now time.Time, name string) map[string]any {
	t.Helper()
	summary := s.Summary("wlan0", now)
	return summary["networks"].(map[string]any)[name].(map[string]any)
}
func TestTrafficNetworksSeparatePhysicalParentsAndRates(t *testing.T) {
	s := NewTraffic(t.TempDir())
	at := time.Date(2026, 11, 11, 1, 0, 0, 0, time.UTC)
	s.FeedNetworks(at, "a", trafficItems("1000", "2000", "3000", "4000"), networkRoles)
	first := network(t, s, at, "cellular")
	if first["monthRX"] != nil || first["rxBytesPerSecond"] != nil || first["speedStatus"] != "baseline" {
		t.Fatal(first)
	}
	second := trafficItems("1200", "2100", "3100", "4040")
	for _, item := range second {
		if item["interface"] != "usb0" && item["interface"] != "wlan0" {
			item["rxBytes"] = "999999999"
			item["txBytes"] = "999999999"
		}
	}
	s.FeedNetworks(at.Add(2*time.Second), "a", second, networkRoles)
	cell, wifi := network(t, s, at.Add(2*time.Second), "cellular"), network(t, s, at.Add(2*time.Second), "wifi")
	if cell["todayRX"] != "200" || cell["todayTX"] != "100" || cell["rxBytesPerSecond"] != float64(100) || cell["txBytesPerSecond"] != float64(50) {
		t.Fatal(cell)
	}
	if wifi["monthRX"] != "100" || wifi["monthTX"] != "40" || wifi["rxBytesPerSecond"] != float64(50) || wifi["txBytesPerSecond"] != float64(20) {
		t.Fatal(wifi)
	}
	if cell["monthObservedSeconds"] != float64(2) {
		t.Fatal(cell)
	}
}
func TestTrafficNetworkZeroIsMeasuredAndStaleRateIsNull(t *testing.T) {
	s := NewTraffic(t.TempDir())
	at := time.Now().UTC()
	items := trafficItems("100", "100", "100", "100")
	s.FeedNetworks(at, "a", items, networkRoles)
	s.FeedNetworks(at.Add(2*time.Second), "a", items, networkRoles)
	v := network(t, s, at.Add(2*time.Second), "cellular")
	if v["monthRX"] != "0" || v["rxBytesPerSecond"] != float64(0) || v["status"] != "available" {
		t.Fatal(v)
	}
	v = network(t, s, at.Add(9*time.Second), "cellular")
	if v["rxBytesPerSecond"] != nil || v["speedStatus"] != "stale" || v["stale"] != true {
		t.Fatal(v)
	}
}
func TestTrafficNetworkRebootResetAndLongGap(t *testing.T) {
	s := NewTraffic(t.TempDir())
	at := time.Now().UTC()
	s.FeedNetworks(at, "a", trafficItems("100", "100", "100", "100"), networkRoles)
	s.FeedNetworks(at.Add(2*time.Second), "a", trafficItems("200", "150", "100", "100"), networkRoles)
	s.FeedNetworks(at.Add(4*time.Second), "b", trafficItems("10", "5", "10", "5"), networkRoles)
	v := network(t, s, at.Add(4*time.Second), "cellular")
	if v["monthRX"] != "100" || v["speedStatus"] != "reboot" || v["rxBytesPerSecond"] != nil {
		t.Fatal(v)
	}
	s.FeedNetworks(at.Add(6*time.Second), "b", trafficItems("5", "2", "10", "5"), networkRoles)
	v = network(t, s, at.Add(6*time.Second), "cellular")
	if v["monthRX"] != "100" || v["speedStatus"] != "reset" || v["rxBytesPerSecond"] != nil {
		t.Fatal(v)
	}
	s.FeedNetworks(at.Add(8*time.Second), "b", trafficItems("25", "12", "10", "5"), networkRoles)
	v = network(t, s, at.Add(8*time.Second), "cellular")
	if v["rxBytesPerSecond"] != float64(10) || v["monthRX"] != "120" {
		t.Fatal(v)
	}
	s.FeedNetworks(at.Add(30*time.Second), "b", trafficItems("125", "62", "10", "5"), networkRoles)
	v = network(t, s, at.Add(30*time.Second), "cellular")
	if v["monthRX"] != "220" || v["speedStatus"] != "gap" || v["rxBytesPerSecond"] != nil {
		t.Fatal(v)
	}
}
func TestTrafficNetworkActualElapsedAndNoInventedMissing(t *testing.T) {
	s := NewTraffic(t.TempDir())
	at := time.Now().UTC()
	s.FeedNetworks(at, "a", trafficItems("100", "100", "100", "100"), networkRoles)
	s.FeedNetworks(at.Add(4*time.Second), "a", trafficItems("200", "120", "100", "100"), networkRoles)
	v := network(t, s, at.Add(4*time.Second), "cellular")
	if v["rxBytesPerSecond"] != float64(25) || v["txBytesPerSecond"] != float64(5) {
		t.Fatal(v)
	}
	s.FeedNetworks(at.Add(6*time.Second), "a", trafficItems("invalid", "120", "100", "100"), networkRoles)
	v = network(t, s, at.Add(6*time.Second), "cellular")
	if v["rxBytesPerSecond"] != nil || v["speedStatus"] != "unknown" || v["monthRX"] != "100" {
		t.Fatal(v)
	}
	s.FeedNetworks(at.Add(8*time.Second), "a", trafficItems("400", "140", "100", "100"), networkRoles)
	v = network(t, s, at.Add(8*time.Second), "cellular")
	if v["speedStatus"] != "baseline" || v["monthRX"] != "100" {
		t.Fatal(v)
	}
	s.FeedNetworks(at.Add(7*time.Second), "a", trafficItems("999", "999", "999", "999"), networkRoles)
	if v2 := network(t, s, at.Add(8*time.Second), "cellular"); v2["monthRX"] != v["monthRX"] {
		t.Fatal(v2)
	}
}
func TestTrafficNetworkRoleChangeAndInactiveNoDoubleCount(t *testing.T) {
	s := NewTraffic(t.TempDir())
	at := time.Now().UTC()
	s.FeedNetworks(at, "a", trafficItems("100", "100", "100", "100"), networkRoles)
	s.FeedNetworks(at.Add(2*time.Second), "a", trafficItems("200", "150", "150", "120"), networkRoles)
	s.FeedNetworks(at.Add(4*time.Second), "a", trafficItems("300", "200", "200", "140"), NetworkInterfaces{Cellular: "usb0"})
	v := network(t, s, at.Add(4*time.Second), "wifi")
	if v["speedStatus"] != "inactive" || v["active"] != false || v["monthRX"] != "50" {
		t.Fatal(v)
	}
	s.FeedNetworks(at.Add(6*time.Second), "a", trafficItems("400", "250", "250", "160"), NetworkInterfaces{Cellular: "wlan0", WiFi: "wlan0"})
	for _, kind := range []string{"cellular", "wifi"} {
		v = network(t, s, at.Add(6*time.Second), kind)
		if v["rxBytesPerSecond"] != nil || v["active"] != false {
			t.Fatal(v)
		}
	}
}
func TestTrafficNetworkPersistenceOldStateAndMonth(t *testing.T) {
	dir := t.TempDir()
	old := TrafficState{StartedAt: time.Now(), Previous: map[string]Counter{}, Days: map[string]map[string]Counter{}}
	raw, _ := json.Marshal(old)
	os.WriteFile(filepath.Join(dir, "traffic.json"), raw, 0600)
	s := NewTraffic(dir)
	at := time.Date(2026, 11, 30, 23, 59, 58, 0, trafficJapan)
	s.FeedNetworks(at, "a", trafficItems("100", "100", "100", "100"), networkRoles)
	s.FeedNetworks(at.Add(time.Second), "a", trafficItems("200", "110", "100", "100"), networkRoles)
	if e := s.Save(); e != nil {
		t.Fatal(e)
	}
	s = NewTraffic(dir)
	s.FeedNetworks(at.Add(3*time.Second), "a", trafficItems("300", "120", "100", "100"), networkRoles)
	v := network(t, s, at.Add(3*time.Second), "cellular")
	if v["monthRX"] != "100" || v["totalRX"] != "200" || v["rxBytesPerSecond"] != float64(50) || v["monthObservedSeconds"] != float64(1) {
		t.Fatal(v)
	}
	if s.Summary("", at.Add(3*time.Second)) == nil {
		t.Fatal("networks must remain available without default route")
	}
}
func TestTrafficNetworkWrapAndUnknownBoot(t *testing.T) {
	s := NewTraffic(t.TempDir())
	at := time.Now().UTC()
	s.FeedNetworks(at, "a", trafficItems("18446744073709551611", "100", "100", "100"), networkRoles)
	s.FeedNetworks(at.Add(2*time.Second), "a", trafficItems("3", "100", "100", "100"), networkRoles)
	v := network(t, s, at.Add(2*time.Second), "cellular")
	if v["monthRX"] != "8" || v["rxBytesPerSecond"] != float64(4) {
		t.Fatal(v)
	}
	s.FeedNetworks(at.Add(4*time.Second), "", trafficItems("20", "100", "100", "100"), networkRoles)
	v = network(t, s, at.Add(4*time.Second), "cellular")
	if v["monthRX"] != "8" || v["speedStatus"] != "unknown" || v["rxBytesPerSecond"] != nil {
		t.Fatal(v)
	}
}
