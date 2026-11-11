package modem

import (
	"context"
	"errors"
	"net/netip"
	"strconv"
	"strings"
)

type NativeAPNNetworkCheck struct {
	Ready                     bool              `json:"ready"`
	ManagementWiFiCompleted   bool              `json:"managementWiFiCompleted"`
	RestorableDefaultCaptured bool              `json:"restorableDefaultCaptured"`
	DataLink                  NativeAPNDataLink `json:"dataLink"`
}

// CheckNativeAPNNetwork is read-only except its short-lived owner reply socket.
// It never starts a data call, changes a route, or returns individual addresses.
func CheckNativeAPNNetwork(ctx context.Context) (NativeAPNNetworkCheck, error) {
	var out NativeAPNNetworkCheck
	before, e := captureNativeAPNNetwork(ctx)
	if e != nil {
		return out, e
	}
	out.Ready = true
	out.ManagementWiFiCompleted = before.wifi.state == "COMPLETED"
	out.RestorableDefaultCaptured = len(before.defaultRoute) > 0
	out.DataLink, e = nativeAPNDataLink(ctx)
	return out, e
}

type nativeAPNWiFiState struct{ state, ssid, id, address string }
type nativeAPNNetworkBaseline struct {
	wifi         nativeAPNWiFiState
	prefix       string
	defaultRoute []string
	stock        string
}

type NativeAPNDataLink struct {
	Status                string   `json:"status"`
	ProcessRunning        *bool    `json:"processRunning"`
	InterfacesUp          []string `json:"interfacesUp"`
	InterfacesWithIPv4    []string `json:"interfacesWithIPv4"`
	DefaultInterfaces     []string `json:"defaultInterfaces"`
	LinuxEgressConfigured *bool    `json:"linuxEgressConfigured"`
}

func parseNativeAPNWiFi(raw string) (nativeAPNWiFiState, error) {
	if len(raw) > 32768 {
		return nativeAPNWiFiState{}, errors.New("APN_WIFI_STATUS_OVERSIZE")
	}
	values := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		pair := strings.SplitN(line, "=", 2)
		if len(pair) != 2 {
			continue
		}
		if _, exists := values[pair[0]]; exists {
			return nativeAPNWiFiState{}, errors.New("APN_WIFI_STATUS_DUPLICATE")
		}
		values[pair[0]] = pair[1]
	}
	state := nativeAPNWiFiState{values["wpa_state"], values["ssid"], values["id"], values["ip_address"]}
	if state.state != "COMPLETED" {
		return state, nil
	}
	id, e := strconv.ParseUint(state.id, 10, 20)
	if e != nil || id > 999999 || state.ssid == "" {
		return nativeAPNWiFiState{}, errors.New("APN_WIFI_STATUS_INVALID")
	}
	address, e := netip.ParseAddr(state.address)
	if e != nil || !address.Is4() || !address.IsGlobalUnicast() {
		return nativeAPNWiFiState{}, errors.New("APN_WIFI_ADDRESS_INVALID")
	}
	return state, nil
}
func nativeAPNWiFiUnchanged(before, after nativeAPNWiFiState) bool {
	return before.state == "COMPLETED" && before == after
}

// Only the route grammar actually captured from this stock device is accepted.
// Returned tokens are never supplied by a Web caller.
func parseNativeAPNDefault(raw, expectedAddress string) ([]string, error) {
	lines := strings.FieldsFunc(strings.TrimSpace(raw), func(r rune) bool { return r == '\n' || r == '\r' })
	if len(lines) == 0 {
		return nil, nil
	}
	if len(lines) != 1 {
		return nil, errors.New("APN_DEFAULT_ROUTE_NOT_UNIQUE")
	}
	words := strings.Fields(lines[0])
	if len(words) < 5 || words[0] != "default" || words[1] != "via" || words[3] != "dev" || words[4] != "wlan0" {
		return nil, errors.New("APN_DEFAULT_ROUTE_UNSUPPORTED")
	}
	gateway, e := netip.ParseAddr(words[2])
	if e != nil || !gateway.Is4() || !gateway.IsGlobalUnicast() {
		return nil, errors.New("APN_DEFAULT_GATEWAY_INVALID")
	}
	seen := map[string]bool{}
	for i := 5; i < len(words); i += 2 {
		if i+1 >= len(words) || seen[words[i]] {
			return nil, errors.New("APN_DEFAULT_ROUTE_OPTION_INVALID")
		}
		seen[words[i]] = true
		switch words[i] {
		case "proto":
			if words[i+1] != "dhcp" && words[i+1] != "static" && words[i+1] != "boot" {
				return nil, errors.New("APN_DEFAULT_ROUTE_PROTOCOL_UNSUPPORTED")
			}
		case "metric":
			if _, e := strconv.ParseUint(words[i+1], 10, 32); e != nil {
				return nil, errors.New("APN_DEFAULT_ROUTE_METRIC_INVALID")
			}
		case "src":
			if words[i+1] != expectedAddress {
				return nil, errors.New("APN_DEFAULT_ROUTE_SOURCE_CHANGED")
			}
		default:
			return nil, errors.New("APN_DEFAULT_ROUTE_OPTION_UNSUPPORTED")
		}
	}
	return words, nil
}
func nativeAPNAddressPrefix(raw string) (string, error) {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) != 1 {
		return "", errors.New("APN_WIFI_ADDRESS_NOT_UNIQUE")
	}
	words := strings.Fields(lines[0])
	if len(words) < 4 || words[1] != "wlan0" || words[2] != "inet" {
		return "", errors.New("APN_WIFI_ADDRESS_INVALID")
	}
	prefix, e := netip.ParsePrefix(words[3])
	if e != nil || !prefix.Addr().Is4() || !prefix.Addr().IsGlobalUnicast() {
		return "", errors.New("APN_WIFI_ADDRESS_INVALID")
	}
	return prefix.String(), nil
}

func nativeAPNHasUsableIPv4(raw, name string) (bool, error) {
	if strings.TrimSpace(raw) == "" {
		return false, nil
	}
	usable := false
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		words := strings.Fields(line)
		if len(words) < 4 || strings.SplitN(words[1], "@", 2)[0] != name || words[2] != "inet" {
			return false, errors.New("APN_INTERFACE_ADDRESS_INVALID")
		}
		prefix, e := netip.ParsePrefix(words[3])
		if e != nil || !prefix.Addr().Is4() {
			return false, errors.New("APN_INTERFACE_ADDRESS_INVALID")
		}
		if prefix.Addr().IsGlobalUnicast() {
			usable = true
		}
	}
	return usable, nil
}
