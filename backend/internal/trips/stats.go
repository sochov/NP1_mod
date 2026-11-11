package trips

import (
	"fmt"
	"math"
	"sort"
	"time"
)

var Japan = time.FixedZone("JST", 9*60*60)

const MaximumGap = 5 * time.Second

type Day struct {
	Date            string   `json:"date"`
	DistanceKM      *float64 `json:"distanceKm"`
	DrivingSeconds  *float64 `json:"drivingSeconds"`
	Observed        bool     `json:"observed"`
	SampleCount     int      `json:"sampleCount"`
	ObservedSeconds float64  `json:"observedSeconds"`
	Reason          string   `json:"reason,omitempty"`
}
type Period struct {
	DistanceKM        *float64 `json:"distanceKm"`
	DrivingSeconds    *float64 `json:"drivingSeconds"`
	HasData           bool     `json:"hasData"`
	ValidObservedDays int      `json:"validObservedDays"`
	SampleCount       int      `json:"sampleCount"`
	Days              []Day    `json:"days"`
}
type Statistics struct {
	Periods         map[string]Period `json:"periods"`
	Source          string            `json:"source"`
	Status          string            `json:"status"`
	Reason          string            `json:"reason,omitempty"`
	ValidSamples    int               `json:"validSamples"`
	LastValidAt     *time.Time        `json:"lastValidAt,omitempty"`
	SourceTruncated bool              `json:"sourceTruncated,omitempty"`
	SourceFiles     int               `json:"sourceFiles,omitempty"`
}
type Trip struct {
	ID              string    `json:"id"`
	Start           time.Time `json:"start"`
	End             time.Time `json:"end"`
	DistanceKM      float64   `json:"distanceKm"`
	DrivingSeconds  float64   `json:"drivingSeconds"`
	ObservedSeconds float64   `json:"observedSeconds"`
	SampleCount     int       `json:"sampleCount"`
	Samples         []Sample  `json:"-"`
}

