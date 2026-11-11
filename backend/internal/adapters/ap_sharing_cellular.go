package adapters

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/np1-control-center/ncc/internal/modem"
)

type apCellularProofDeps struct {
	verify   func() ([]string, error)
	readFile func(string) ([]byte, error)
	command  func(context.Context, ...string) (string, error)
	now      func() time.Time
}
type apCellularRouteProof struct {
	iface      string
	address    netip.Prefix
	gateway    string
	observedAt time.Time
}
type apCellularCommandOutput struct{ bytes.Buffer }

func (b *apCellularCommandOutput) Write(raw []byte) (int, error) {
	if b.Len()+len(raw) > 32768 {
		return 0, errors.New("AP_CELLULAR_OUTPUT_OVERSIZE")
	}
	return b.Buffer.Write(raw)
}
func apCellularIPCommand(parent context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/sbin/ip", args...)
	cmd.Env = []string{"PATH=/sbin:/bin:/usr/sbin:/usr/bin", "LC_ALL=C"}
	var out apCellularCommandOutput
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if cmd.Run() != nil {
		return "", errors.New("AP_CELLULAR_IP_READ_FAILED")
	}
	return strings.TrimSpace(out.String()), nil
}

// ReadVerifiedCellularUpstream never selects a modem merely because it is
// registered or has an AT PDP context. It requires live EC25 interface/route
// proof, and leaves DNS unknown until an interface-bound source is verified.
// In that case the AUTO controller must use a genuinely proven STA fallback.
func ReadVerifiedCellularUpstream(ctx context.Context) (APSharingUpstream, error) {
	proof, e := readAPCellularRouteProof(ctx, apCellularProofDeps{modem.VerifyCellularInterfaces, os.ReadFile, apCellularIPCommand, time.Now})
	if e != nil {
		return APSharingUpstream{}, e
	}
	dns, e := verifiedAPCellularDNS(ctx, proof)
	if e != nil {
		return APSharingUpstream{}, e
	}
	return newAPSharingUpstream("cellular", proof.iface, proof.address.Addr().String(), proof.address.Bits(), proof.gateway, dns, "linux-ec25-gobinet-route-bound-system-dns", proof.observedAt)
}
func apCellularAddress(raw, name string) (netip.Prefix, error) {
	var found netip.Prefix
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if line == "" {
			continue
		}
		words := strings.Fields(line)
		if len(words) < 4 || strings.SplitN(words[1], "@", 2)[0] != name || words[2] != "inet" {
			return netip.Prefix{}, errors.New("AP_CELLULAR_ADDRESS_INVALID")
		}
		value, e := netip.ParsePrefix(words[3])
		if e != nil || !value.Addr().Is4() {
			return netip.Prefix{}, errors.New("AP_CELLULAR_ADDRESS_INVALID")
		}
		if !value.Addr().IsGlobalUnicast() {
			continue
		}
		if found.IsValid() {
			return netip.Prefix{}, errors.New("AP_CELLULAR_ADDRESS_AMBIGUOUS")
		}
		found = value
	}
	if !found.IsValid() {
		return found, errors.New("AP_CELLULAR_IPV4_UNAVAILABLE")
	}
	return found, nil
}
func apCellularGateway(raw, name string, address netip.Addr) (string, bool, error) {
	var gateway string
	found := false
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		words := strings.Fields(line)
		if len(words) == 0 {
			continue
		}
		if words[0] != "default" {
			return "", false, errors.New("AP_CELLULAR_DEFAULT_INVALID")
		}
		iface := ""
		via := ""
		source := ""
		seen := map[string]bool{}
		for i := 1; i < len(words); i++ {
			key := words[i]
			if key == "onlink" {
				if seen[key] {
					return "", false, errors.New("AP_CELLULAR_ROUTE_OPTION_INVALID")
				}
				seen[key] = true
				continue
			}
			if i+1 >= len(words) || seen[key] {
				return "", false, errors.New("AP_CELLULAR_ROUTE_OPTION_INVALID")
			}
			seen[key] = true
			i++
			value := words[i]
			switch key {
			case "dev":
				iface = value
			case "via":
				via = value
			case "src":
				source = value
			case "metric":
				if _, e := strconv.ParseUint(value, 10, 32); e != nil {
					return "", false, errors.New("AP_CELLULAR_ROUTE_METRIC_INVALID")
				}
			case "proto":
				if value != "dhcp" && value != "static" && value != "boot" && value != "kernel" {
					return "", false, errors.New("AP_CELLULAR_ROUTE_PROTOCOL_UNSUPPORTED")
				}
			case "scope":
				if value != "link" {
					return "", false, errors.New("AP_CELLULAR_ROUTE_SCOPE_UNSUPPORTED")
				}
			default:
				return "", false, errors.New("AP_CELLULAR_ROUTE_OPTION_UNSUPPORTED")
			}
		}
		if iface != name {
			continue
		}
		if found {
			return "", false, errors.New("AP_CELLULAR_DEFAULT_AMBIGUOUS")
		}
		if source != "" && source != address.String() {
			return "", false, errors.New("AP_CELLULAR_ROUTE_SOURCE_MISMATCH")
		}
		if via != "" {
			ip, e := netip.ParseAddr(via)
			if e != nil || !ip.Is4() || !ip.IsGlobalUnicast() {
				return "", false, errors.New("AP_CELLULAR_GATEWAY_INVALID")
			}
		}
		found = true
		gateway = via
	}
	return gateway, found, nil
}

