package adapters

import (
	"encoding/json"
	"errors"
	"github.com/np1-control-center/ncc/internal/config"
	"github.com/np1-control-center/ncc/internal/led"
	"github.com/np1-control-center/ncc/internal/model"
	"os"
	"path/filepath"
	"sync"
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

type ledApplyDependencies struct {
	load         func() (led.Settings, error)
	save, marker func(led.Settings) error
	serviceState func(string) (string, error)
	control      func(string) error
	readState    func() (led.State, error)
	observe      func() model.Observation
	now          func() time.Time
	sleep        func(time.Duration)
	stopScreen   func() error
}
type ledApplyError struct {
	status       int
	code, reason string
}

func (e *ledApplyError) Error() string                 { return e.reason }
func ledFailure(status int, code, reason string) error { return &ledApplyError{status, code, reason} }
func LEDErrorStatus(e error) (int, string, string) {
	var problem *ledApplyError
	if errors.As(e, &problem) {
		return problem.status, problem.code, problem.reason
	}
	return 503, "LED_UNAVAILABLE", "LED制御の状態を確認できません。状態を更新してください"
}

var ledApplyMu sync.Mutex

func ApplyLED(expected int, next led.Settings) (model.Observation, error) {
	if !ledApplyMu.TryLock() {
		return model.Observation{}, ledFailure(409, "LED_BUSY", "LEDの変更を確認中です")
	}
	defer ledApplyMu.Unlock()
	return applyLED(ledApplyDependencies{
		load: led.Load, save: func(c led.Settings) error { return config.Atomic(filepath.Join(led.Directory, "config.json"), c) }, marker: ledMarker,
		serviceState: func(unit string) (string, error) { return command("systemctl", "is-active", unit) },
		control:      func(action string) error { _, e := command("systemctl", action, "np1-control-led.service"); return e },
		readState: func() (led.State, error) {
			var state led.State
			b, e := os.ReadFile(led.RuntimeState)
			if e == nil {
				e = json.Unmarshal(b, &state)
			}
			return state, e
		},
		observe: LEDStatus, now: time.Now, sleep: time.Sleep, stopScreen: led.DefaultScreenStore.Reset,
	}, expected, next)
}
func applyLED(d ledApplyDependencies, expected int, next led.Settings) (model.Observation, error) {
	if e := led.Validate(next); e != nil {
		return model.Observation{}, ledFailure(400, "INVALID_LED_SETTINGS", "LEDの設定値を確認してください")
	}
	if state, _ := d.serviceState("np1-front-led-rainbow.service"); state == "active" {
		return model.Observation{}, ledFailure(409, "LED_IN_USE", "別のLED制御サービスが動作しています")
	}
	previous, e := d.load()
	if e != nil {
		return model.Observation{}, ledFailure(503, "LED_SETTINGS_UNAVAILABLE", "LEDの保存設定を取得できません")
	}
	if previous.Revision != expected {
		return model.Observation{}, ledFailure(409, "REVISION_CONFLICT", "別の操作でLED設定が変わりました。状態を更新してください")
	}
	if d.stopScreen != nil {
		if e := d.stopScreen(); e != nil {
			return model.Observation{}, ledFailure(503, "SCREEN_LED_STOP_FAILED", "画面連動LEDを停止できません")
		}
	}
	next.Revision = expected + 1
	previousService, _ := d.serviceState("np1-control-led.service")
	rollback := func() error {
		if e := d.save(previous); e != nil {
			return ledFailure(503, "LED_RESTORE_NOT_CONFIRMED", "LEDの保存設定を元に戻せません。状態を確認してください")
		}
		if e := d.marker(previous); e != nil {
			return ledFailure(503, "LED_RESTORE_NOT_CONFIRMED", "LEDの起動設定を元に戻せません。状態を確認してください")
		}
		return nil
	}
	if e = d.save(next); e != nil {
		return model.Observation{}, ledFailure(503, "LED_CONFIG_WRITE_FAILED", "LEDの設定を保存できません")
	}
	e = d.marker(next)
	if e != nil {
		if restoreError := rollback(); restoreError != nil {
			return model.Observation{}, restoreError
		}
		return model.Observation{}, ledFailure(503, "LED_CONFIG_WRITE_FAILED", "LEDの起動設定を保存できません")
	}
	// A configuration change is consumed by the live worker. Only a stopped or
	// failed worker is started; repeated edits never count as service starts.
	if previousService != "active" {
		if previousService == "failed" {
			e = d.control("reset-failed")
		}
		if e == nil {
			e = d.control("start")
		}
		if e != nil {
			if restoreError := rollback(); restoreError != nil {
				return model.Observation{}, restoreError
			}
			return model.Observation{}, ledFailure(503, "LED_START_FAILED", "LED制御を開始できません。状態を更新してください")
		}
	}
	until := d.now().Add(2 * time.Second)
	for d.now().Before(until) {
		state, e := d.readState()
		if e == nil && state.Revision == next.Revision && state.Mode == next.Mode && state.Error == "" && (state.Running || next.Mode == "off") {
			return d.observe(), nil
		}
		d.sleep(30 * time.Millisecond)
	}
	if restoreError := rollback(); restoreError != nil {
		return model.Observation{}, restoreError
	}
	return model.Observation{}, ledFailure(503, "LED_COMMAND_NOT_CONFIRMED", "LEDへの反映を確認できません。保存設定を元に戻しました")
}
