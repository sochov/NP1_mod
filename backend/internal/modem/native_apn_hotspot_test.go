package modem

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func hotspotManagementFixture(t *testing.T) (nativeAPNHotspotIO, map[string][]byte) {
	t.Helper()
	now := time.Now().UTC()
	files := map[string][]byte{
		"/proc/sys/kernel/random/boot_id":            []byte("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee\n"),
		"/sys/class/net/nccap0/ifindex":              []byte("17\n"),
		"/sys/class/net/nccap0/flags":                []byte("0x1003\n"),
		"/run/np1-control-ap/private/ownership.json": []byte(`{"ifIndex":17,"bootId":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee","hostapd":{"pid":50,"start":"1000"},"dhcp":{"pid":51,"start":"1001"}}`),
	}
	files["/proc/50/stat"] = []byte("50 (fixture ap) S " + strings.Repeat("0 ", 18) + "1000\n")
	files["/proc/51/stat"] = []byte("51 (fixture dhcp) S " + strings.Repeat("0 ", 18) + "1001\n")
	state, _ := json.Marshal(map[string]any{"enabled": true, "dhcpReady": true, "state": "enabled", "updatedAt": now})
	files["/run/np1-control-ap/status.json"] = state
	d := nativeAPNHotspotIO{
		read: func(path string) ([]byte, error) {
			value, ok := files[path]
			if !ok {
				return nil, errors.New("absent")
			}
			return value, nil
		},
		command: func(_ context.Context, name string, args ...string) (string, error) {
			if name == "/bin/systemctl" {
				return "Id=np1-control-ap.service\nMainPID=45\nInvocationID=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\nActiveState=active\nSubState=running\nJob=", nil
			}
			if name == "/sbin/ip" && strings.Join(args, " ") == "-o -4 address show dev nccap0" {
				return "17: nccap0 inet 192.0.2.1/24 brd 192.0.2.255 scope global nccap0", nil
			}
			t.Fatal("unexpected management command", name, args)
			return "", errors.New("unexpected")
		},
		http: func(_ context.Context, address string) error {
			if address != "192.0.2.1" {
				t.Fatal("wrong HTTP management target", address)
			}
			return nil
		},
		now: func() time.Time { return now },
	}
	return d, files
}

