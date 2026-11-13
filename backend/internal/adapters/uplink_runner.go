package adapters

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/np1-control-center/ncc/internal/config"
)

// Paths of the uplink supervisor (unit np1-control-uplink).
const (
	UplinkRuntime   = "/run/np1-control-uplink"
	uplinkHelper    = UplinkRuntime + "/helper"
	UplinkStatus    = UplinkRuntime + "/status.json"
	uplinkPrivate   = "/overlay/owner-apps/np1-control/private/uplink"
	uplinkStateFile = uplinkPrivate + "/state.json"
	// UplinkSettings is written by the broker for the owner's on/off choice.
	UplinkSettings  = uplinkPrivate + "/settings.json"
	stockSettingMap = "/tmp/victoria/victoria_PMEM_AL_SRV_SET_SETTING_ITEM.map"
)

type UplinkStatusView struct {
	At         time.Time   `json:"at"`
	Mode       string      `json:"mode"`
	STAOn      *bool       `json:"staOn"`
	WPAState   string      `json:"wpaState,omitempty"`
	Uplink     string      `json:"uplink"` // wifi | cellular | none
	State      UplinkState `json:"state"`
	ScanKnown  bool        `json:"scanKnown"`
	SavedSeen  bool        `json:"savedSeen"`
	Error      string      `json:"error,omitempty"`
	HelperSeen bool        `json:"helperAvailable"`
}

// stockSTAOn reads stock setting 40 (0 = STA mode on, 1 = off) from the shared settings
// map, with the same layout checks as the field trial.
func stockSTAOn() (bool, error) {
	raw, e := os.ReadFile(stockSettingMap)
	if e != nil {
		return false, e
	}
	at := func(offset int) uint32 { return binary.LittleEndian.Uint32(raw[offset:]) }
	if len(raw) != 7296 || at(0) != 7168 || at(4) != 0 || at(128) != 1 || at(132) != 1 || at(140) != 0 || at(148) != 44 || at(152) != 86 {
		return false, errors.New("UPLINK_STOCK_LAYOUT")
	}
	switch at(128 + 44 + 4*40) {
	case 0:
		return true, nil
	case 1:
		return false, nil
	}
	return false, errors.New("UPLINK_STOCK_VALUE")
}

func communicationPID() (string, error) {
	entries, e := os.ReadDir("/proc")
	if e != nil {
		return "", e
	}
	found := ""
	for _, entry := range entries {
		if _, e := strconv.Atoi(entry.Name()); e != nil {
			continue
		}
		if exe, e := os.Readlink(filepath.Join("/proc", entry.Name(), "exe")); e == nil && exe == "/usr/local/bin/Communication" {
			if found != "" {
				return "", errors.New("UPLINK_OWNER_AMBIGUOUS")
			}
			found = entry.Name()
		}
	}
	if found == "" {
		return "", errors.New("UPLINK_OWNER_MISSING")
	}
	return found, nil
}

var helperWire = regexp.MustCompile(`(?m)^wifi-sta-wire=([12])$`)

