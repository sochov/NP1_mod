package adapters

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/trips"
)

func TestPlateMetaNearPicksTheDetectionSecond(t *testing.T) {
	base := time.Date(2026, 11, 14, 14, 0, 0, 0, time.UTC)
	var tr metaTracker
	for s := 0; s < 60; s++ {
		// About 15 m/s north.
		tr.history = append(tr.history, trips.MetaPosition{At: base.Add(time.Duration(s) * time.Second), Lat: 35.5 + float64(s)*0.000135, Lon: 139.4})
	}
	p, moving := tr.near(base.Add(20*time.Second+300*time.Millisecond), livePositionTolerance)
	if p == nil || !p.At.Equal(base.Add(20*time.Second)) || !moving {
		t.Fatalf("near = %+v moving %v", p, moving)
	}
	if p, _ := tr.near(base.Add(70*time.Second), livePositionTolerance); p != nil {
		t.Fatalf("a record beyond the tolerance was used: %+v", p)
	}
}

func TestSharedLivePositionsKeepTheTimeOfALongPlateTrack(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(-8 * time.Minute)
	liveMeta.mu.Lock()
	previous := liveMeta.history
	liveMeta.history = []trips.MetaPosition{
		{At: start.Add(-4 * time.Second), Lat: 35.5, Lon: 139.5},
		{At: start, Lat: 35.5004, Lon: 139.5},
		{At: now.Add(-5 * time.Second), Lat: 35.52, Lon: 139.5},
		{At: now, Lat: 35.521, Lon: 139.5},
	}
	liveMeta.mu.Unlock()
	t.Cleanup(func() {
		liveMeta.mu.Lock()
		liveMeta.history = previous
		liveMeta.mu.Unlock()
	})
	q := url.Values{"at": {start.Format(time.RFC3339), now.Add(-4 * time.Minute).Format(time.RFC3339), now.Format(time.RFC3339)}}
	w := httptest.NewRecorder()
	HandleLivePositions(w, httptest.NewRequest("GET", "/gps/positions?"+q.Encode(), nil))
	var response struct{ Positions []*trips.Sample }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil || len(response.Positions) != 3 {
		t.Fatal(w.Code, w.Body.String())
	}
	first, gap, last := response.Positions[0], response.Positions[1], response.Positions[2]
	if first == nil || !first.At.Equal(start) || first.Lat != 35.5004 || gap != nil || last == nil || !last.At.Equal(now) {
		t.Fatalf("wrong historical or current GPS association: %s", w.Body.String())
	}
	current := LiveGNSS().Data.(map[string]any)
	if current["lat"] != last.Lat || current["at"] != last.At {
		t.Fatalf("current GPS used a different source: %v", current)
	}
}

func TestPlateMetaTrustNeedsMovementOrANearbyFix(t *testing.T) {
	at := time.Date(2026, 11, 14, 14, 0, 0, 0, time.UTC)
	meta := trips.MetaPosition{At: at, Lat: 35.5, Lon: 139.4}
	if metaTrusted(at, meta, false, nil) {
		t.Fatal("a stationary record without any fix was trusted")
	}
	near := &trips.Sample{At: at.Add(-2 * time.Minute), Lat: 35.501, Lon: 139.4}
	if !metaTrusted(at, meta, false, near) {
		t.Fatal("a stationary record next to a recent fix was not trusted")
	}
	far := &trips.Sample{At: at.Add(-10 * time.Second), Lat: 36.5, Lon: 139.4}
	if metaTrusted(at, meta, false, far) {
		t.Fatal("a stationary record far from the newest fix was trusted")
	}
	if !metaTrusted(at, meta, true, nil) {
		t.Fatal("a moving record was not trusted")
	}
}
