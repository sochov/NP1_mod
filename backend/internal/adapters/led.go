package adapters

import (
	"encoding/json"
	"errors"
	"github.com/np1-control-center/ncc/internal/config"
	"github.com/np1-control-center/ncc/internal/led"
	"github.com/np1-control-center/ncc/internal/model"
	"os"
	"path/filepath"
	"time"
)

func LEDStatus() model.Observation {
	c, e := led.Load()
	if e != nil {
		return model.Missing("NCC LED worker", e.Error())
	}
	running, _ := command("systemctl", "is-active", "np1-control-led.service")
	var state *led.State
	if b, e := os.ReadFile(led.RuntimeState); e == nil {
		_ = json.Unmarshal(b, &state)
	}
	if running == "" {
		running = "unknown"
	}
	return model.Value("NCC LED worker (command state; hardware readback unsupported)", map[string]any{"settings": c, "serviceState": running, "lastCommand": state, "stopBehavior": "停止時は5ゾーン消灯。純正の元表示を復元する操作ではありません"})
}
func ledMarker(c led.Settings) error {
	p := filepath.Join(led.Directory, "autostart")
	if c.Autostart && c.Mode != "off" {
		return os.WriteFile(p, []byte("enabled\n"), 0600)
	}
	e := os.Remove(p)
	if os.IsNotExist(e) {
		return nil
	}
	return e
}
func ApplyLED(expected int, next led.Settings) (model.Observation, error) {
	if e := led.Validate(next); e != nil {
		return model.Observation{}, e
	}
	if state, _ := command("systemctl", "is-active", "np1-front-led-rainbow.service"); state == "active" {
		return model.Observation{}, errors.New("another owner LED service is active")
	}
	previous, e := led.Load()
	if e != nil {
		return model.Observation{}, e
	}
	if previous.Revision != expected {
		return model.Observation{}, errors.New("REVISION_CONFLICT")
	}
	next.Revision = expected + 1
	previousService, _ := command("systemctl", "is-active", "np1-control-led.service")
	rollback := func() {
		_, _ = command("systemctl", "stop", "np1-control-led.service")
		_ = config.Atomic(filepath.Join(led.Directory, "config.json"), previous)
		_ = ledMarker(previous)
		if previousService == "active" {
			_, _ = command("systemctl", "start", "np1-control-led.service")
		}
	}
	if e = config.Atomic(filepath.Join(led.Directory, "config.json"), next); e != nil {
		return model.Observation{}, e
	}
	e = ledMarker(next)
	if e != nil {
		rollback()
		return model.Observation{}, e
	}
	if _, e = command("systemctl", "restart", "np1-control-led.service"); e != nil {
		rollback()
		return model.Observation{}, e
	}
	until := time.Now().Add(2 * time.Second)
	for time.Now().Before(until) {
		var state led.State
		if b, e := os.ReadFile(led.RuntimeState); e == nil && json.Unmarshal(b, &state) == nil && state.Revision == next.Revision && state.Mode == next.Mode && state.Error == "" && (state.Running || next.Mode == "off") {
			return LEDStatus(), nil
		}
		time.Sleep(30 * time.Millisecond)
	}
	rollback()
	return model.Observation{}, errors.New("LED worker did not confirm command; previous settings restored")
}
