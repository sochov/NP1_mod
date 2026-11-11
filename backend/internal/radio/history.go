package radio

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Position struct {
	At        time.Time `json:"at"`
	Lat       float64   `json:"lat"`
	Lon       float64   `json:"lon"`
	AccuracyM *float64  `json:"accuracyM,omitempty"`
	HDOP      *float64  `json:"hdop,omitempty"`
	Source    string    `json:"source"`
}
type Observation struct {
	At         time.Time `json:"at"`
	Registered *bool     `json:"registered"`
	Carrier    string    `json:"carrier,omitempty"`
	PLMN       string    `json:"plmn,omitempty"`
	RAT        string    `json:"rat,omitempty"`
	Band       string    `json:"band,omitempty"`
	EARFCN     *int      `json:"earfcn,omitempty"`
	PCI        *int      `json:"pci,omitempty"`
	RSRP       *float64  `json:"rsrp"`
	RSRQ       *float64  `json:"rsrq"`
	SINR       *float64  `json:"sinr"`
	SNR        *float64  `json:"snr"`
	RSSI       *float64  `json:"rssi"`
	Source     string    `json:"source"`
	Trip       string    `json:"trip,omitempty"`
	GPS        *Position `json:"gps,omitempty"`
}
type History struct {
	mu        sync.Mutex
	directory string
}

func NewHistory(directory string) *History { return &History{directory: directory} }

const maximumHistoryBytes = 16 * 1024 * 1024

