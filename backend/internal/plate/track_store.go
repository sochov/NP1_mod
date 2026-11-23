package plate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	narrowRepeatWidth      = 40
	narrowRepeatConfidence = .78
	wideUnconfirmedWidth   = 160
)

// ingestTrack replaces the OCR consensus for one track in place. Unlike a
// legacy detection, this snapshot already combines independent frame reads.
func (s *Store) ingestTrack(d Detection, settings Settings, frameImage func(string) ([]byte, error)) (Outcome, error) {
	span := d.Track
	if !recordIDPat.MatchString(span.ID) || !strings.HasPrefix(span.ID, "pt-") || d.At.IsZero() ||
		span.LastSeen.Before(d.At) || span.Support < 1 || span.Frames < span.Support {
		return Outcome{Skipped: "invalid-track"}, nil
	}
	reading := Normalize(d.Reading)
	threshold := DefaultInstantConfidence
	if d.Box[2] < instantWideWidth {
		threshold = instantNarrowConfidence
	}
	// Narrow crops can repeat the same character error. Very large detections
	// without any plate fields need a second frame to distinguish signs and rails.
	minimum := settings.MinConfidence
	if d.Box[2] > 0 && d.Box[2] < narrowRepeatWidth {
		minimum = max(minimum, narrowRepeatConfidence)
	}
	wideUnconfirmed := d.Box[2] >= wideUnconfirmedWidth && reading.Region == "" && reading.Class == "" && reading.Kana == ""
	minSightings := max(1, settings.MinSightings)
	if wideUnconfirmed {
		minSightings = max(2, minSightings)
	}
	instant := !span.Conflict && settings.InstantConfidence > 0 && settings.InstantConfidence < 1 &&
		reading.Confidence >= max(threshold, settings.InstantConfidence) && d.Box[2] >= InstantMinWidth && !wideUnconfirmed
	qualified := !span.Ambiguous && reading.Readable() && reading.Confidence >= minimum &&
		(span.Support >= minSightings || instant)

	s.mu.Lock()
	defer s.mu.Unlock()
	closeErr := s.closeExpired(span.LastSeen, time.Duration(settings.MergeSeconds)*time.Second)
	r := s.open[span.ID]
	if r == nil && (span.Update || !d.At.After(s.started)) {
		// Recover older snapshots and retries after a restart. A newly started
		// track in this process needs no scan of the day's history.
		byID := map[string]Record{}
		if err := readDay(filepath.Join(s.recordsDir(), Day(d.At)+".jsonl"), byID); err != nil && !errors.Is(err, os.ErrNotExist) {
			return Outcome{}, err
		}
		if saved, ok := byID[span.ID]; ok {
			r = &saved
		}
	}
	isNew := r == nil
	if isNew {
		if !qualified {
			return Outcome{Skipped: "provisional"}, closeErr
		}
		r = &Record{ID: span.ID, FirstSeen: d.At, Matches: []Match{}}
	} else if span.Frames <= r.Sightings || span.LastSeen.Before(r.LastSeen) {
		return Outcome{Record: *r, Skipped: "stale-track"}, closeErr
	}
	// Work on a copy so a failed append is safe to retry with the same snapshot.
	next := *r
	next.Matches = append(make([]Match, 0, len(r.Matches)), r.Matches...)
	next.LastSeen, next.Sightings = span.LastSeen, span.Frames
	if next.Location == nil {
		next.Location = d.Location
	}
	var added []Match
	if qualified {
		if reading.Number == next.Reading.Number {
			if reading.Region == "" {
				reading.Region = next.Reading.Region
			}
			if reading.Class == "" {
				reading.Class = next.Reading.Class
			}
			if reading.Kana == "" {
				reading.Kana = next.Reading.Kana
			}
		}
		next.Reading, next.Display = reading, reading.Display()
		if next.ImageID == "" {
			next.Box = d.Box
		}
		added = s.applyRules(&next, settings, d.Frame, frameImage)
	}
	if err := s.appendRecord(next); err != nil {
		return Outcome{Skipped: "storage-unavailable"}, err
	}
	s.open[next.ID] = &next
	return Outcome{Record: next, New: isNew, NewMatches: added}, closeErr
}
