package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/np1-control-center/ncc/internal/config"
	"github.com/np1-control-center/ncc/internal/model"
)

const APDirectory = "/overlay/owner-apps/np1-control/private/ap"
const APRuntimeDirectory = "/run/np1-control-ap"
const apInterface = "nccap0"
const apAddress = "192.168.226.1/24"
const apUnit = "np1-control-ap.service"

type APSettings struct {
	Revision             int    `json:"revision"`
	Enabled              bool   `json:"enabled"`
	SSID                 string `json:"ssid"`
	PassphraseConfigured bool   `json:"passphraseConfigured"`
	StandaloneFallback   bool   `json:"standaloneFallback"`
}
type APChange struct {
	Enabled    bool   `json:"enabled"`
	SSID       string `json:"ssid"`
	Passphrase string `json:"passphrase,omitempty"`
}
type apFailure struct {
	Code  string    `json:"code"`
	Stage string    `json:"stage"`
	At    time.Time `json:"at"`
}
type apConfiguration struct {
	Revision   int    `json:"revision"`
	Enabled    bool   `json:"enabled"`
	SSID       string `json:"ssid"`
	Passphrase string `json:"passphrase"`
	// Standalone startup defaults on after the controlled no-STA field trial.
	// Explicit false remains an operator-only diagnostic override.
	StandaloneFallback bool `json:"standaloneFallback"`
}
type APWorkerState struct {
	Sharing          APSharingState `json:"sharing"`
	Revision         int            `json:"revision"`
	State            string         `json:"state"`
	Enabled          bool           `json:"enabled"`
	DHCPReady        bool           `json:"dhcpReady"`
	Channel          int            `json:"channel,omitempty"`
	ChannelSource    string         `json:"channelSource,omitempty"`
	Address          string         `json:"address,omitempty"`
	ClientCount      *int           `json:"clientCount"`
	Clients          []APClient     `json:"clients"`
	ClientsAvailable bool           `json:"clientsAvailable"`
	UpdatedAt        time.Time      `json:"updatedAt"`
	ErrorCode        string         `json:"errorCode,omitempty"`
}

var apApplyMutex sync.Mutex

