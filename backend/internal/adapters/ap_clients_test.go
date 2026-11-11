package adapters

import (
	"fmt"
	"testing"
	"time"
)

func TestAPStationListCountsOnlyAssociatedStations(t *testing.T) {
	raw := "Selected interface 'nccap0'\n00:11:22:33:44:55\nflags=[AUTH][ASSOC][AUTHORIZED]\nconnected_time=120\nsignal=-58\n00:11:22:33:44:66\nflags=[AUTH]\nconnected_time=2\n"
	clients, e := apParseStations(raw)
	if e != nil || len(clients) != 1 || !clients[0].Authorized || clients[0].SignalDBm == nil || *clients[0].SignalDBm != -58 || clients[0].ConnectedSeconds == nil || *clients[0].ConnectedSeconds != 120 {
		t.Fatalf("associated clients %#v %v", clients, e)
	}
	empty, e := apParseStations("")
	if e != nil || empty == nil || len(empty) != 0 {
		t.Fatal("empty successful list must be []")
	}
}
func TestAPLeasesEnrichExistingStationWithoutInventingOnlineClients(t *testing.T) {
	now := time.Unix(2000, 0)
	raw := fmt.Sprintf("3000 00:11:22:33:44:55 192.168.226.20 example-device ignored-client-id\n1000 00:11:22:33:44:66 192.168.226.21 expired ignored\n3000 00:11:22:33:44:77 192.168.77.30 foreign-subnet ignored\n0 00:11:22:33:44:88 192.168.226.22 * ignored\n")
	leases := apParseLeases(raw, now)
	if len(leases) != 2 || leases["00:11:22:33:44:55"].Hostname != "example-device" || leases["00:11:22:33:44:88"].Hostname != "" {
		t.Fatalf("lease filtering %#v", leases)
	}
	clients, e := apParseStations("00:11:22:33:44:55\nflags=[ASSOC]\n")
	if e != nil {
		t.Fatal(e)
	}
	for i := range clients {
		lease := leases[clients[i].MAC]
		clients[i].IPv4 = lease.IPv4
	}
	if len(clients) != 1 || clients[0].IPv4 != "192.168.226.20" {
		t.Fatal("lease changed association membership")
	}
}
func TestAPStationMalformedOutputStaysUnknown(t *testing.T) {
	for _, raw := range []string{"FAIL", "00:11:22:33:44:55\nconnected_time=2", "00:11:22:33:44:55\nflags=[ASSOC]\n00:11:22:33:44:55\nflags=[ASSOC]", "02:bad:mac\nflags=[ASSOC]", "Selected interface 'wlan0'\n"} {
		if _, e := apParseStations(raw); e == nil {
			t.Fatalf("accepted malformed station output %q", raw)
		}
	}
}
