//go:build linux

package modem

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type nativeAPNBoundedOutput struct{ bytes.Buffer }

func (b *nativeAPNBoundedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 65536 {
		return 0, errors.New("APN_NETWORK_OUTPUT_OVERSIZE")
	}
	return b.Buffer.Write(p)
}
func nativeAPNCommand(parent context.Context, program string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, program, args...)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
	var output nativeAPNBoundedOutput
	command.Stdout = &output
	command.Stderr = io.Discard
	if command.Run() != nil {
		return "", errors.New("APN_FIXED_NETWORK_COMMAND_FAILED")
	}
	return strings.TrimSpace(output.String()), nil
}
func nativeAPNWiFiStatus(ctx context.Context) (nativeAPNWiFiState, error) {
	nonce := make([]byte, 8)
	if _, e := rand.Read(nonce); e != nil {
		return nativeAPNWiFiState{}, errors.New("APN_WIFI_SOCKET_FAILED")
	}
	local := "/run/np1-control/wpa-apn-" + hex.EncodeToString(nonce)
	channel, e := net.DialUnix("unixgram", &net.UnixAddr{Name: local, Net: "unixgram"}, &net.UnixAddr{Name: "/run/wifi/wpa_supplicant/wlan0", Net: "unixgram"})
	if e != nil {
		return nativeAPNWiFiState{}, errors.New("APN_WIFI_STATUS_UNAVAILABLE")
	}
	defer channel.Close()
	defer os.Remove(local)
	group, e := user.LookupGroup("NEventWatcherCom")
	if e != nil {
		return nativeAPNWiFiState{}, errors.New("APN_WIFI_REPLY_GROUP_UNAVAILABLE")
	}
	gid, e := strconv.Atoi(group.Gid)
	if e != nil {
		return nativeAPNWiFiState{}, errors.New("APN_WIFI_REPLY_GROUP_INVALID")
	}
	if os.Chown(local, 0, gid) != nil || os.Chmod(local, 0660) != nil {
		return nativeAPNWiFiState{}, errors.New("APN_WIFI_REPLY_PERMISSION_FAILED")
	}
	deadline := time.Now().Add(2 * time.Second)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	_ = channel.SetDeadline(deadline)
	if _, e = channel.Write([]byte("STATUS")); e != nil {
		return nativeAPNWiFiState{}, errors.New("APN_WIFI_STATUS_FAILED")
	}
	raw := make([]byte, 32769)
	n, e := channel.Read(raw)
	if e != nil {
		return nativeAPNWiFiState{}, errors.New("APN_WIFI_STATUS_FAILED")
	}
	return parseNativeAPNWiFi(string(raw[:n]))
}
func nativeAPNStockSnapshot(ctx context.Context) (string, error) {
	var snapshot strings.Builder
	for _, unit := range []string{"app-@Communication", "app-@DriveRec", "app-@ImageRecog", "app-@Voice", "qmmf-server", "NEventWatcherCom", "app-@Setting"} {
		raw, e := nativeAPNCommand(ctx, "/bin/systemctl", "show", unit+".service", "-p", "Id", "-p", "MainPID", "-p", "InvocationID", "-p", "ActiveState", "-p", "SubState")
		if e != nil {
			return "", e
		}
		if !nativeAPNStockStateSafe(unit, raw) {
			return "", errors.New("APN_STOCK_SERVICE_NOT_READY")
		}
		snapshot.WriteString(raw)
		snapshot.WriteByte('\n')
	}
	return snapshot.String(), nil
}
func captureNativeAPNNetwork(ctx context.Context) (nativeAPNNetworkBaseline, error) {
	var out nativeAPNNetworkBaseline
	var e error
	out.stock, e = nativeAPNStockSnapshot(ctx)
	if e != nil {
		return out, e
	}
	out.wifi, e = nativeAPNWiFiStatus(ctx)
	wifiError := e
	hotspot, hotspotError := captureNativeAPNHotspot(ctx)
	if hotspotError == nil {
		out.hotspot = &hotspot
	}
	if wifiError != nil {
		if wifiError.Error() != "APN_WIFI_STATUS_UNAVAILABLE" || out.hotspot == nil {
			return out, wifiError
		}
		addresses, addressError := nativeAPNCommand(ctx, "/sbin/ip", "-o", "-4", "address", "show")
		routes, routeError := nativeAPNCommand(ctx, "/sbin/ip", "-4", "route", "show", "table", "main", "default")
		if addressError != nil || routeError != nil || !nativeAPNUnobservedWiFiInactive(addresses, routes) {
			return out, errors.New("APN_UNOBSERVED_WIFI_MAY_BE_ACTIVE")
		}
		out.wifi.state = "UNAVAILABLE"
		return out, nil
	}
	if out.wifi.state != "COMPLETED" {
		if out.hotspot == nil {
			return out, errors.New("APN_MANAGEMENT_PATH_UNVERIFIED")
		}
		return out, nil
	}
	address, e := nativeAPNCommand(ctx, "/sbin/ip", "-o", "-4", "address", "show", "dev", "wlan0")
	if e != nil {
		return out, e
	}
	out.prefix, e = nativeAPNAddressPrefix(address)
	if e != nil {
		return out, e
	}
	if !strings.HasPrefix(out.prefix, out.wifi.address+"/") {
		return out, errors.New("APN_WIFI_IPV4_EVIDENCE_MISMATCH")
	}
	routes, e := nativeAPNCommand(ctx, "/sbin/ip", "-4", "route", "show", "table", "main", "default")
	if e != nil {
		return out, e
	}
	// A preexisting non-WiFi default is preserved. No route restoration will be
	// attempted unless a single original wlan0 default was positively captured.
	if routes != "" && !strings.Contains(routes, "dev wlan0") {
		return out, nil
	}
	out.defaultRoute, e = parseNativeAPNDefault(routes, out.wifi.address)
	return out, e
}
func restoreNativeAPNDefault(ctx context.Context, before nativeAPNNetworkBaseline) (string, error) {
	stock, e := nativeAPNStockSnapshot(ctx)
	if e != nil || stock != before.stock {
		return "blocked", errors.New("APN_STOCK_SERVICE_CHANGED")
	}
	if before.hotspot != nil {
		current, hotspotError := captureNativeAPNHotspot(ctx)
		if hotspotError != nil || current != *before.hotspot {
			return "blocked", errors.New("APN_HOTSPOT_MANAGEMENT_CHANGED")
		}
	}
	if len(before.defaultRoute) == 0 {
		return "not-needed", nil
	}
	wifi, e := nativeAPNWiFiStatus(ctx)
	if e != nil || !nativeAPNWiFiUnchanged(before.wifi, wifi) {
		return "blocked", errors.New("APN_WIFI_PROFILE_CHANGED")
	}
	address, e := nativeAPNCommand(ctx, "/sbin/ip", "-o", "-4", "address", "show", "dev", "wlan0")
	if e != nil {
		return "blocked", e
	}
	prefix, e := nativeAPNAddressPrefix(address)
	if e != nil || prefix != before.prefix {
		return "blocked", errors.New("APN_WIFI_ADDRESS_CHANGED")
	}
	raw, e := nativeAPNCommand(ctx, "/sbin/ip", "-4", "route", "show", "table", "main", "default")
	if e != nil {
		return "blocked", e
	}
	expected := strings.Join(before.defaultRoute, " ")
	if strings.Join(strings.Fields(raw), " ") == expected {
		return "preserved", nil
	}
	if raw != "" {
		return "blocked", errors.New("APN_COMPETING_DEFAULT_ROUTE")
	}
	args := append([]string{"-4", "route", "add"}, before.defaultRoute...)
	if _, e = nativeAPNCommand(ctx, "/sbin/ip", args...); e != nil {
		return "failed", e
	}
	raw, e = nativeAPNCommand(ctx, "/sbin/ip", "-4", "route", "show", "table", "main", "default")
	if e != nil || strings.Join(strings.Fields(raw), " ") != expected {
		return "failed", errors.New("APN_DEFAULT_ROUTE_READBACK_FAILED")
	}
	wifi, e = nativeAPNWiFiStatus(ctx)
	if e != nil || !nativeAPNWiFiUnchanged(before.wifi, wifi) {
		return "failed", errors.New("APN_WIFI_RECOVERY_READBACK_FAILED")
	}
	stock, e = nativeAPNStockSnapshot(ctx)
	if e != nil || stock != before.stock {
		return "failed", errors.New("APN_STOCK_SERVICE_CHANGED")
	}
	return "restored", nil
}

