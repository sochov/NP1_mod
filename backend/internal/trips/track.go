package trips

import (
	"errors"
	"time"
)

type TrackPoint struct {
	At       time.Time `json:"at"`
	Lat      float64   `json:"lat"`
	Lon      float64   `json:"lon"`
	Segment  int       `json:"segment"`
	SpeedMPS *float64  `json:"speedMps,omitempty"`
	Heading  *float64  `json:"heading,omitempty"`
}
type TrackSegment struct {
	ID             int       `json:"id"`
	Start          time.Time `json:"start"`
	End            time.Time `json:"end"`
	PointCount     int       `json:"pointCount"`
	DistanceKM     *float64  `json:"distanceKm"`
	DrivingSeconds *float64  `json:"drivingSeconds"`
}
type TrackReport struct {
	Date            string         `json:"date"`
	Points          []TrackPoint   `json:"points"`
	Segments        []TrackSegment `json:"segments"`
	Source          string         `json:"source"`
	SourceTruncated bool           `json:"sourceTruncated"`
	IndexedAt       time.Time      `json:"indexedAt"`
	Reason          string         `json:"reason,omitempty"`
}

func TrackDate(date string) (time.Time, error) {
	day, err := time.ParseInLocation("2006-01-02", date, Japan)
	if err != nil || day.Format("2006-01-02") != date {
		return time.Time{}, errors.New("INVALID_DATE")
	}
	return day, nil
}

// DayTrack exposes measured coordinates separately from aggregate statistics.
// Invalid fixes, missing intervals and unrealistic jumps never become lines.
func DayTrack(snapshot Snapshot, date string, now time.Time) (TrackReport, error) {
	out := TrackReport{Date: date, Points: []TrackPoint{}, Segments: []TrackSegment{}, Source: "stock-recording-nmea", SourceTruncated: snapshot.Truncated, IndexedAt: snapshot.IndexedAt}
	day, err := TrackDate(date)
	if err != nil {
		return out, err
	}
	end := day.AddDate(0, 0, 1)
	var previous *Sample
	for _, sample := range ordered(snapshot.Samples) {
		if sample.At.Before(day) || !sample.At.Before(end) || sample.At.After(now) || !Usable(sample) {
			previous = nil
			continue
		}
		var segment *TrackSegment
		if previous != nil {
			if distance, driving, _, ok := interval(*previous, sample); ok {
				segment = &out.Segments[len(out.Segments)-1]
				if segment.DistanceKM == nil {
					segment.DistanceKM = ptr(0)
					segment.DrivingSeconds = ptr(0)
				}
				*segment.DistanceKM += distance
				*segment.DrivingSeconds += driving
			}
		}
		if segment == nil {
			out.Segments = append(out.Segments, TrackSegment{ID: len(out.Segments), Start: sample.At.UTC()})
			segment = &out.Segments[len(out.Segments)-1]
		}
		segment.End = sample.At.UTC()
		segment.PointCount++
		out.Points = append(out.Points, TrackPoint{sample.At.UTC(), sample.Lat, sample.Lon, segment.ID, sample.SpeedMPS, sample.Heading})
		copy := sample
		previous = &copy
	}
	if len(out.Points) == 0 {
		out.Reason = "この日の録画に有効な位置記録はありません"
	}
	return out, nil
}