// Append records measured observations from the continuous local logger.
// NewHistory/Query never create files.
func (h *History) Append(o Observation) error {
	if o.At.IsZero() || o.Source == "" {
		return errors.New("observation timestamp and source required")
	}
	if h.directory == "" {
		return errors.New("history directory unavailable")
	}
	if o.GPS != nil {
		gps := *o.GPS
		o.GPS = &gps
		if !validPosition(o) {
			o.GPS = nil
		}
	}
	raw, e := json.Marshal(o)
	if e != nil {
		return e
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if e = os.MkdirAll(h.directory, 0700); e != nil {
		return e
	}
	filename := filepath.Join(h.directory, "radio-observations.jsonl")
	if st, e := os.Stat(filename); e == nil && st.Size()+int64(len(raw))+1 > maximumHistoryBytes {
		previous := filename + ".1"
		if e = os.Remove(previous); e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if e = os.Rename(filename, previous); e != nil {
			return e
		}
	}
	f, e := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	_, e = f.Write(append(raw, '\n'))
	if e != nil {
		return e
	}
	return f.Sync()
}

type Query struct {
	From, To               time.Time
	Metric                 string
	Carrier, Band          string
	CellSize               int
	TripWeighted           bool
	IncludeUnknownAccuracy bool
}
type MapPoint struct {
	Observation
	Value           *float64 `json:"value"`
	State           string   `json:"state"`
	Color           string   `json:"color"`
	AccuracyUnknown bool     `json:"accuracyUnknown"`
}
type MapCell struct {
	Cell
	Bounds [2][2]float64 `json:"bounds"`
	Color  string        `json:"color"`
}
type Result struct {
	TripWeighted     bool       `json:"tripWeighted"`
	TripCountKnown   bool       `json:"tripCountKnown"`
	Metric           string     `json:"metric"`
	Unit             string     `json:"unit"`
	Points           []MapPoint `json:"points"`
	Cells            []MapCell  `json:"cells"`
	Total            int        `json:"total"`
	Unplaced         int        `json:"unplaced"`
	AccuracyExcluded int        `json:"accuracyExcluded"`
	Missing          int        `json:"missing"`
	OutOfService     int        `json:"outOfService"`
	Carriers         []string   `json:"carriers"`
	Bands            []string   `json:"bands"`
	Truncated        bool       `json:"truncated"`
	Reason           string     `json:"reason,omitempty"`
}

func validPosition(o Observation) bool {
	p := o.GPS
	if p == nil || p.At.IsZero() || math.Abs(o.At.Sub(p.At).Seconds()) > 5 || math.IsNaN(p.Lat) || math.IsNaN(p.Lon) || math.Abs(p.Lat) > 85 || math.Abs(p.Lon) > 180 || (p.Lat == 0 && p.Lon == 0) {
		return false
	}
	if p.AccuracyM != nil && (!finiteNumber(*p.AccuracyM) || *p.AccuracyM <= 0 || *p.AccuracyM > 50) {
		return false
	}
	return p.HDOP == nil || finiteNumber(*p.HDOP) && *p.HDOP > 0 && *p.HDOP <= 5
}
func finiteNumber(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func metricValue(o Observation, metric string) *float64 {
	switch metric {
	case "sinr":
		return o.SINR
	case "rsrq":
		return o.RSRQ
	default:
		return o.RSRP
	}
}
func pointColor(metric string, value *float64, registered *bool) string {
	state := Classify(metric, value, registered)
	if state == "out-of-service" {
		return "#3f4a5a"
	}
	if state == "missing" {
		return "#a8afb8"
	}
	scale, _ := Legend(metric)
	for _, t := range scale.Thresholds {
		if t.Label == state {
			return t.Color
		}
	}
	return "#a8afb8"
}
func (h *History) Query(q Query) (Result, error) {
	if q.Metric == "" {
		q.Metric = "rsrp"
	}
	scale, e := Legend(q.Metric)
	out := Result{Metric: q.Metric, Unit: scale.Unit, Points: []MapPoint{}, Cells: []MapCell{}, Carriers: []string{}, Bands: []string{}}
	if h.directory == "" {
		return out, errors.New("history directory unavailable")
	}
	if e != nil {
		return out, e
	}
	if q.CellSize == 0 {
		q.CellSize = 50
	}
	if q.CellSize != 20 && q.CellSize != 50 && q.CellSize != 100 {
		return out, errors.New("invalid grid size")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	observations := []Observation{}
	for _, suffix := range []string{".1", ""} {
		f, e := os.Open(filepath.Join(h.directory, "radio-observations.jsonl"+suffix))
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return out, e
		}
		scanner := bufio.NewScanner(io.LimitReader(f, maximumHistoryBytes+1))
		scanner.Buffer(make([]byte, 4096), 64*1024)
		for scanner.Scan() {
			var o Observation
			if json.Unmarshal(scanner.Bytes(), &o) != nil || o.At.IsZero() {
				continue
			}
			if !q.From.IsZero() && o.At.Before(q.From) || !q.To.IsZero() && o.At.After(q.To) {
				continue
			}
			observations = append(observations, o)
		}
		e = scanner.Err()
		f.Close()
		if e != nil {
			return out, e
		}
	}
	sort.SliceStable(observations, func(a, b int) bool { return observations[a].At.Before(observations[b].At) })
	if len(observations) > 20000 {
		observations = observations[len(observations)-20000:]
		out.Truncated = true
	}
	carriers, bands := map[string]bool{}, map[string]bool{}
	grid := []Point{}
	allTripsKnown := true
	for _, o := range observations {
		if o.Carrier != "" {
			carriers[o.Carrier] = true
		}
		if o.Band != "" {
			bands[o.Band] = true
		}
		if q.Carrier != "" && o.Carrier != q.Carrier || q.Band != "" && o.Band != q.Band {
			continue
		}
		out.Total++
		value := metricValue(o, q.Metric)
		state := Classify(q.Metric, value, o.Registered)
		if state == "missing" {
			out.Missing++
		}
		if state == "out-of-service" {
			out.OutOfService++
		}
		if !validPosition(o) {
			out.Unplaced++
			continue
		}
		unknown := o.GPS.AccuracyM == nil
		if unknown && !q.IncludeUnknownAccuracy {
			out.AccuracyExcluded++
			continue
		}
		out.Points = append(out.Points, MapPoint{o, value, state, pointColor(q.Metric, value, o.Registered), unknown})
		if !unknown {
			trip := o.Trip
			if trip == "" {
				trip = "unassigned"
				allTripsKnown = false
			}
			grid = append(grid, Point{Trip: trip, At: o.At, GPSAt: o.GPS.At, Lat: o.GPS.Lat, Lon: o.GPS.Lon, Accuracy: *o.GPS.AccuracyM, Metric: q.Metric, Value: value, Registered: o.Registered})
		}
	}
	for v := range carriers {
		out.Carriers = append(out.Carriers, v)
	}
	for v := range bands {
		out.Bands = append(out.Bands, v)
	}
	sort.Strings(out.Carriers)
	sort.Strings(out.Bands)
	out.TripCountKnown = allTripsKnown
	out.TripWeighted = q.TripWeighted && allTripsKnown
	cells, e := Grid(grid, q.Metric, q.CellSize, out.TripWeighted)
	if e != nil {
		return out, e
	}
	const radius = 6371008.8
	cos := math.Cos(math.Pi / 6)
	for _, cell := range cells {
		if !allTripsKnown {
			cell.Trips = 0
		}
		x, y := float64(cell.X)*float64(q.CellSize), float64(cell.Y)*float64(q.CellSize)
		inverse := func(x, y float64) [2]float64 {
			return [2]float64{math.Asin(math.Min(1, math.Max(-1, y*cos/radius))) * 180 / math.Pi, x / (radius * cos) * 180 / math.Pi}
		}
		v := cell.Median
		out.Cells = append(out.Cells, MapCell{cell, [2][2]float64{inverse(x, y), inverse(x+float64(q.CellSize), y+float64(q.CellSize))}, pointColor(q.Metric, &v, nil)})
	}
	if out.Total == 0 {
		out.Reason = "対象期間の電波観測はまだありません"
	} else if len(out.Points) == 0 {
		out.Reason = "電波観測はありますが、同時刻の有効な測位または精度情報が不足しています"
	}
	return out, nil
}