// requestSTA sends the verified owner request (1 = ON, 2 = OFF) and then confirms the
// result from setting 40, which is the state Communication itself acts on. The helper's
// exit status is only advisory: its strict shared-memory reference check can fail when
// another stock process maps the same region at that moment, even though the request
// completed (seen in the 2026-11-13 trial).
func requestSTA(on bool) error {
	pid, e := communicationPID()
	if e != nil {
		return e
	}
	expected, next := "2", "1"
	if !on {
		expected, next = "1", "2"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, uplinkHelper, "--apply", pid, expected, next)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LD_LIBRARY_PATH=/usr/local/lib", "NCC_NATIVE_WIFI_STA_TRIAL=1"}
	out, runErr := cmd.Output()
	if m := helperWire.FindSubmatch(out); runErr != nil && m == nil {
		log.Printf("uplink helper: %v", runErr)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if state, e := stockSTAOn(); e == nil && state == on {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("UPLINK_REQUEST_UNCONFIRMED")
}

func savedSSIDs() []string {
	raw, e := vendorWPA("LIST_NETWORKS")
	if e != nil {
		return nil
	}
	networks, e := parseWiFiNetworks(raw)
	if e != nil {
		return nil
	}
	out := []string{}
	for _, n := range networks {
		if n.SSID != "" {
			out = append(out, n.SSID)
		}
	}
	return out
}

// scanForSaved asks the supplicant (if it still runs with the mode off) for a scan and
// reports whether a saved SSID is heard at a usable level.
func scanForSaved(saved []string) (known, seen bool) {
	if len(saved) == 0 {
		return false, false
	}
	if reply, e := vendorWPA("SCAN"); e != nil || !strings.HasPrefix(strings.TrimSpace(reply), "OK") && !strings.Contains(reply, "FAIL-BUSY") {
		return false, false
	}
	time.Sleep(5 * time.Second)
	raw, e := vendorWPA("SCAN_RESULTS")
	if e != nil {
		return false, false
	}
	want := map[string]bool{}
	for _, s := range saved {
		want[s] = true
	}
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n")[1:] {
		f := strings.Split(line, "\t")
		if len(f) < 5 {
			continue
		}
		level, _ := strconv.Atoi(f[2])
		if ssid, e := decodeWPASSID(f[4]); e == nil && want[ssid] && level >= -80 {
			return true, true
		}
	}
	return true, false
}

// Uplink modes chosen by the owner on the Wi-Fi/AP page (written by the broker).
const (
	UplinkModeAuto     = "auto"     // switch to 4G when out of Wi-Fi range, back near saved Wi-Fi
	UplinkModeWiFi     = "wifi"     // keep the stock STA mode on
	UplinkModeCellular = "cellular" // keep the stock STA mode off (4G)
)

type UplinkOwnerSettings struct {
	Mode string `json:"mode"`
}

func ValidUplinkMode(mode string) bool {
	return mode == UplinkModeAuto || mode == UplinkModeWiFi || mode == UplinkModeCellular
}
func uplinkMode() string {
	var v UplinkOwnerSettings
	if raw, e := os.ReadFile(UplinkSettings); e == nil && json.Unmarshal(raw, &v) == nil && ValidUplinkMode(v.Mode) {
		return v.Mode
	}
	return UplinkModeAuto
}

func currentUplink() string {
	switch defaultIface() {
	case "wlan0":
		return "wifi"
	case "":
		return "none"
	}
	if strings.HasPrefix(defaultIface(), "usb0") || strings.HasPrefix(defaultIface(), "rmnet") {
		return "cellular"
	}
	return "other"
}

// RunUplinkSupervisor ticks every 10 seconds until ctx ends.
func RunUplinkSupervisor(ctx context.Context) error {
	if e := os.MkdirAll(uplinkPrivate, 0700); e != nil {
		return e
	}
	var state UplinkState
	if raw, e := os.ReadFile(uplinkStateFile); e == nil {
		_ = json.Unmarshal(raw, &state)
	}
	var saved []string
	if raw, e := os.ReadFile(filepath.Join(uplinkPrivate, "saved.json")); e == nil {
		_ = json.Unmarshal(raw, &saved)
	}
	previousMode := ""
	var manualRetry time.Time
	for {
		mode := uplinkMode()
		status := UplinkStatusView{At: time.Now().UTC(), Mode: mode}
		_, helperErr := os.Stat(uplinkHelper)
		status.HelperSeen = helperErr == nil
		on, e := stockSTAOn()
		if e != nil {
			status.Error = e.Error()
		} else {
			status.STAOn = &on
			now := time.Now()
			if mode != previousMode {
				manualRetry = time.Time{}
				// Entering automatic mode with the STA mode off (for example after manual 4G)
				// lets the automation bring Wi-Fi back near a saved network.
				if mode == UplinkModeAuto && previousMode != "" && !on && state.AutoOffAt == nil {
					t := now
					state.AutoOffAt = &t
				}
				previousMode = mode
			}
			if on {
				status.WPAState = wpaState()
				if list := savedSSIDs(); len(list) > 0 {
					saved = list
					_ = config.Atomic(filepath.Join(uplinkPrivate, "saved.json"), saved)
				}
			}
			action, reason, next := UplinkNone, "", state
			switch mode {
			case UplinkModeWiFi, UplinkModeCellular:
				// Manual: hold the chosen mode; automatic markers do not apply.
				want := mode == UplinkModeWiFi
				next.AutoOffAt, next.UnconnectedSince, next.RetryAfter = nil, nil, nil
				if want != on && status.HelperSeen && now.After(manualRetry) {
					action, reason = UplinkOff, "owner-manual"
					if want {
						action = UplinkOn
					}
				}
			default:
				in := UplinkInputs{Now: now, STAOn: on, WPAState: status.WPAState, Working: on && currentUplink() == "wifi", Enabled: status.HelperSeen}
				if fix, e := liveGNSS.LatestWithin(ctx, now, uplinkFixMaxAge); e == nil && fix != nil {
					in.Position = &UplinkPosition{At: fix.At, Lat: fix.Lat, Lon: fix.Lon}
				}
				if !on && state.AutoOffAt != nil {
					in.ScanKnown, in.SavedSeen = scanForSaved(saved)
					status.ScanKnown, status.SavedSeen = in.ScanKnown, in.SavedSeen
				}
				action, reason, next = DecideUplink(in, state)
			}
			if action != UplinkNone {
				if e := requestSTA(action == UplinkOn); e != nil {
					status.Error = e.Error()
					// Keep the previous state so the decision is retried later.
					next = state
					manualRetry = now.Add(time.Minute)
				} else {
					next.Switches++
					next.LastAction, next.LastReason = string(action), reason
					log.Printf("uplink: STA %s (%s)", action, reason)
				}
			}
			state = next
			_ = config.Atomic(uplinkStateFile, state)
		}
		status.State = state
		status.Uplink = currentUplink()
		_ = config.Atomic(UplinkStatus, status)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(10 * time.Second):
		}
	}
}

// UplinkView is what the Wi-Fi/AP page shows: the owner's mode and the supervisor's latest
// round (null when older than a minute, never a remembered value).
func UplinkView() map[string]any {
	view := map[string]any{"mode": uplinkMode(), "status": nil}
	if raw, e := os.ReadFile(UplinkStatus); e == nil {
		var s UplinkStatusView
		if json.Unmarshal(raw, &s) == nil && time.Since(s.At) < time.Minute {
			view["status"] = s
		}
	}
	return view
}

// HandleUplink serves GET/PUT /network/uplink on the root broker.
func HandleUplink(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		recordingJSON(w, 200, UplinkView())
		return
	}
	var body UplinkOwnerSettings
	if !wifiDecode(w, r, &body) {
		return
	}
	if !ValidUplinkMode(body.Mode) {
		nativeError(w, 400, "INVALID_UPLINK_MODE", "回線の切替方法を選んでください")
		return
	}
	if e := config.Atomic(UplinkSettings, body); e != nil {
		nativeError(w, 503, "UPLINK_SETTINGS_UNAVAILABLE", "設定を保存できません")
		return
	}
	recordingJSON(w, 200, UplinkView())
}
