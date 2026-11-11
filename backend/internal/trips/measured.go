package trips

import (
	"errors"
	"time"
)

const MaximumMeasuredBatch = 5000

// MeasuredInterval contains derived quantities only. Coordinates, file names and
// device identifiers do not cross into the long-term journey aggregate store.
type MeasuredInterval struct {
	Start          time.Time `json:"start"`
	End            time.Time `json:"end"`
	DistanceKM     float64   `json:"distanceKm"`
	DrivingSeconds float64   `json:"drivingSeconds"`
}

type SourceHealth struct {
	Files           int       `json:"files"`
	UnreadableFiles int       `json:"unreadableFiles"`
	NoFix           int       `json:"noFix"`
	BadChecksum     int       `json:"badChecksum"`
	Malformed       int       `json:"malformed"`
	IndexedAt       time.Time `json:"indexedAt"`
	Truncated       bool      `json:"truncated"`
}

type MeasuredIntervalBatch struct {
	After     time.Time          `json:"after"`
	Next      time.Time          `json:"next"`
	More      bool               `json:"more"`
	Intervals []MeasuredInterval `json:"intervals"`
	Health    SourceHealth       `json:"health"`
}

// MeasuredBatch pages by measured interval end time, never by recording length.
// The collector periodically restarts its scan from zero so a late sidecar or a
// replaced SD card can fill previously unobserved intervals. Archive coverage,
// rather than this transport cursor, is the authority for duplicate detection.
func MeasuredBatch(snapshot Snapshot, after time.Time, limit int, now time.Time) (MeasuredIntervalBatch, error) {
	out := MeasuredIntervalBatch{After: after, Next: after, Intervals: []MeasuredInterval{}, Health: SourceHealth{
		Files: snapshot.Files, UnreadableFiles: snapshot.UnreadableFiles, NoFix: snapshot.NoFix,
		BadChecksum: snapshot.BadChecksum, Malformed: snapshot.Malformed, IndexedAt: snapshot.IndexedAt, Truncated: snapshot.Truncated,
	}}
	if limit < 1 || limit > MaximumMeasuredBatch || after.After(now) {
		return out, errors.New("invalid measured interval cursor or limit")
	}
	points := ordered(snapshot.Samples)
	for n := 1; n < len(points); n++ {
		a, b := points[n-1], points[n]
		if !b.At.After(after) || b.At.After(now) {
			continue
		}
		distance, driving, _, valid := interval(a, b)
		if !valid {
			continue
		}
		if len(out.Intervals) == limit {
			out.More = true
			break
		}
		out.Intervals = append(out.Intervals, MeasuredInterval{Start: a.At.UTC(), End: b.At.UTC(), DistanceKM: distance, DrivingSeconds: driving})
		out.Next = b.At.UTC()
	}
	return out, nil
}
