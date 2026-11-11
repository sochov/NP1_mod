package adapters

import (
	"context"
	"github.com/np1-control-center/ncc/internal/model"
	"github.com/np1-control-center/ncc/internal/modem"
	"path/filepath"
	"testing"
	"time"
)

func TestModemFileObservationFreshnessAndNoPDPInference(t *testing.T) {
	at := time.Now().UTC()
	yes := true
	rsrp := -85.0
	state := modem.WorkerState{Version: 1, Mode: modem.WithStatus, Status: "available", UpdatedAt: at, Signal: &modem.GobiNetSignal{ObservedAt: at, Source: "gobinet-nas-independent-cid", RSRP: &rsrp, Registered: &yes, PSAttached: &yes, PLMN: "00101", Band: "B19"}}
	out := modemWorkerObservation(state, at)
	data := out.Data.(map[string]any)
	if out.Status != "available" || data["pdpConnected"] != nil || data["sinr"] != nil || data["psAttached"] != &yes || data["plmn"] != "00101" {
		t.Fatal(out)
	}
	if out = modemWorkerObservation(state, at.Add(36*time.Second)); out.Status != "stale" {
		t.Fatal(out)
	}
	state.UpdatedAt = at.Add(time.Minute)
	if out = modemWorkerObservation(state, at.Add(time.Minute)); out.Status != "stale" {
		t.Fatal("worker heartbeat refreshed old signal", out)
	}
	state.Status = "stopped"
	if out = modemWorkerObservation(state, at); out.Status != "stale" {
		t.Fatal(out)
	}
}
func TestModemMissingWorkerIsUnknownOrCachedStale(t *testing.T) {
	observer := NewModemObserver(context.Background())
	observer.path = filepath.Join(t.TempDir(), "missing-state.json")
	if out := observer.Get(); out.Status != "unknown" || out.Data != nil {
		t.Fatal(out)
	}
	observer.latest = model.Value("synthetic", map[string]any{"rsrp": -85})
	if out := observer.Get(); out.Status != "stale" || out.Data == nil {
		t.Fatal(out)
	}
}
