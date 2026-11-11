package adapters

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func cellularProofFixture() (apCellularProofDeps, map[string]string) {
	files := map[string]string{
		"/sys/class/net/usb0/operstate": "up", "/sys/class/net/usb0/flags": "0x1003", "/sys/class/net/usb0/ifindex": "7",
		"/sys/class/net/usb0.1/operstate": "unknown", "/sys/class/net/usb0.1/flags": "0x1003", "/sys/class/net/usb0.1/ifindex": "8",
	}
	dep := apCellularProofDeps{
		verify: func() ([]string, error) { return []string{"usb0", "usb0.1"}, nil },
		readFile: func(p string) ([]byte, error) {
			if v, ok := files[p]; ok {
				return []byte(v), nil
			}
			return nil, errors.New("missing")
		},
		command: func(_ context.Context, args ...string) (string, error) {
			if strings.Join(args, " ") == "-4 route show table main default" {
				return "default via 100.64.1.1 dev usb0.1 proto static src 100.64.1.2", nil
			}
			if args[len(args)-1] == "usb0.1" {
				return "8: usb0.1 inet 100.64.1.2/24 scope global usb0.1", nil
			}
			return "", nil
		}, now: func() time.Time { return time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC) },
	}
	return dep, files
}

func TestCellularProofRequiresLiveHardwareIPAndMainDefault(t *testing.T) {
	dep, files := cellularProofFixture()
	proof, e := readAPCellularRouteProof(context.Background(), dep)
	if e != nil || proof.iface != "usb0.1" || proof.address != netip.MustParsePrefix("100.64.1.2/24") || proof.observedAt.IsZero() {
		t.Fatal("QMAP child proof failed", e)
	}
	// This proves route setup only. Missing interface-bound DNS must still keep
	// AUTO sharing on its independently verified STA fallback.
	_, e = newAPSharingUpstream("cellular", proof.iface, proof.address.Addr().String(), proof.address.Bits(), proof.gateway, nil, "linux-ec25-gobinet-route", proof.observedAt)
	if e == nil || e.Error() != "AP_UPSTREAM_DNS_UNVERIFIED" {
		t.Fatal(e)
	}
	for _, path := range []string{"/sys/class/net/usb0/operstate", "/sys/class/net/usb0.1/flags"} {
		old := files[path]
		files[path] = "0"
		if _, e := readAPCellularRouteProof(context.Background(), dep); e == nil {
			t.Fatal("down parent/child accepted")
		}
		files[path] = old
	}
	dep.verify = func() ([]string, error) { return []string{"wlan0"}, nil }
	if _, e := readAPCellularRouteProof(context.Background(), dep); e == nil {
		t.Fatal("STA misclassified as cellular")
	}
}

func TestCellularProofRejectsResetOrRouteAddressChangeDuringRead(t *testing.T) {
	for _, change := range []string{"index", "hardware", "address", "route"} {
		t.Run(change, func(t *testing.T) {
			dep, files := cellularProofFixture()
			originalVerify, originalCommand := dep.verify, dep.command
			verifications := 0
			routeReads, addressReads := 0, 0
			dep.verify = func() ([]string, error) {
				verifications++
				if verifications == 2 {
					if change == "index" {
						files["/sys/class/net/usb0.1/ifindex"] = "99"
					}
					if change == "hardware" {
						return nil, errors.New("changed")
					}
				}
				return originalVerify()
			}
			dep.command = func(ctx context.Context, args ...string) (string, error) {
				if args[0] == "-4" {
					routeReads++
					if change == "route" && routeReads == 2 {
						return "", nil
					}
				}
				if args[len(args)-1] == "usb0.1" {
					addressReads++
					if change == "address" && addressReads == 2 {
						return "8: usb0.1 inet 100.64.1.3/24 scope global usb0.1", nil
					}
				}
				return originalCommand(ctx, args...)
			}
			if _, e := readAPCellularRouteProof(context.Background(), dep); e == nil {
				t.Fatal("changing proof accepted")
			}
		})
	}
}

func TestCellularProofRejectsAmbiguousOrUnusableValues(t *testing.T) {
	for _, raw := range []string{"8: usb0.1 inet 169.254.2.2/16", "8: wlan0 inet 100.64.1.2/24", "8: usb0.1 inet 100.64.1.2/24\n8: usb0.1 inet 100.64.1.3/24"} {
		if _, e := apCellularAddress(raw, "usb0.1"); e == nil {
			t.Fatal("ambiguous/unusable address accepted")
		}
	}
	address := netip.MustParseAddr("100.64.1.2")
	for _, raw := range []string{"default dev usb0.1 nexthop dev wlan0", "default dev usb0.1 src 100.64.1.3", "default dev usb0.1\ndefault dev usb0.1"} {
		if _, _, e := apCellularGateway(raw, "usb0.1", address); e == nil {
			t.Fatal("ambiguous route accepted")
		}
	}
}