func ptr(v float64) *float64 { return &v }
func Usable(s Sample) bool {
	return s.Valid && !s.At.IsZero() && s.Quality > 0 && !(s.Lat == 0 && s.Lon == 0) && math.Abs(s.Lat) <= 90 && math.Abs(s.Lon) <= 180 && !math.IsNaN(s.Lat) && !math.IsNaN(s.Lon) && (s.HDOP == nil || (*s.HDOP > 0 && *s.HDOP <= 5)) && (s.AccuracyM == nil || (*s.AccuracyM > 0 && *s.AccuracyM <= 50))
}
func metres(a, b Sample) float64 {
	const radius = 6371008.8
	lat1, lat2 := a.Lat*math.Pi/180, b.Lat*math.Pi/180
	dlat := (b.Lat - a.Lat) * math.Pi / 180
	dlon := (b.Lon - a.Lon) * math.Pi / 180
	v := math.Sin(dlat/2)*math.Sin(dlat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(dlon/2)*math.Sin(dlon/2)
	return 2 * radius * math.Asin(math.Sqrt(math.Min(1, math.Max(0, v))))
}
func ordered(samples []Sample) []Sample {
	out := append([]Sample(nil), samples...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	dedup := make([]Sample, 0, len(out))
	for _, s := range out {
		if s.At.IsZero() {
			continue
		}
		if len(dedup) > 0 && dedup[len(dedup)-1].At.Equal(s.At) {
			old := &dedup[len(dedup)-1]
			cut := old.BreakBefore || s.BreakBefore
			if !Usable(*old) && Usable(s) {
				*old = s
			}
			old.BreakBefore = cut
			continue
		}
		dedup = append(dedup, s)
	}
	return dedup
}
func interval(a, b Sample) (distance, driving, seconds float64, ok bool) {
	dt := b.At.Sub(a.At)
	if !Usable(a) || !Usable(b) || b.BreakBefore || dt <= 0 || dt > MaximumGap {
		return 0, 0, 0, false
	}
	seconds = dt.Seconds()
	d := metres(a, b)
	if d/seconds > 75 {
		return 0, 0, 0, false
	}
	speed := d / seconds
	if a.SpeedMPS != nil && b.SpeedMPS != nil {
		speed = (*a.SpeedMPS + *b.SpeedMPS) / 2
		if speed > 75 {
			return 0, 0, 0, false
		}
	}
	if speed >= 0.5 {
		driving = seconds
		if d >= 3 {
			distance = d / 1000
		} else if a.SpeedMPS != nil && b.SpeedMPS != nil {
			distance = speed * seconds / 1000
		}
	}
	return distance, driving, seconds, true
}
func BuildTrips(samples []Sample) []Trip {
	result := []Trip{}
	ordered := ordered(samples)
	var current *Trip
	var previous *Sample
	for _, s := range ordered {
		if !Usable(s) {
			current = nil
			previous = nil
			continue
		}
		if previous == nil {
			result = append(result, Trip{ID: fmt.Sprintf("trip-%x", s.At.UnixMilli()), Start: s.At, End: s.At, SampleCount: 1, Samples: []Sample{s}})
			current = &result[len(result)-1]
			previous = &current.Samples[len(current.Samples)-1]
			continue
		}
		distance, driving, seconds, ok := interval(*previous, s)
		if !ok {
			result = append(result, Trip{ID: fmt.Sprintf("trip-%x", s.At.UnixMilli()), Start: s.At, End: s.At, SampleCount: 1, Samples: []Sample{s}})
			current = &result[len(result)-1]
		} else {
			current.End = s.At
			current.DistanceKM += distance
			current.DrivingSeconds += driving
			current.ObservedSeconds += seconds
			current.SampleCount++
			current.Samples = append(current.Samples, s)
		}
		previous = &current.Samples[len(current.Samples)-1]
	}
	out := result[:0]
	for _, trip := range result {
		if trip.ObservedSeconds > 0 {
			out = append(out, trip)
		}
	}
	return out
}
func Stats(samples []Sample, now time.Time) Statistics {
	stats := Statistics{Periods: map[string]Period{}, Source: "stock-recording-nmea", Status: "unknown", Reason: "有効な連続測位データをまだ受信していません"}
	days := map[string]*Day{}
	ordered := ordered(samples)
	dayFor := func(at time.Time) *Day {
		key := at.In(Japan).Format("2006-01-02")
		d := days[key]
		if d == nil {
			d = &Day{Date: key, Reason: "no-continuous-position"}
			days[key] = d
		}
		return d
	}
	var previous *Sample
	for i := range ordered {
		s := ordered[i]
		if !Usable(s) {
			previous = nil
			continue
		}
		stats.ValidSamples++
		at := s.At
		stats.LastValidAt = &at
		dayFor(s.At).SampleCount++
		if previous != nil {
			distance, driving, seconds, ok := interval(*previous, s)
			if ok {
				start := previous.At
				for start.Before(s.At) {
					boundary := time.Date(start.In(Japan).Year(), start.In(Japan).Month(), start.In(Japan).Day()+1, 0, 0, 0, 0, Japan)
					end := s.At
					if boundary.Before(end) {
						end = boundary
					}
					fraction := end.Sub(start).Seconds() / seconds
					d := dayFor(start)
					if !d.Observed {
						d.Observed = true
						d.DistanceKM = ptr(0)
						d.DrivingSeconds = ptr(0)
						d.Reason = ""
					}
					*d.DistanceKM += distance * fraction
					*d.DrivingSeconds += driving * fraction
					d.ObservedSeconds += end.Sub(start).Seconds()
					start = end
				}
			}
		}
		previous = &ordered[i]
	}
	midnight := time.Date(now.In(Japan).Year(), now.In(Japan).Month(), now.In(Japan).Day(), 0, 0, 0, 0, Japan)
	for _, count := range []int{7, 30, 90, 365} {
		p := Period{Days: make([]Day, 0, count)}
		distance, driving := 0.0, 0.0
		for offset := count - 1; offset >= 0; offset-- {
			date := midnight.AddDate(0, 0, -offset).Format("2006-01-02")
			d, ok := days[date]
			if !ok {
				p.Days = append(p.Days, Day{Date: date, Reason: "no-position"})
				continue
			}
			p.Days = append(p.Days, *d)
			p.SampleCount += d.SampleCount
			if d.Observed {
				p.HasData = true
				p.ValidObservedDays++
				distance += *d.DistanceKM
				driving += *d.DrivingSeconds
			}
		}
		if p.HasData {
			p.DistanceKM = &distance
			p.DrivingSeconds = &driving
			stats.Status = "available"
			stats.Reason = ""
		}
		stats.Periods[strconvI(count)] = p
	}
	return stats
}
func strconvI(v int) string { return fmt.Sprint(v) }