func settleNativeAPNDefault(ctx context.Context, before nativeAPNNetworkBaseline) (string, error) {
	until := time.Now().Add(10 * time.Second)
	result := "preserved"
	for {
		status, e := restoreNativeAPNDefault(ctx, before)
		if status == "restored" || result != "restored" {
			result = status
		}
		if e != nil {
			return result, e
		}
		if !time.Now().Before(until) {
			return result, nil
		}
		select {
		case <-ctx.Done():
			return result, errors.New("APN_MANAGEMENT_SETTLE_INTERRUPTED")
		case <-time.After(time.Second):
		}
	}
}
func nativeAPNDataLink(ctx context.Context) (NativeAPNDataLink, error) {
	out := NativeAPNDataLink{Status: "unknown", InterfacesUp: []string{}, InterfacesWithIPv4: []string{}, DefaultInterfaces: []string{}}
	if e := verifyGobiNet(); e != nil {
		return out, errors.New("APN_MODEM_INTERFACE_UNVERIFIED")
	}
	physical, e := filepath.EvalSymlinks("/sys/class/net/usb0/device")
	if e != nil {
		return out, errors.New("APN_PHYSICAL_INTERFACE_UNVERIFIED")
	}
	driver, e := filepath.EvalSymlinks(filepath.Join(physical, "driver"))
	if e != nil || filepath.Base(driver) != "GobiNet" {
		return out, errors.New("APN_PHYSICAL_DRIVER_UNVERIFIED")
	}
	matchedUSB := false
	for ancestor := physical; ancestor != "/"; ancestor = filepath.Dir(ancestor) {
		vendor, readError := os.ReadFile(filepath.Join(ancestor, "idVendor"))
		if readError != nil {
			continue
		}
		product, readError := os.ReadFile(filepath.Join(ancestor, "idProduct"))
		if readError == nil && strings.TrimSpace(string(vendor)) == "2c7c" && strings.TrimSpace(string(product)) == "0125" {
			matchedUSB = true
		}
		break
	}
	if !matchedUSB {
		return out, errors.New("APN_PHYSICAL_USB_UNVERIFIED")
	}
	parentNet, e := filepath.EvalSymlinks("/sys/class/net/usb0")
	if e != nil {
		return out, errors.New("APN_PHYSICAL_INDEX_UNAVAILABLE")
	}
	entries, e := os.ReadDir("/proc")
	if e != nil || len(entries) > 32768 {
		return out, errors.New("APN_PROCESS_INVENTORY_UNAVAILABLE")
	}
	running := false
	for _, entry := range entries {
		if _, e := strconv.Atoi(entry.Name()); e != nil {
			continue
		}
		target, e := os.Readlink(filepath.Join("/proc", entry.Name(), "exe"))
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return out, errors.New("APN_PROCESS_INVENTORY_INCOMPLETE")
		}
		if target == "/usr/bin/quectel-CM" {
			running = true
		}
	}
	out.ProcessRunning = &running
	routes, e := nativeAPNCommand(ctx, "/sbin/ip", "-4", "route", "show", "table", "main", "default")
	if e != nil {
		return out, e
	}
	for _, name := range []string{"usb0", "usb0.1", "usb0.2"} {
		state, e := os.ReadFile("/sys/class/net/" + name + "/operstate")
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return out, errors.New("APN_INTERFACE_STATE_UNAVAILABLE")
		}
		if name != "usb0" {
			// The pinned GobiNet QMAP children use their own iflink, unlike
			// VLANs. Their sysfs device points to the verified parent netdev.
			link, linkError := filepath.EvalSymlinks("/sys/class/net/" + name + "/device")
			if linkError != nil || link != parentNet {
				return out, errors.New("APN_CHILD_INTERFACE_UNVERIFIED")
			}
		}
		if strings.TrimSpace(string(state)) == "up" {
			out.InterfacesUp = append(out.InterfacesUp, name)
		}
		raw, e := nativeAPNCommand(ctx, "/sbin/ip", "-o", "-4", "address", "show", "dev", name)
		if e != nil {
			return out, e
		}
		usable, addressError := nativeAPNHasUsableIPv4(raw, name)
		if addressError != nil {
			return out, addressError
		}
		if usable {
			out.InterfacesWithIPv4 = append(out.InterfacesWithIPv4, name)
		}
		for _, line := range strings.Split(routes, "\n") {
			words := strings.Fields(line)
			for i := 0; i+1 < len(words); i++ {
				if words[i] == "dev" && words[i+1] == name {
					out.DefaultInterfaces = append(out.DefaultInterfaces, name)
				}
			}
		}
	}
	configured := false
	for _, name := range out.InterfacesWithIPv4 {
		for _, route := range out.DefaultInterfaces {
			if name == route {
				configured = true
			}
		}
	}
	out.LinuxEgressConfigured = &configured
	out.Status = "available"
	return out, nil
}
