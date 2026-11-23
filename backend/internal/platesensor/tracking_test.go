package platesensor

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

func TestTrackAssignmentKeepsBothPlausibleTracks(t *testing.T) {
	at := time.Now()
	left, right := &plateTrack{}, &plateTrack{}
	for i, track := range []*plateTrack{left, right} {
		track.observe(at, at, EnginePlate{Box: [4]int{120 + 35*i, 220, 80, 40}}, "1", .6)
	}
	detections := []EnginePlate{
		{Box: [4]int{129, 220, 80, 40}}, // can move from either track
		{Box: [4]int{95, 220, 80, 40}},  // can move only from the left track
		{Box: [4]int{700, 220, 80, 40}},
	}
	if got := assignTracks([]*plateTrack{left, right}, at.Add(250*time.Millisecond), detections); !slices.Equal(got, []int{1, 0, -1}) {
		t.Fatalf("assignTracks = %v, want [1 0 -1]", got)
	}
}

func TestRejoinFastPassingPlateRequiresStrongEvidence(t *testing.T) {
	at := time.Now()
	for _, tc := range []struct {
		number     string
		confidence float64
		want       bool
	}{
		{"1234", .94, true},
		{"1234", .81, false},
		{"12", .94, false},
	} {
		first := &plateTrack{first: at}
		secondAt := at.Add(250 * time.Millisecond)
		second := &plateTrack{first: secondAt}
		reading := plate.Reading{Number: tc.number, Confidence: tc.confidence}
		first.observe(at, at, EnginePlate{Box: [4]int{994, 460, 55, 28}, Score: .8, Reading: reading}, "1", .6)
		second.observe(secondAt, secondAt, EnginePlate{Box: [4]int{1216, 474, 64, 31}, Score: .8, Reading: reading}, "2", .6)
		if got := first.canRejoin(second); got != tc.want {
			t.Fatalf("rejoin for %#v = %t, want %t", tc, got, tc.want)
		}
	}
}

func TestTrackingReReadsTwoPlatesAndKeepsDetectorTimes(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 11, 22, 15, 0, 0, 0, time.UTC)
	now := start
	s := New(setup(t, true, true), &fakeSource{}, nil, nil)
	s.SetClock(func() time.Time { return now })
	engine := &fakeEngine{}
	buf := make([]byte, FrameBytes+stampBytes)
	left := EnginePlate{Box: [4]int{120, 220, 80, 40}, Score: .9}
	right := EnginePlate{Box: [4]int{280, 220, 80, 40}, Score: .9,
		Reading: plate.Reading{Region: "横浜", Number: "1234", Confidence: .91}}
	for i, reading := range []plate.Reading{
		{}, // detector sees the plate before OCR can read it
		{Region: "千葉", Number: "1284", Confidence: .99},
		{Region: "品川", Class: "300", Number: "1234", Confidence: .8},
		{Region: "品川", Kana: "あ", Number: "1234", Confidence: .82},
		{Region: "品川", Class: "310", Kana: "あ", Number: "1234", Confidence: .77},
		{}, // detector still sees the plate after the last successful OCR
	} {
		now = start.Add(time.Duration(i) * 250 * time.Millisecond)
		left.Reading = reading
		left.Box[0] += 12
		right.Box[0] -= 10
		engine.plates = []EnginePlate{left, right}
		if i%2 == 1 {
			engine.plates = []EnginePlate{right, left}
		}
		if err := s.step(ctx, engine, buf); err != nil {
			t.Fatal(err)
		}
	}
	last := now
	s.finishTracks(now.Add(trackRejoinGap+trackReadGrace), false)
	s.flushPending(ctx, true)
	if engine.calls != 6 || len(s.queue) != 2 || len(s.tracks) != 0 {
		t.Fatalf("calls=%d queue=%+v active=%d", engine.calls, s.queue, len(s.tracks))
	}
	results := map[string]plate.Detection{}
	for _, d := range s.queue {
		results[d.Reading.Region] = d
		if d.Reading.Number != "1234" || !d.At.Equal(start) || !d.Track.LastSeen.Equal(last) || d.Track.Frames != 6 || d.Frame == "" {
			t.Fatalf("detection = %+v, span = %+v", d, d.Track)
		}
	}
	a, b := results["品川"], results["横浜"]
	if a.Track == nil || b.Track == nil || a.Track.ID == b.Track.ID || a.Track.Support != 3 || b.Track.Support != 6 ||
		a.Reading.Class != "300" || a.Reading.Kana != "あ" || b.Reading.Class != "" || a.Frame != "4" {
		t.Fatalf("left = %+v, right = %+v", a, b)
	}
}

