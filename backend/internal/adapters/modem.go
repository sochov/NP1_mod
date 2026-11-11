package adapters

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/model"
	"github.com/np1-control-center/ncc/internal/modem"
)

type ModemObserver struct {
	mu     sync.Mutex
	latest model.Observation
}

func NewModemObserver(ctx context.Context) *ModemObserver {
	observer := &ModemObserver{latest: model.Missing("GobiNet NAS observer", "回線の信号値を確認しています")}
	go func() {
		poll := func() {
			executable, e := os.Executable()
			if e != nil {
				return
			}
			bounded, cancel := context.WithTimeout(ctx, 12*time.Second)
			defer cancel()
			bytes, e := exec.CommandContext(bounded, executable, "--mode", "gobinet-probe").Output()
			observer.mu.Lock()
			defer observer.mu.Unlock()
			if e != nil {
				if observer.latest.Status == "available" || observer.latest.Status == "stale" {
					observer.latest.Status = "stale"
					observer.latest.Reason = "信号値の更新を確認できません"
				} else {
					observer.latest = model.Missing("GobiNet NAS observer", "安全な信号照会を完了できません")
				}
				return
			}
			var signal modem.GobiNetSignal
			if json.Unmarshal(bytes, &signal) != nil || signal.ObservedAt.IsZero() {
				observer.latest = model.Missing("GobiNet NAS observer", "信号応答を確認できません")
				return
			}
			value := model.Value(signal.Source, map[string]any{"rssi": signal.RSSI, "rsrq": signal.RSRQ, "rsrp": signal.RSRP, "snr": signal.SNR, "sinr": nil, "registered": nil, "pdpConnected": nil})
			value.ObservedAt = signal.ObservedAt
			value.RefreshSeconds = 15
			observer.latest = value
		}
		poll()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				poll()
			}
		}
	}()
	return observer
}
func (o *ModemObserver) Get() model.Observation { o.mu.Lock(); defer o.mu.Unlock(); return o.latest }
