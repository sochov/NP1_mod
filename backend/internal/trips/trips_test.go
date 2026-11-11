package trips

import (
	"math"
	"strings"
	"testing"
	"time"
)

func sample(at time.Time, lat, lon float64, speed float64) Sample {
	return Sample{At: at, Lat: lat, Lon: lon, Valid: true, Quality: 1, SpeedMPS: &speed}
}
func TestNoFixIsUnknown(t *testing.T) {
	raw := Sentence("GPRMC,,V,,,,,,,,,,N") + "\n" + Sentence("GPGGA,,,,,,0,,,,,,,,")
	report, e := ParseNMEA(strings.NewReader(raw), "test")
	if e != nil || report.NoFix != 1 || report.ValidFixes != 0 {
		t.Fatal(report, e)
	}
	s := Stats(report.Samples, time.Now())
	if s.Periods["7"].DistanceKM != nil || s.Periods["7"].HasData {
		t.Fatal("missing fixes must not become zero", s)
	}
}
func TestChecksumAndGGA(t *testing.T) {
	raw := Sentence("GPRMC,120000.00,A,3500.0000,N,13900.0000,E,10.0,90.0,111126,,,A") + "\n" + Sentence("GPGGA,120000.00,3500.0000,N,13900.0000,E,1,08,1.2,0,M,0,M,,")
	r, e := ParseNMEA(strings.NewReader(raw), "test")
	if e != nil || r.ValidFixes != 1 || r.Samples[0].HDOP == nil || r.Samples[0].AccuracyM != nil {
		t.Fatal(r, e)
	}
	r, _ = ParseNMEA(strings.NewReader(strings.Replace(raw, "*", "*00", 1)), "test")
	if r.BadChecksum != 1 {
		t.Fatal(r)
	}
}
func TestStatisticsMissingStationaryAndPeriods(t *testing.T) {
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, Japan)
	a := sample(now.Add(-time.Second), 35, 139, 0)
	b := sample(now, 35, 139, 0)
	s := Stats([]Sample{a, b}, now)
	for key, count := range map[string]int{"7": 7, "30": 30, "90": 90, "365": 365} {
		p := s.Periods[key]
		if !p.HasData || p.DistanceKM == nil || *p.DistanceKM != 0 || len(p.Days) != count || p.ValidObservedDays != 1 {
			t.Fatal(key, p)
		}
		if p.Days[0].Observed || p.Days[0].DistanceKM != nil {
			t.Fatal("missing day fabricated")
		}
	}
}
func TestMissingGapAndJumpNeverBridge(t *testing.T) {
	at := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	a := sample(at, 35, 139, 10)
	b := sample(at.Add(time.Second), 35, 139.0001, 10)
	c := sample(at.Add(20*time.Second), 35, 140, 10)
	d := sample(at.Add(21*time.Second), 35, 141, 10)
	if trips := BuildTrips([]Sample{a, b, c, d}); len(trips) != 1 || trips[0].SampleCount != 2 {
		t.Fatal(trips)
	}
	b.BreakBefore = true
	if trips := BuildTrips([]Sample{a, b}); len(trips) != 0 {
		t.Fatal(trips)
	}
}
func TestMidnightSplitsAndDuplicateCamera(t *testing.T) {
	midnight := time.Date(2026, 11, 11, 0, 0, 0, 0, Japan)
	a := sample(midnight.Add(-time.Second), 35, 139, 10)
	b := sample(midnight.Add(time.Second), 35, 139.0002, 10)
	s := Stats([]Sample{a, a, b, b}, midnight.Add(time.Hour))
	p := s.Periods["7"]
	if p.ValidObservedDays != 2 || p.SampleCount != 2 || math.Abs(*p.DrivingSeconds-2) > 0.001 {
		t.Fatal(p)
	}
}
