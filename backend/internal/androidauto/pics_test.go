package androidauto

import (
	"testing"
	"time"
)

func i64(v int64) *int64 { return &v }
func ip(v int) *int      { return &v }

func TestSignalAtFollowsTheWebDashboard(t *testing.T) {
	start := time.Unix(1000, 0)
	snap := picsSnapshot{ok: true, started: start}
	snap.status.Settings.Enabled = true
	snap.status.Signals = []picsSignal{{Color: "red", ValidForMs: i64(1650), EstimateMs: i64(27000), EstimateForMs: i64(30000)}}
	if s := snap.signalAt(start.Add(300 * time.Millisecond)); s == nil || s.Text != "青まであと27秒" || !s.Estimate {
		t.Fatalf("%+v", s)
	}
	if s := snap.signalAt(start.Add(1700 * time.Millisecond)); s != nil {
		t.Fatalf("expired signal shown: %+v", s)
	}
	snap.status.Signals = []picsSignal{{Color: "red", Seconds: ip(5), ValidForMs: i64(1650)}}
	if s := snap.signalAt(start); s == nil || s.Text != "青まであと5秒" {
		t.Fatalf("%+v", s)
	}
	snap.status.Signals = []picsSignal{{Color: "blink-green"}, {Color: "green", DirectionMatch: true}}
	if s := snap.signalAt(start); s == nil || s.Text != "青点滅" {
		t.Fatalf("%+v", s)
	}
	snap.status.Settings.Directional = true
	if s := snap.signalAt(start); s == nil || s.Text != "青" {
		t.Fatalf("directional: %+v", s)
	}
	snap.status.Settings.Enabled = false
	if snap.signalAt(start) != nil {
		t.Fatal("disabled PICS shown")
	}
}
