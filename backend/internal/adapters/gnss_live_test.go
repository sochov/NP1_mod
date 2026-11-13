package adapters

import (
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/trips"
)

func TestCombineGNSSUsesThePerSecondPositionWithARecentFix(t *testing.T) {
	now := time.Date(2026, 11, 14, 3, 0, 0, 0, time.UTC)
	sats, hdop := 9, 0.7
	fix := &trips.Sample{At: now.Add(-2 * time.Minute), Lat: 35.6800, Lon: 139.7600, Satellites: &sats, HDOP: &hdop}
	meta := &trips.MetaPosition{At: now.Add(-time.Second), Lat: 35.6812, Lon: 139.7671}
	v := combineGNSS(now, meta, false, fix, nil)
	d := v.Data.(map[string]any)
	if v.Status != "available" || d["live"] != true || d["lat"] != 35.6812 || d["ageSeconds"] != 1 {
		t.Fatalf("got %v %v", v.Status, d)
	}
	if d["satellites"] != &sats || d["qualityAgeSeconds"] != 120 {
		t.Fatalf("quality must come from NMEA with its age: %v", d)
	}
}

func TestCombineGNSSDoesNotCallAStaleRecordLive(t *testing.T) {
	now := time.Date(2026, 11, 14, 3, 0, 0, 0, time.UTC)
	meta := &trips.MetaPosition{At: now, Lat: 35.6812, Lon: 139.7671}
	v := combineGNSS(now, meta, false, nil, map[string]any{"sourceFiles": 3})
	d := v.Data.(map[string]any)
	if v.Status == "available" || d["fix"] != false || d["lastKnown"] == nil || d["sourceFiles"] != 3 {
		t.Fatalf("got %v %v", v.Status, d)
	}
}

func TestCombineGNSSTrustsAMovingRecordWithoutAFix(t *testing.T) {
	now := time.Date(2026, 11, 14, 3, 0, 0, 0, time.UTC)
	meta := &trips.MetaPosition{At: now, Lat: 35.6812, Lon: 139.7671}
	v := combineGNSS(now, meta, true, nil, nil)
	d := v.Data.(map[string]any)
	if v.Status != "available" || d["live"] != true || d["satellites"] != nil {
		t.Fatalf("got %v %v", v.Status, d)
	}
}

func TestCombineGNSSRejectsARecordFarFromTheFix(t *testing.T) {
	now := time.Date(2026, 11, 14, 3, 0, 0, 0, time.UTC)
	fix := &trips.Sample{At: now.Add(-time.Minute), Lat: 35.6800, Lon: 139.7600}
	meta := &trips.MetaPosition{At: now, Lat: 34.0, Lon: 135.0}
	d := combineGNSS(now, meta, false, fix, nil).Data.(map[string]any)
	if d["live"] != false || d["lat"] != 35.6800 {
		t.Fatalf("got %v", d)
	}
}

func TestMetaMovingNeedsRealMovement(t *testing.T) {
	at := time.Date(2026, 11, 14, 3, 0, 0, 0, time.UTC)
	still := []trips.MetaPosition{{At: at.Add(-10 * time.Second), Lat: 35.68, Lon: 139.76}, {At: at, Lat: 35.68001, Lon: 139.76}}
	if metaMoving(still, at) {
		t.Fatal("a metre of jitter is not movement")
	}
	moved := []trips.MetaPosition{{At: at.Add(-10 * time.Second), Lat: 35.68, Lon: 139.76}, {At: at, Lat: 35.6805, Lon: 139.76}}
	if !metaMoving(moved, at) {
		t.Fatal("55 m in ten seconds is movement")
	}
	old := []trips.MetaPosition{{At: at.Add(-5 * time.Minute), Lat: 35.60, Lon: 139.76}, {At: at, Lat: 35.68, Lon: 139.76}}
	if metaMoving(old, at) {
		t.Fatal("movement outside the window does not count")
	}
}
