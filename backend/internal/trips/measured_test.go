package trips

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMeasuredBatchPagesOnlyValidIntervalsWithoutCoordinates(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	points := []Sample{sample(now.Add(-4*time.Second), 35, 139, 1), sample(now.Add(-3*time.Second), 35, 139, 1), sample(now.Add(-2*time.Second), 35, 139, 1), sample(now.Add(-time.Second), 35, 139, 1), sample(now, 35, 139, 1)}
	// Same instant from a second camera cannot create a second interval.
	points = append(points, points[1])
	snapshot := Snapshot{Samples: points, Files: 2, NoFix: 10}
	first, e := MeasuredBatch(snapshot, time.Time{}, 2, now)
	if e != nil || len(first.Intervals) != 2 || !first.More || !first.Next.Equal(now.Add(-2*time.Second)) {
		t.Fatal(first, e)
	}
	second, e := MeasuredBatch(snapshot, first.Next, 2, now)
	if e != nil || len(second.Intervals) != 2 || second.More || !second.Next.Equal(now) {
		t.Fatal(second, e)
	}
	if first.Health.NoFix != 10 || first.Health.Files != 2 {
		t.Fatal("source diagnostics lost")
	}
	raw, _ := json.Marshal(first)
	var wire map[string]any
	if json.Unmarshal(raw, &wire) != nil {
		t.Fatal("bad JSON")
	}
	interval := wire["intervals"].([]any)[0].(map[string]any)
	if len(interval) != 4 || interval["lat"] != nil || interval["lon"] != nil {
		t.Fatal("coordinates crossed aggregate transport", interval)
	}
	points[2].BreakBefore = true
	points = append(points, sample(now.Add(time.Second), 35, 139, 1))
	b, e := MeasuredBatch(Snapshot{Samples: points}, time.Time{}, MaximumMeasuredBatch, now)
	if e != nil || len(b.Intervals) != 3 {
		t.Fatal("break or future record accepted", len(b.Intervals), e)
	}
}

func TestMeasuredBatchResponseIsBounded(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 123456789, time.UTC)
	points := make([]Sample, MaximumMeasuredBatch+2)
	for n := range points {
		points[n] = sample(now.Add(time.Duration(n-len(points))*time.Second), 35, 139, 1)
	}
	batch, e := MeasuredBatch(Snapshot{Samples: points}, time.Time{}, MaximumMeasuredBatch, now)
	raw, encodeError := json.Marshal(batch)
	if e != nil || encodeError != nil || len(batch.Intervals) != MaximumMeasuredBatch || !batch.More || len(raw) > 1024*1024-16384 {
		t.Fatal("batch transport budget", len(raw), e, encodeError)
	}
	if _, e = MeasuredBatch(Snapshot{}, now.Add(time.Second), 1, now); e == nil {
		t.Fatal("future cursor accepted")
	}
	if _, e = MeasuredBatch(Snapshot{}, time.Time{}, MaximumMeasuredBatch+1, now); e == nil {
		t.Fatal("unbounded batch accepted")
	}
}