func TestAPNHotspotManagementKeepsSeparateWitness(t *testing.T) {
	d, _ := hotspotManagementFixture(t)
	before, err := readNativeAPNHotspot(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	baseline := nativeAPNNetworkBaseline{hotspot: &before}
	if !nativeAPNManagementReady(baseline) || nativeAPNManagementKind(baseline) != "hotspot" {
		t.Fatal("verified AP was not a management path")
	}
	after, err := readNativeAPNHotspot(context.Background(), d)
	if err != nil || after != before {
		t.Fatal("same management identity changed", err)
	}
	d.http = func(context.Context, string) error { return errors.New("HTTP unavailable") }
	if _, err = readNativeAPNHotspot(context.Background(), d); err == nil {
		t.Fatal("interface alone fabricated HTTP readiness")
	}
}

func TestAPNHotspotManagementRejectsStaleOrForeignEvidence(t *testing.T) {
	for _, name := range []string{"stale", "future", "down", "no-dhcp", "wrong-boot", "wrong-index", "missing-owner", "changed-process", "dead-dhcp", "pid-reused"} {
		t.Run(name, func(t *testing.T) {
			d, files := hotspotManagementFixture(t)
			switch name {
			case "stale", "future", "no-dhcp":
				stamp := d.now().Add(-8 * time.Second)
				ready := true
				if name == "future" {
					stamp = d.now().Add(2 * time.Second)
				}
				if name == "no-dhcp" {
					stamp = d.now()
					ready = false
				}
				files["/run/np1-control-ap/status.json"], _ = json.Marshal(map[string]any{"enabled": true, "dhcpReady": ready, "state": "enabled", "updatedAt": stamp})
			case "down":
				files["/sys/class/net/nccap0/flags"] = []byte("0x1002")
			case "wrong-boot":
				files["/proc/sys/kernel/random/boot_id"] = []byte("11111111-2222-3333-4444-555555555555")
			case "wrong-index":
				files["/sys/class/net/nccap0/ifindex"] = []byte("18")
			case "missing-owner":
				delete(files, "/run/np1-control-ap/private/ownership.json")
			case "dead-dhcp":
				delete(files, "/proc/51/stat")
			case "pid-reused":
				files["/proc/50/stat"] = []byte("50 (reused) S " + strings.Repeat("0 ", 18) + "1002\n")
			case "changed-process":
				original := d.command
				d.command = func(ctx context.Context, path string, args ...string) (string, error) {
					v, e := original(ctx, path, args...)
					return strings.ReplaceAll(v, "MainPID=45", "MainPID=0"), e
				}
			}
			if _, err := readNativeAPNHotspot(context.Background(), d); err == nil {
				t.Fatal("unproven management path accepted")
			}
		})
	}
}

func TestAPNRefusesWriteWithoutManagementPath(t *testing.T) {
	dep, _, writes := nativeAPNFixtureIO(t)
	dep.network = func(context.Context) (nativeAPNNetworkBaseline, error) { return nativeAPNNetworkBaseline{}, nil }
	result, err := runNativeAPNTransaction(context.Background(), nativeAPNTarget(t), dep)
	if err == nil || err.Error() != "APN_MANAGEMENT_PATH_UNVERIFIED" || *writes != 0 || result.Attempted {
		t.Fatal("APN was written without management path", err, *writes)
	}
}

func TestAPNHotspotHTTPWitnessChecksActualUI(t *testing.T) {
	if !nativeAPNHotspotHTTPResponse(200, []byte(`<!doctype html><div id="app"></div>`)) {
		t.Fatal("actual UI marker rejected")
	}
	if nativeAPNHotspotHTTPResponse(302, []byte(`<div id="app">`)) || nativeAPNHotspotHTTPResponse(200, []byte("different page")) || nativeAPNHotspotHTTPResponse(200, []byte(`<div id="app">`+strings.Repeat("x", 8192))) {
		t.Fatal("redirect/foreign/oversized page accepted")
	}
}

func TestAPNUnobservedWiFiRequiresNoAddressOrDefault(t *testing.T) {
	addresses := "1: lo inet 127.0.0.1/8 scope host lo\n17: nccap0 inet 192.0.2.1/24 scope global nccap0\n"
	routes := "default dev eth0 src 198.51.100.7 metric 10"
	if !nativeAPNUnobservedWiFiInactive(addresses, routes) || !nativeAPNUnobservedWiFiInactive("", "") {
		t.Fatal("absent STA should allow independently proven AP management")
	}
	for _, test := range []struct{ addresses, routes string }{
		{addresses + "2: wlan0 inet 192.0.2.8/24 scope global wlan0", routes},
		{addresses + "2: wlan0@parent inet 169.254.1.1/16 scope link wlan0", routes},
		{addresses, "default via 192.0.2.1 dev wlan0 metric 10"},
		{"malformed", routes}, {addresses, "default dev"}, {addresses, "default via 192.0.2.1"},
		{addresses, "default dev eth0 dev eth1"},
	} {
		if nativeAPNUnobservedWiFiInactive(test.addresses, test.routes) {
			t.Fatal("possibly active or unverified STA accepted")
		}
	}
}

func TestAPNStockBaselineRespectsUserStoppedUnrelatedServices(t *testing.T) {
	row := "Id=app-@DriveRec.service\nMainPID=0\nInvocationID=\nActiveState=inactive\nSubState=dead"
	if !nativeAPNStockStateSafe("app-@DriveRec", row) {
		t.Fatal("user-stopped recording should be preserved")
	}
	for _, unit := range []string{"app-@Communication", "app-@Setting", "NEventWatcherCom"} {
		if nativeAPNStockStateSafe(unit, strings.ReplaceAll(row, "app-@DriveRec", unit)) {
			t.Fatal("inactive owner accepted")
		}
	}
	for _, invalid := range []string{strings.ReplaceAll(row, "inactive", "failed"), row + "\nMainPID=0", strings.ReplaceAll(row, "MainPID=0", "MainPID=12")} {
		if nativeAPNStockStateSafe("app-@DriveRec", invalid) {
			t.Fatal("unstable or inconsistent state accepted")
		}
	}
}