func apCellularLinkAvailable(dep apCellularProofDeps, name string) bool {
	state, e := dep.readFile("/sys/class/net/" + name + "/operstate")
	if e != nil {
		return false
	}
	value := strings.TrimSpace(string(state))
	flags, e := dep.readFile("/sys/class/net/" + name + "/flags")
	if e != nil {
		return false
	}
	bits, e := strconv.ParseUint(strings.TrimSpace(string(flags)), 0, 32)
	if e != nil || bits&1 == 0 {
		return false
	}
	if name == "usb0" {
		return value == "up"
	}
	// QMAP logical children may report UNKNOWN despite IFF_UP, an assigned
	// address and a real default. Their pinned physical EC25 parent must be UP.
	return (value == "up" || value == "unknown") && apCellularLinkAvailable(dep, "usb0")
}

func readAPCellularRouteProof(ctx context.Context, dep apCellularProofDeps) (apCellularRouteProof, error) {
	names, e := dep.verify()
	if e != nil {
		return apCellularRouteProof{}, errors.New("AP_CELLULAR_HARDWARE_UNVERIFIED")
	}
	routes, e := dep.command(ctx, "-4", "route", "show", "table", "main", "default")
	if e != nil {
		return apCellularRouteProof{}, e
	}
	var chosen apCellularRouteProof
	var index string
	for _, name := range names {
		if name != "usb0" && name != "usb0.1" && name != "usb0.2" {
			return apCellularRouteProof{}, errors.New("AP_CELLULAR_INTERFACE_UNSUPPORTED")
		}
		base := "/sys/class/net/" + name + "/"
		if !apCellularLinkAvailable(dep, name) {
			continue
		}
		id, e := dep.readFile(base + "ifindex")
		if e != nil {
			return apCellularRouteProof{}, errors.New("AP_CELLULAR_INDEX_UNAVAILABLE")
		}
		if value, e := strconv.ParseUint(strings.TrimSpace(string(id)), 10, 32); e != nil || value == 0 {
			return apCellularRouteProof{}, errors.New("AP_CELLULAR_INDEX_INVALID")
		}
		raw, e := dep.command(ctx, "-o", "-4", "address", "show", "dev", name)
		if e != nil {
			return apCellularRouteProof{}, e
		}
		address, e := apCellularAddress(raw, name)
		if e != nil {
			continue
		}
		gateway, present, e := apCellularGateway(routes, name, address.Addr())
		if e != nil {
			return apCellularRouteProof{}, e
		}
		if !present {
			continue
		}
		if chosen.iface != "" {
			return apCellularRouteProof{}, errors.New("AP_CELLULAR_UPSTREAM_AMBIGUOUS")
		}
		chosen = apCellularRouteProof{iface: name, address: address, gateway: gateway}
		index = strings.TrimSpace(string(id))
	}
	if chosen.iface == "" {
		return chosen, errors.New("AP_CELLULAR_LINUX_EGRESS_UNAVAILABLE")
	}
	verified, e := dep.verify()
	if e != nil || !reflect.DeepEqual(names, verified) {
		return apCellularRouteProof{}, errors.New("AP_CELLULAR_HARDWARE_CHANGED")
	}
	base := "/sys/class/net/" + chosen.iface + "/"
	id, e := dep.readFile(base + "ifindex")
	if e != nil || strings.TrimSpace(string(id)) != index {
		return apCellularRouteProof{}, errors.New("AP_CELLULAR_INTERFACE_CHANGED")
	}
	if !apCellularLinkAvailable(dep, chosen.iface) {
		return apCellularRouteProof{}, errors.New("AP_CELLULAR_LINK_CHANGED")
	}
	finalAddress, e := dep.command(ctx, "-o", "-4", "address", "show", "dev", chosen.iface)
	if e != nil {
		return apCellularRouteProof{}, e
	}
	address, e := apCellularAddress(finalAddress, chosen.iface)
	if e != nil || address != chosen.address {
		return apCellularRouteProof{}, errors.New("AP_CELLULAR_ADDRESS_CHANGED")
	}
	finalRoutes, e := dep.command(ctx, "-4", "route", "show", "table", "main", "default")
	if e != nil {
		return apCellularRouteProof{}, e
	}
	gateway, present, e := apCellularGateway(finalRoutes, chosen.iface, chosen.address.Addr())
	if e != nil || !present || gateway != chosen.gateway {
		return apCellularRouteProof{}, errors.New("AP_CELLULAR_DEFAULT_CHANGED")
	}
	chosen.observedAt = dep.now().UTC()
	return chosen, nil
}