func TestTrackingExpiryAndEarlyUpdates(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	s := New(setup(t, true, true), &fakeSource{}, nil, nil)
	s.SetClock(func() time.Time { return now })
	p := EnginePlate{Box: [4]int{120, 220, 80, 40}, Score: .9,
		Reading: plate.Reading{Number: "1234", Confidence: .91}}
	for _, frame := range []string{"1", "2", "3"} {
		now = now.Add(time.Second)
		// Time spent running OCR must not expire the track between requests.
		s.observeFrame(now, now.Add(2*time.Second), []EnginePlate{p}, frame, nil)
		now = now.Add(2 * time.Second)
		s.finishTracks(now, false)
	}
	s.flushPending(ctx, true)
	if len(s.queue) != 1 || s.queue[0].Track.Frames != 3 {
		t.Fatalf("early publication = %+v", s.queue)
	}
	now = now.Add(time.Second)
	s.observeFrame(now, now, []EnginePlate{p}, "4", nil)
	for i := range trackMaxMisses {
		s.observeFrame(now.Add(time.Duration(i+1)*200*time.Millisecond), now, nil, "", nil)
	}
	s.finishTracks(now.Add(trackRejoinGap), false)
	s.flushPending(ctx, true)
	if len(s.queue) != 2 || s.queue[1].Track.ID != s.queue[0].Track.ID || s.queue[1].Track.Frames != 4 || !s.queue[1].Track.LastSeen.Equal(now) {
		t.Fatalf("updated publication = %+v", s.queue)
	}
	now = now.Add(trackRejoinGap + time.Second)
	s.observeFrame(now, now, []EnginePlate{p}, "8", nil)
	s.finishTracks(now, true)
	s.flushPending(ctx, true)
	if len(s.queue) != 3 || s.queue[2].Track.ID == s.queue[0].Track.ID || s.queue[2].Track.Frames != 1 {
		t.Fatalf("reappearance = %+v", s.queue)
	}
}

type finiteTrackingSource struct{ fakeSource }

func (s *finiteTrackingSource) Next(buf []byte) ([2]int64, bool, error) {
	if s.n > 0 {
		return [2]int64{}, false, errEndOfReplay
	}
	return s.fakeSource.Next(buf)
}

func TestReplayEndPublishesLastTrackWithoutGPS(t *testing.T) {
	engine := &fakeEngine{plates: []EnginePlate{{Box: [4]int{120, 220, 80, 40},
		Reading: plate.Reading{Number: "1234", Confidence: .91}}}}
	now := time.Now()
	s := New(setup(t, true, true), &finiteTrackingSource{}, nil, func(context.Context) (Engine, error) {
		return engine, nil
	})
	s.SetClock(func() time.Time { return now })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s.Run(ctx)
	if ctx.Err() != nil || engine.calls != 1 || len(s.queue) != 1 || !s.queue[0].Track.LastSeen.Equal(now) {
		t.Fatalf("replay end: err=%v calls=%d queue=%+v", ctx.Err(), engine.calls, s.queue)
	}
}

func TestRejoinSeparatedSerialAndKeepOptionalFields(t *testing.T) {
	start := time.Now()
	s := New(Config{}, &fakeSource{}, nil, nil)
	observe := func(secs float64, id string, x int, reading plate.Reading) {
		at := start.Add(time.Duration(secs * float64(time.Second)))
		s.finishTracks(at, false)
		s.observeFrame(at, at, []EnginePlate{{Box: [4]int{x, 500, 48, 24}, Score: .8, Reading: reading}}, id, nil)
	}
	reading := plate.Reading{Number: "1234", Confidence: .90, Region: "品川", Kana: "あ"}
	observe(0, "1", 320, reading)
	// More than one plate width per frame exceeds the narrow geometry gate.
	observe(.25, "2", 445, plate.Reading{Number: "1234", Confidence: .88, Class: "300"})
	// A gap between fragments and one conflicting optional field.
	observe(3.75, "3", 545, plate.Reading{Number: "1234", Confidence: .86, Kana: "い"})
	for i := range trackOCRWindow {
		observe(4+float64(i)*.25, "4", 545, plate.Reading{Number: "1234", Confidence: .92})
	}
	s.finishTracks(start.Add(20*time.Second), true)
	s.flushPending(context.Background(), true)
	if len(s.queue) != 1 {
		t.Fatalf("want one cumulative detection, got %d", len(s.queue))
	}
	detection := s.queue[0]
	if detection.Track.Frames != trackOCRWindow+3 || detection.Track.Support != trackOCRWindow ||
		!detection.At.Equal(start) || detection.Reading.Number != "1234" || detection.Reading.Region != "品川" || detection.Reading.Class != "300" {
		t.Fatalf("joined evidence: %+v %+v", detection.Reading, detection.Track)
	}
}

