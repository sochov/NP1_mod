package trips

import (
	"testing"
	"time"
)

func TestDayTrackKeepsUTCAndNeverBridgesGaps(t *testing.T) {
	at := time.Date(2026, 11, 11, 14, 59, 58, 0, time.UTC)
	a := sample(at, 35, 139, 10)
	b := sample(at.Add(time.Second), 35, 139.0001, 10)
	missing := Sample{At: at.Add(2 * time.Second)}
	c := sample(at.Add(3*time.Second), 35, 139.0002, 10)
	d := sample(at.Add(4*time.Second), 35, 139.0003, 10)
	jump := sample(at.Add(5*time.Second), 36, 140, 10)
	out, err := DayTrack(Snapshot{Samples: []Sample{b, a, a, missing, c, d, jump}}, "2026-11-11", at.Add(time.Hour))
	if err != nil || len(out.Points) != 2 || len(out.Segments) != 1 || out.Points[0].At != at || out.Segments[0].DistanceKM == nil {
		t.Fatal(out, err)
	}
	out, err = DayTrack(Snapshot{Samples: []Sample{a, b, missing, c, d, jump}}, "2026-11-12", at.Add(time.Hour))
	if err != nil || len(out.Points) != 3 || len(out.Segments) != 2 || out.Segments[1].DistanceKM != nil {
		t.Fatal("midnight, invalid fix or jump joined", out, err)
	}
}
func TestDayTrackRejectsBadDateAndFutureFixes(t *testing.T) {
	at := time.Date(2026, 11, 11, 1, 0, 0, 0, time.UTC)
	for _, date := range []string{"", "2026-02-30", "2026-1-01", "../2026-11-11"} {
		if _, err := DayTrack(Snapshot{}, date, at); err == nil {
			t.Fatal(date)
		}
	}
	out, err := DayTrack(Snapshot{Samples: []Sample{sample(at.Add(time.Second), 35, 139, 10)}}, "2026-11-11", at)
	if err != nil || len(out.Points) != 0 || out.Reason == "" {
		t.Fatal("future position displayed", out, err)
	}
}
