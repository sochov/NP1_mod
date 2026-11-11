// Package radio implements reproducible derived radio-quality cells. It never invents observations.
package radio

import (
	"errors"
	"math"
	"sort"
	"time"
)

type Point struct {
	Trip               string
	At                 time.Time
	GPSAt              time.Time
	Lat, Lon, Accuracy float64
	Metric             string
	Value              *float64
	Registered         *bool
}
type Cell struct {
	X             int64     `json:"x"`
	Y             int64     `json:"y"`
	Count         int       `json:"sample_count"`
	Trips         int       `json:"distinct_trip_count"`
	Mean          float64   `json:"mean"`
	Median        float64   `json:"median"`
	Min           float64   `json:"min"`
	Max           float64   `json:"max"`
	P10           float64   `json:"p10"`
	P90           float64   `json:"p90"`
	Last          time.Time `json:"last_observed_at"`
	LowConfidence bool      `json:"low_confidence"`
}
type Threshold struct {
	Minimum *float64 `json:"minimum"`
	Label   string   `json:"label"`
	Color   string   `json:"color"`
}
type Scale struct {
	Metric     string      `json:"metric"`
	Unit       string      `json:"unit"`
	Thresholds []Threshold `json:"thresholds"`
	Note       string      `json:"note"`
}

func ptr(v float64) *float64 { return &v }
func Legend(metric string) (Scale, error) {
	if metric == "" {
		metric = "rsrp"
	}
	s := Scale{Metric: metric, Unit: "dB", Note: "UI初期値。通信可否を保証する境界ではありません"}
	switch metric {
	case "rsrp":
		s.Unit = "dBm"
		s.Thresholds = []Threshold{{ptr(-80), "良好", "#247976"}, {ptr(-90), "やや良好", "#6c9a76"}, {ptr(-100), "普通", "#b89d52"}, {ptr(-110), "弱い", "#c37b48"}, {nil, "非常に弱い", "#ac514b"}}
	case "sinr":
		s.Thresholds = []Threshold{{ptr(20), "良好", "#247976"}, {ptr(13), "やや良好", "#6c9a76"}, {ptr(0), "低い", "#b89d52"}, {nil, "非常に低い", "#ac514b"}}
	case "rsrq":
		s.Thresholds = []Threshold{{ptr(-10), "良好", "#247976"}, {ptr(-15), "普通", "#b89d52"}, {ptr(-20), "低い", "#c37b48"}, {nil, "非常に低い", "#ac514b"}}
	default:
		return s, errors.New("metric must be rsrp, sinr or rsrq")
	}
	return s, nil
}
func Valid(metric string, v float64) bool {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return false
	}
	switch metric {
	case "rsrp":
		return v >= -160 && v <= -30
	case "sinr":
		return v >= -30 && v <= 50
	case "rsrq":
		return v >= -40 && v <= -1
	}
	return false
}
func Classify(metric string, v *float64, registered *bool) string {
	if registered != nil && !*registered {
		return "out-of-service"
	}
	if v == nil || !Valid(metric, *v) {
		return "missing"
	}
	s, e := Legend(metric)
	if e != nil {
		return "missing"
	}
	for _, t := range s.Thresholds {
		if t.Minimum == nil || *v >= *t.Minimum {
			return t.Label
		}
	}
	return "missing"
}
func quantile(v []float64, p float64) float64 {
	pos := float64(len(v)-1) * p
	i := int(pos)
	if i+1 >= len(v) {
		return v[i]
	}
	return v[i] + (v[i+1]-v[i])*(pos-float64(i))
}

// Spherical cylindrical equal-area projection: R=6371008.8m, standard parallel 30°.
// Fixed geographic cells remain identical at every display zoom. This is not EPSG:6933.
func Grid(points []Point, metric string, size int, tripWeighted bool) ([]Cell, error) {
	if _, e := Legend(metric); e != nil {
		return nil, e
	}
	if size != 20 && size != 50 && size != 100 {
		return nil, errors.New("cell size must be 20,50,100 metres")
	}
	type bucket struct {
		values []float64
		trips  map[string][]float64
		last   time.Time
	}
	b := map[[2]int64]*bucket{}
	const radius = 6371008.8
	cos := math.Cos(math.Pi / 6)
	for _, p := range points {
		if p.Metric != metric || p.Value == nil || !Valid(metric, *p.Value) || p.Trip == "" || p.Registered != nil && !*p.Registered || p.Accuracy <= 0 || p.Accuracy > 50 || math.IsNaN(p.Lat) || math.IsNaN(p.Lon) || math.Abs(p.Lat) > 85 || math.Abs(p.Lon) > 180 || p.At.IsZero() || p.GPSAt.IsZero() || math.Abs(p.At.Sub(p.GPSAt).Seconds()) > 5 {
			continue
		}
		x := radius * cos * p.Lon * math.Pi / 180
		y := radius * math.Sin(p.Lat*math.Pi/180) / cos
		k := [2]int64{int64(math.Floor(x / float64(size))), int64(math.Floor(y / float64(size)))}
		v := b[k]
		if v == nil {
			v = &bucket{trips: map[string][]float64{}}
			b[k] = v
		}
		v.values = append(v.values, *p.Value)
		v.trips[p.Trip] = append(v.trips[p.Trip], *p.Value)
		if p.At.After(v.last) {
			v.last = p.At
		}
	}
	out := []Cell{}
	for k, b := range b {
		values := b.values
		if tripWeighted {
			values = []float64{}
			for _, v := range b.trips {
				sort.Float64s(v)
				values = append(values, quantile(v, .5))
			}
		}
		sort.Float64s(values)
		sum := 0.0
		for _, v := range values {
			sum += v
		}
		out = append(out, Cell{k[0], k[1], len(b.values), len(b.trips), sum / float64(len(values)), quantile(values, .5), values[0], values[len(values)-1], quantile(values, .1), quantile(values, .9), b.last, len(b.values) < 3})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].X != out[j].X {
			return out[i].X < out[j].X
		}
		return out[i].Y < out[j].Y
	})
	return out, nil
}
