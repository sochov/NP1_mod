package platesensor

import (
	"context"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

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
	s.finishTracks(now.Add(trackGap), false)
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
	s.flushPending(ctx, true)
	if len(s.queue) != 2 || s.queue[1].Track.ID != s.queue[0].Track.ID || s.queue[1].Track.Frames != 4 || !s.queue[1].Track.LastSeen.Equal(now) {
		t.Fatalf("updated publication = %+v", s.queue)
	}
	now = now.Add(time.Second)
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