func apPublic(c apConfiguration) APSettings {
	return APSettings{c.Revision, c.Enabled, c.SSID, c.Passphrase != "", c.StandaloneFallback}
}
func apLoadConfig(path string) (apConfiguration, error) {
	c := apConfiguration{SSID: "NP1-LAN", StandaloneFallback: true}
	st, e := os.Lstat(path)
	if os.IsNotExist(e) {
		return c, nil
	}
	if e != nil {
		return c, e
	}
	if !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 || st.Size() > 4096 || (runtime.GOOS == "linux" && st.Mode().Perm()&0077 != 0) {
		return c, errors.New("AP_PRIVATE_CONFIG_INVALID")
	}
	f, e := os.Open(path)
	if e != nil {
		return c, e
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, 4097))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&c); e != nil {
		return c, errors.New("AP_PRIVATE_CONFIG_INVALID")
	}
	if e = apValidate(c.SSID, c.Passphrase, c.Enabled); e != nil {
		return c, e
	}
	return c, nil
}
func apValidate(ssid, passphrase string, enabled bool) error {
	if !utf8.ValidString(ssid) || len(ssid) < 1 || len(ssid) > 32 {
		return errors.New("SSID must contain 1 to 32 UTF-8 bytes")
	}
	for _, r := range ssid {
		if unicode.IsControl(r) {
			return errors.New("SSID must not contain control characters")
		}
	}
	if passphrase == "" && !enabled {
		return nil
	}
	if len(passphrase) < 8 || len(passphrase) > 63 {
		return errors.New("AP passphrase must contain 8 to 63 printable ASCII characters")
	}
	for _, b := range []byte(passphrase) {
		if b < 32 || b > 126 {
			return errors.New("AP passphrase must contain printable ASCII characters")
		}
	}
	return nil
}
func APStatus() model.Observation {
	c, e := apLoadConfig(filepath.Join(APDirectory, "config.json"))
	if e != nil {
		return model.Missing("NCC management AP", "AP設定を読み取れません")
	}
	var state APWorkerState
	data := map[string]any{"settings": apPublic(c), "ssid": c.SSID, "interface": apInterface, "address": apAddress, "enabled": nil, "clientCount": nil, "clients": nil, "clientsAvailable": false, "dhcpReady": nil, "mode": "hotspot-auto-uplink", "internetForwarding": false, "sharing": APSharingState{Enabled: true, State: "unknown"}}

	var failure apFailure
	if b, e := os.ReadFile(filepath.Join(apPrivate(APRuntimeDirectory), "failure.json")); e == nil && len(b) <= 2048 && json.Unmarshal(b, &failure) == nil && strings.HasPrefix(failure.Code, "AP_") {
		data["lastFailure"] = failure
	}
	if b, e := os.ReadFile(filepath.Join(APRuntimeDirectory, "status.json")); e == nil && len(b) <= 65536 && json.Unmarshal(b, &state) == nil && state.Revision == c.Revision {
		workerStatus := state
		workerStatus.Clients = nil // Publish client details once, in data.clients.
		data["worker"] = workerStatus
		if time.Since(state.UpdatedAt) >= 0 && time.Since(state.UpdatedAt) < 7*time.Second {
			data["enabled"] = state.Enabled
			data["clientCount"] = state.ClientCount
			data["clients"] = state.Clients
			data["clientsAvailable"] = state.ClientsAvailable
			data["dhcpReady"] = state.DHCPReady
			data["channel"] = state.Channel
			data["channelSource"] = state.ChannelSource
			data["sharing"] = state.Sharing
			data["internetForwarding"] = state.Enabled && state.Sharing.Ready
		}
	} else if !c.Enabled {
		if _, e := os.Stat("/sys/class/net/" + apInterface); os.IsNotExist(e) {
			data["enabled"] = false
			data["dhcpReady"] = false
		}
	}
	o := model.Value("NCC AP hostapd STATUS + owned DHCP socket", data)
	o.RefreshSeconds = 2
	return o
}
func apService(action string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "systemctl", action, apUnit).Run()
}
func apMarker(enabled bool) error {
	path := filepath.Join(APDirectory, "autostart")
	if enabled {
		return os.WriteFile(path, []byte("enabled\n"), 0600)
	}
	e := os.Remove(path)
	if os.IsNotExist(e) {
		return nil
	}
	return e
}
func ApplyAP(expected int, next APChange) (model.Observation, error) {
	apApplyMutex.Lock()
	defer apApplyMutex.Unlock()
	previous, e := apLoadConfig(filepath.Join(APDirectory, "config.json"))
	if e != nil {
		return model.Observation{}, e
	}
	if previous.Revision != expected {
		return model.Observation{}, errors.New("REVISION_CONFLICT")
	}
	current := previous
	current.Revision = expected + 1
	current.Enabled = next.Enabled
	current.SSID = next.SSID
	if next.Passphrase != "" {
		current.Passphrase = next.Passphrase
	}
	next.Passphrase = ""
	if e = apValidate(current.SSID, current.Passphrase, current.Enabled); e != nil {
		return model.Observation{}, e
	}
	if e = os.MkdirAll(APDirectory, 0700); e != nil {
		return model.Observation{}, errors.New("AP_CONFIG_WRITE_FAILED")
	}
	if e = config.Atomic(filepath.Join(APDirectory, "config.json"), current); e != nil {
		return model.Observation{}, errors.New("AP_CONFIG_WRITE_FAILED")
	}
	if e = apMarker(current.Enabled); e == nil {
		action := "stop"
		if current.Enabled {
			action = "restart"
		}
		e = apService(action)
	}
	if e == nil {
		until := time.Now().Add(12 * time.Second)
		for time.Now().Before(until) {
			var state APWorkerState
			b, _ := os.ReadFile(filepath.Join(APRuntimeDirectory, "status.json"))
			_ = json.Unmarshal(b, &state)
			if !current.Enabled {
				if _, statErr := os.Stat("/sys/class/net/" + apInterface); os.IsNotExist(statErr) {
					return APStatus(), nil
				}
			}
			if current.Enabled && state.Revision == current.Revision && state.Enabled && state.DHCPReady && time.Since(state.UpdatedAt) < 7*time.Second {
				return APStatus(), nil
			}
			if state.Revision == current.Revision && state.ErrorCode != "" {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	_ = apService("stop")
	restoreErr := config.Atomic(filepath.Join(APDirectory, "config.json"), previous)
	if markerErr := apMarker(previous.Enabled); restoreErr == nil {
		restoreErr = markerErr
	}
	if previous.Enabled {
		if startErr := apService("start"); restoreErr == nil {
			restoreErr = startErr
		}
	}
	if restoreErr != nil {
		return model.Observation{}, errors.New("AP_APPLY_FAILED_ROLLBACK_INCOMPLETE")
	}
	return model.Observation{}, errors.New("AP_APPLY_NOT_CONFIRMED_PREVIOUS_CONFIG_RESTORED")
}

func apSafeError(e error) string {
	if e == nil {
		return ""
	}
	if errors.Is(e, context.Canceled) {
		return "AP_OPERATION_CANCELLED"
	}
	if errors.Is(e, context.DeadlineExceeded) {
		return "AP_OPERATION_TIMED_OUT"
	}
	s := e.Error()
	if strings.HasPrefix(s, "AP_") {
		return s
	}
	return "AP_OPERATION_FAILED"
}
