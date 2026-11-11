package radio

import (
	"math"
	"testing"
	"time"
)

func TestLegendBoundariesAndMissing(t *testing.T) {
	for _, v := range []struct {
		value float64
		want  string
	}{{-80, "良好"}, {-80.01, "やや良好"}, {-90, "やや良好"}, {-100, "普通"}, {-110, "弱い"}, {-110.01, "非常に弱い"}, {0, "missing"}, {math.NaN(), "missing"}} {
		if got := Classify("rsrp", ptr(v.value), nil); got != v.want {
			t.Errorf("%v: %s != %s", v.value, got, v.want)
		}
	}
	if Classify("rsrp", nil, nil) != "missing" {
		t.Fatal("missing not distinguished")
	}
	no := false
	if Classify("rsrp", nil, &no) != "out-of-service" {
		t.Fatal("out of service not distinguished")
	}
}
func TestGridTimeJoinAndTripWeighting(t *testing.T) {
	now := time.Now().UTC()
	points := []Point{}
	for i := 0; i < 9; i++ {
		points = append(points, Point{Trip: "parked", At: now, GPSAt: now, Lat: 35, Lon: 139, Accuracy: 10, Metric: "rsrp", Value: ptr(-70)})
	}
	points = append(points, Point{Trip: "other-trip", At: now, GPSAt: now, Lat: 35, Lon: 139, Accuracy: 10, Metric: "rsrp", Value: ptr(-110)})
	bad := points[0]
	bad.Trip = "bad-time"
	bad.GPSAt = now.Add(-6 * time.Second)
	points = append(points, bad)
	bad = points[0]
	bad.Value = ptr(0)
	points = append(points, bad)
	bad = points[0]
	bad.Metric = "sinr"
	bad.Value = ptr(20)
	points = append(points, bad)
	simple, e := Grid(points, "rsrp", 50, false)
	if e != nil || len(simple) != 1 {
		t.Fatalf("grid: %v %+v", e, simple)
	}
	weighted, _ := Grid(points, "rsrp", 50, true)
	if simple[0].Count != 10 || simple[0].Trips != 2 || simple[0].Median != -70 || weighted[0].Median != -90 {
		t.Fatalf("incorrect statistics: %+v %+v", simple, weighted)
	}
	if _, e := Grid(points, "rsrp", 30, false); e == nil {
		t.Fatal("invalid cell size accepted")
	}
}