func TestInvalidAndAmbiguousSerialsDoNotPublish(t *testing.T) {
	for _, readings := range [][]string{{"12?4", "12345", "????"}, {"1234", "1235", "1234", "1235"}} {
		start := time.Now()
		s := New(Config{}, &fakeSource{}, nil, nil)
		for i, number := range readings {
			at := start.Add(time.Duration(i) * 250 * time.Millisecond)
			s.observeFrame(at, at, []EnginePlate{{Box: [4]int{500, 500, 80, 40}, Score: .9,
				Reading: plate.Reading{Number: number, Confidence: .92}}}, "1", nil)
		}
		s.finishTracks(start.Add(trackRejoinGap), true)
		s.flushPending(context.Background(), true)
		if len(s.queue) != 0 {
			t.Fatalf("unstable or invalid numbers published: %+v", s.queue)
		}
	}
}

func TestLongTrackRejoinRequiresRepeatReadsAndStableGeometry(t *testing.T) {
	for _, tc := range []struct {
		name   string
		number string
		x      int
		reads  int
		want   int
	}{
		{"same plate", "1234", 920, 2, 1},
		{"different position", "1234", 1030, 2, 2},
		{"single read", "1234", 920, 1, 2},
		{"short serial without corroboration", "12", 920, 2, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			s := New(Config{}, &fakeSource{}, nil, nil)
			p := EnginePlate{Box: [4]int{910, 470, 65, 32}, Score: .9,
				Reading: plate.Reading{Number: tc.number, Region: "品川", Kana: "あ", Confidence: .92}}
			s.observeFrame(start, start, []EnginePlate{p}, "1", nil)
			last := start.Add(250 * time.Millisecond)
			s.observeFrame(last, last, []EnginePlate{p}, "2", nil)
			last = start.Add(8 * time.Second)
			s.finishTracks(last, false)
			s.flushPending(context.Background(), true)
			if len(s.queue) != 1 || len(s.tracks) != 0 {
				t.Fatalf("first fragment not published before reappearance: %+v", s.queue)
			}
			p.Box[0], p.Reading.Region, p.Reading.Kana = tc.x, "足立", "い"
			for _, frame := range []string{"3", "4"}[:tc.reads] {
				last = last.Add(250 * time.Millisecond)
				s.observeFrame(last, last, []EnginePlate{p}, frame, nil)
			}
			s.finishTracks(last, true)
			s.flushPending(context.Background(), true)
			if len(s.queue) != 2 {
				t.Fatalf("want two publications, got %+v", s.queue)
			}
			a, b := s.queue[0], s.queue[1]
			if tc.want == 1 && (a.Track.ID != b.Track.ID || !b.Track.Update || !b.At.Equal(start) || !b.Track.LastSeen.Equal(last) || b.Track.Frames != 4) {
				t.Fatalf("expected an update to the original interval: %+v %+v", a.Track, b.Track)
			}
			if tc.want == 2 && a.Track.ID == b.Track.ID {
				t.Fatalf("insufficient evidence to join: %+v %+v", a.Track, b.Track)
			}
		})
	}
}

func TestFastPassingMatchUsesOutwardMotionAndSerialEvidence(t *testing.T) {
	start := time.Now()
	track := &plateTrack{first: start}
	passing := EnginePlate{Box: [4]int{1340, 480, 45, 24}, Score: .8,
		Reading: plate.Reading{Number: "1234", Confidence: .9}}
	track.observe(start, start, passing, "1", .6)
	passing.Box[0] += 91
	at := start.Add(250 * time.Millisecond)
	if track.match(at, passing) <= 0 {
		t.Fatal("lost outward-moving plate")
	}
	passing.Reading.Number = "5678"
	if track.match(at, passing) > 0 {
		t.Fatal("attached a different high-confidence serial")
	}
	passing.Reading.Number = "1234"
	passing.Box[0] -= 182
	if track.match(at, passing) > 0 {
		t.Fatal("attached a plate moving toward the center")
	}
}
