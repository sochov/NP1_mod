package plate

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStoreTrackUpdatesAndSeparatesIdenticalSerials(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plate")
	s := NewStore(dir)
	settings := DefaultSettings()
	start := time.Date(2026, 11, 22, 14, 59, 59, 0, time.UTC)
	d := Detection{At: start, Box: [4]int{100, 100, 70, 40}, Frame: "first",
		Reading: Reading{Number: "1284", Confidence: .8},
		Track:   &TrackSpan{ID: "pt-test-left", Frames: 4, Support: 3, LastSeen: start.Add(time.Second)}}
	settings.Rules = []Rule{{ID: "test", Enabled: true, Number: "1234", SaveImage: true}}
	first, err := s.Ingest(d, settings, nil)
	if err != nil || !first.New {
		t.Fatalf("first = %+v, err = %v", first, err)
	}
	s = NewStore(dir) // web process restarted while the sensor kept tracking
	if retry, err := s.Ingest(d, settings, nil); err != nil || retry.Skipped != "stale-track" {
		t.Fatalf("retry after restart = %+v, err = %v", retry, err)
	}
	d.Reading = Reading{Region: "品川", Number: "1234", Confidence: .9}
	d.Track.LastSeen = start.Add(4 * time.Second)
	d.Track.Frames, d.Track.Support, d.Track.Update = 8, 5, true
	d.Frame = "best"
	d.Box[0] = 120
	updated, err := s.Ingest(d, settings, func(frame string) ([]byte, error) {
		if frame != "best" {
			t.Fatalf("image frame = %s", frame)
		}
		return []byte{0xff, 0xd8, 0xff, 0xd9}, nil
	})
	if err != nil || updated.New || updated.Record.ID != first.Record.ID || updated.Record.Reading != d.Reading || updated.Record.ImageID == "" || len(updated.NewMatches) != 1 {
		t.Fatalf("update = %+v, err = %v", updated, err)
	}
	if stale, err := s.Ingest(d, settings, nil); err != nil || stale.Skipped != "stale-track" {
		t.Fatalf("duplicate = %+v, err = %v", stale, err)
	}
	d.Reading.Region = "横浜"
	d.Box[0] = 390
	d.Track = &TrackSpan{ID: "pt-test-right", Frames: 4, Support: 4, LastSeen: start.Add(time.Second)}
	if second, err := s.Ingest(d, settings, nil); err != nil || !second.New || second.Record.ID == first.Record.ID {
		t.Fatalf("second = %+v, err = %v", second, err)
	}
	rows, err := NewStore(dir).ListAll(Query{From: Day(start), To: Day(start)})
	if err != nil || len(rows) != 2 || rows[1].Reading.Region != "品川" || !rows[1].LastSeen.Equal(start.Add(4*time.Second)) || rows[1].Sightings != 8 || !rows[1].FirstSeen.Equal(start) {
		t.Fatalf("stored rows = %+v, err = %v", rows, err)
	}
}

func TestStoreTrackConfirmationCountsOCRSupport(t *testing.T) {
	s := NewStore(t.TempDir())
	settings := DefaultSettings()
	at := time.Now()
	d := Detection{At: at, Box: [4]int{10, 10, 80, 40}, Reading: Reading{Number: "1", Confidence: .96},
		Track: &TrackSpan{ID: "pt-test-one", Frames: 4, Support: 1, Conflict: true, LastSeen: at.Add(time.Second)}}
	if outcome, err := s.Ingest(d, settings, nil); err != nil || outcome.Skipped != "provisional" {
		t.Fatalf("contradicted single OCR = %+v, err = %v", outcome, err)
	}
	d.Track.Conflict = false
	if outcome, err := s.Ingest(d, settings, nil); err != nil || !outcome.New || outcome.Record.Sightings != 4 {
		t.Fatalf("single OCR fast path = %+v, err = %v", outcome, err)
	}
}

func TestTrackSingleReadFloorAndCorrectedNumberClearsOldFields(t *testing.T) {
	s := NewStore(t.TempDir())
	settings := DefaultSettings()
	settings.InstantConfidence = .7 // settings persisted before the stricter floor
	at := time.Now()
	d := Detection{At: at, Box: [4]int{10, 10, 45, 25}, Reading: Reading{Number: "1234", Region: "品川", Confidence: .79},
		Track: &TrackSpan{ID: "pt-test-floor", Frames: 1, Support: 1, LastSeen: at}}
	if outcome, err := s.Ingest(d, settings, nil); err != nil || outcome.Skipped != "provisional" {
		t.Fatalf("single moderate read = %+v, err = %v", outcome, err)
	}
	d.Track.Frames, d.Track.Support = 2, 2
	if outcome, err := s.Ingest(d, settings, nil); err != nil || !outcome.New {
		t.Fatalf("repeated read = %+v, err = %v", outcome, err)
	}
	d.Reading.Region = ""
	d.Track.Frames++
	if outcome, err := s.Ingest(d, settings, nil); err != nil || outcome.Record.Reading.Region != "品川" {
		t.Fatalf("same serial loses stored field = %+v, err = %v", outcome, err)
	}
	d.Track.FieldsChecked = true
	d.Track.Frames++
	if outcome, err := s.Ingest(d, settings, nil); err != nil || outcome.Record.Reading.Region != "" {
		t.Fatalf("an inconclusive field was kept after a checked update = %+v, err = %v", outcome, err)
	}
	d.Reading.Number = "1235"
	d.Track.Frames++
	if outcome, err := s.Ingest(d, settings, nil); err != nil || outcome.Record.Reading.Number != "1235" || outcome.Record.Reading.Region != "" {
		t.Fatalf("old field carried into corrected serial = %+v, err = %v", outcome, err)
	}
}

func TestTrackedRecordNeedsEvidenceAtSizeExtremes(t *testing.T) {
	for _, c := range []struct {
		name       string
		width      int
		confidence float64
		support    int
		want       bool
	}{
		{"narrow repeated misread", 35, .74, 3, false},
		{"narrow clear repeat", 35, .84, 2, true},
		{"very narrow read repeated twice", 30, .94, 2, false},
		{"very narrow read repeated three times", 30, .94, 3, true},
		{"large sign in a single frame", 296, .95, 1, false},
		{"large repeated serial without other fields", 296, .95, 2, true},
		{"ordinary single serial without other fields", 70, .90, 1, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := NewStore(t.TempDir())
			at := time.Now()
			d := Detection{At: at, Box: [4]int{120, 440, c.width, 22}, Reading: Reading{Number: "1234", Confidence: c.confidence},
				Track: &TrackSpan{ID: "pt-size-check", Frames: c.support, Support: c.support, LastSeen: at}}
			settings := DefaultSettings()
			if c.width >= wideUnconfirmedWidth {
				settings.MinSightings = 1
			}
			outcome, err := s.Ingest(d, settings, nil)
			if err != nil || outcome.New != c.want {
				t.Fatalf("recorded=%t err=%v, want %t", outcome.New, err, c.want)
			}
		})
	}
}
