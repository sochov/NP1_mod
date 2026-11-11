package adapters

import (
	"context"
	"github.com/np1-control-center/ncc/internal/model"
	"github.com/np1-control-center/ncc/internal/modem"
	"sync"
	"time"
)

// ModemObserver only reads a small published status file. The broker never
// launches a root helper, inspects process descriptors, or opens a modem port.
type ModemObserver struct {
	mu     sync.Mutex
	path   string
	latest model.Observation
}

func NewModemObserver(_ context.Context) *ModemObserver {
	return &ModemObserver{path: modem.WorkerStatePath, latest: model.Missing("isolated GobiNet NAS observer", "専用の回線観測プロセスを待っています")}
}
func (o *ModemObserver) Get() model.Observation { return o.getAt(time.Now().UTC()) }
func (o *ModemObserver) getAt(now time.Time) model.Observation {
	o.mu.Lock()
	defer o.mu.Unlock()
	state, e := modem.ReadWorkerState(o.path, now)
	if e != nil {
		if o.latest.Data != nil {
			o.latest.Status = "stale"
			o.latest.Reason = "専用の回線観測プロセスから更新を確認できません"
		} else {
			o.latest = model.Missing("isolated GobiNet NAS observer", "専用の回線観測プロセスの状態を取得できません")
		}
		return o.latest
	}
	o.latest = modemWorkerObservation(state, now)
	return o.latest
}
func modemWorkerObservation(state modem.WorkerState, now time.Time) model.Observation {
	if state.Signal == nil {
		return model.Missing("isolated GobiNet NAS observer", "回線情報はまだ取得できていません")
	}
	signal := state.Signal
	value := model.Value(signal.Source, map[string]any{"rssi": signal.RSSI, "rsrq": signal.RSRQ, "rsrp": signal.RSRP, "snr": signal.SNR, "sinr": nil, "registered": signal.Registered, "psAttached": signal.PSAttached, "pdpConnected": nil, "carrier": signal.Carrier, "plmn": signal.PLMN, "band": signal.Band, "bands": signal.Bands, "rat": signal.RAT, "queries": signal.Queries})
	value.ObservedAt = signal.ObservedAt
	value.RefreshSeconds = 15
	if state.Status != "available" || now.Sub(signal.ObservedAt) > modem.WorkerFreshness || now.Sub(state.UpdatedAt) > modem.WorkerFreshness || now.Before(signal.ObservedAt) {
		value.Status = "stale"
		value.Reason = "回線情報の更新を確認できません"
		if state.Status == "stopped" {
			value.Reason = "専用の回線観測プロセスは停止しています"
		}
	}
	return value
}
