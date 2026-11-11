package trips

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestHistoryNoFixIsUnknownAndNoCoordinatesAreExported(t *testing.T) {
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, Japan)
	report, e := History(Snapshot{Files: 1, NoFix: 180}, HistoryOptions{Period: "all"}, now)
	if e != nil || report.Status != "unknown" || report.Period.DistanceKM != nil || report.Period.DrivingSeconds != nil || len(report.Trips) != 0 || report.NoFixSentences != 180 || report.Range.SourceFrom != "" {
		t.Fatal(report, e)
	}
	a := sample(now.Add(-time.Second), 35, 139, 0)
	b := sample(now, 35, 139, 0)
	report, e = History(Snapshot{Samples: []Sample{a, b}}, HistoryOptions{Period: "7"}, now)
	if e != nil || report.Status != "available" || *report.Period.DistanceKM != 0 || *report.Period.DrivingSeconds != 0 || len(report.Trips) != 1 || report.Trips[0].SampleCount != 2 || report.Days[0].DistanceKM != nil {
		t.Fatal(report, e)
	}
	raw, _ := json.Marshal(report)
	if strings.Contains(string(raw), `"lat"`) || strings.Contains(string(raw), `"lon"`) {
		t.Fatal("history unexpectedly contains position data")
	}
}
func TestHistoryCustomClipsAtJSTMidnightAndMatchesMeasuredTotals(t *testing.T) {
	midnight := time.Date(2026, 11, 11, 0, 0, 0, 0, Japan)
	a, b := sample(midnight.Add(-time.Second), 35, 139, 10), sample(midnight.Add(time.Second), 35, 139.0002, 10)
	report, e := History(Snapshot{Samples: []Sample{a, b}}, HistoryOptions{Period: "custom", From: "2026-11-11", To: "2026-11-11"}, midnight.Add(time.Hour))
	if e != nil || len(report.Trips) != 1 || len(report.Days) != 1 || math.Abs(*report.Period.DrivingSeconds-1) > .00001 || !report.Trips[0].Start.Equal(midnight) || report.Trips[0].ObservedSeconds != 1 || math.Abs(report.Trips[0].DistanceKM-*report.Period.DistanceKM) > .00001 {
		t.Fatal(report, e)
	}
}
func TestHistoryAllIsSourceBoundAndNeverBridgesNoFixOrFuture(t *testing.T) {
	at := time.Date(2026, 11, 11, 10, 0, 0, 0, Japan)
	samples := []Sample{sample(at.Add(-48*time.Hour), 35, 139, 10), sample(at.Add(-48*time.Hour+time.Second), 35, 139.0001, 10), {At: at.Add(-24 * time.Hour), Valid: false}, sample(at.Add(-time.Second), 35, 139, 0), sample(at, 35, 139, 0), sample(at.Add(time.Second), 35, 139, 10)}
	report, e := History(Snapshot{Samples: samples}, HistoryOptions{Period: "all"}, at)
	if e != nil || report.Range.From != "2026-11-09" || report.Range.To != "2026-11-11" || len(report.Trips) != 2 || report.TotalTrips != 2 || report.Days[1].Observed || report.Days[1].DrivingSeconds != nil || report.Period.ValidObservedDays != 2 || *report.Period.DrivingSeconds != 1 {
		t.Fatal(report, e)
	}
	legacy := Stats(samples[:len(samples)-1], at).Periods["7"]
	if math.Abs(*legacy.DistanceKM-*report.Period.DistanceKM) > .00001 || *legacy.DrivingSeconds != *report.Period.DrivingSeconds {
		t.Fatal("history differs from established statistics")
	}
}
func TestHistoryRangeValidationAndIsolatedFix(t *testing.T) {
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, Japan)
	for _, options := range []HistoryOptions{{Period: "8"}, {Period: "7", From: "2026-11-11"}, {Period: "all", To: "2026-11-11"}, {Period: "custom", From: "2026-02-30", To: "2026-11-11"}, {Period: "custom", From: "2026-11-12", To: "2026-11-11"}, {Period: "custom", From: "2026-11-11", To: "2026-11-12"}, {Period: "custom", From: "0001-01-01", To: "2026-11-11"}} {
		if _, e := History(Snapshot{}, options, now); e == nil {
			t.Fatal("invalid range accepted", options)
		}
	}
	report, e := History(Snapshot{Samples: []Sample{sample(now, 35, 139, 20)}}, HistoryOptions{Period: "7"}, now)
	if e != nil || report.Status != "unknown" || len(report.Trips) != 0 || report.Days[6].SampleCount != 1 || report.Days[6].DrivingSeconds != nil {
		t.Fatal("isolated fix fabricated motion", report, e)
	}
}
