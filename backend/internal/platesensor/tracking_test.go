package platesensor

import (
	"context"
	"slices"
	"strconv"
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

func TestOverlappingDetectionsKeepDistinctStrongReadings(t *testing.T) {
	a := EnginePlate{Box: [4]int{940, 500, 60, 30}, Score: .63,
		Reading: plate.Reading{Number: "1234", Confidence: .93}}
	b := EnginePlate{Box: [4]int{946, 510, 60, 32}, Score: .43,
		Reading: plate.Reading{Number: "1234", Confidence: .87}}
	if got, tracks := collapseOverlappingDetections([]EnginePlate{a, b}, []int{-1, 0}); len(got) != 1 || got[0].Box != a.Box || tracks[0] != 0 {
		t.Fatalf("duplicate crop was retained: %+v", got)
	}
	if got, _ := collapseOverlappingDetections([]EnginePlate{a, b}, []int{0, 1}); len(got) != 2 {
		t.Fatalf("separate tracks with the same serial were suppressed: %+v", got)
	}
	b.Reading.Number = "5678"
	if got, _ := collapseOverlappingDetections([]EnginePlate{a, b}, []int{-1, -1}); len(got) != 2 {
		t.Fatalf("distinct strong reading was suppressed: %+v", got)
	}
}

func TestOverlappingDetectionsDropWeakOffsetCropOfMatchedPlate(t *testing.T) {
	matched := EnginePlate{Box: [4]int{1388, 560, 45, 24}, Score: .70,
		Reading: plate.Reading{Number: "1234", Confidence: .87}}
	offset := EnginePlate{Box: [4]int{1390, 552, 40, 21}, Score: .54,
		Reading: plate.Reading{Number: "5678", Confidence: .26}}
	if got, tracks := collapseOverlappingDetections([]EnginePlate{matched, offset}, []int{0, -1}); len(got) != 1 || tracks[0] != 0 {
		t.Fatalf("weak offset crop formed a second track: %+v", got)
	}
	if got, _ := collapseOverlappingDetections([]EnginePlate{matched, offset}, []int{0, 1}); len(got) != 2 {
		t.Fatal("suppressed an offset crop that matched a different track")
	}
	offset.Reading.Confidence = .88
	if got, _ := collapseOverlappingDetections([]EnginePlate{matched, offset}, []int{0, -1}); len(got) != 2 {
		t.Fatal("suppressed a distinct confident number")
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

func TestRejoinAcceleratingSameNumberUsesMeasuredMotion(t *testing.T) {
	start := time.Now()
	p := EnginePlate{Score: .8, Reading: plate.Reading{Number: "1234", Confidence: .81}}
	earlier := &plateTrack{first: start}
	for i, b := range [][4]int{{828, 529, 45, 24}, {925, 531, 55, 27}, {1071, 535, 55, 30}} {
		at := start.Add(time.Duration(i) * 250 * time.Millisecond)
		p.Box = b
		earlier.observe(at, at, p, "1", .60)
	}
	at := start.Add(750 * time.Millisecond)
	later := &plateTrack{first: at}
	p.Box, p.Reading.Confidence = [4]int{1255, 549, 60, 33}, .76
	later.observe(at, at, p, "4", .60)
	if !earlier.canRejoin(later) {
		t.Fatal("split an accelerating plate with three earlier reads")
	}
	earlier.velocity[0] = -earlier.velocity[0]
	if earlier.canRejoin(later) {
		t.Fatal("joined a fragment moving against the measured trajectory")
	}
}

func TestRejoinFastThreeDigitPlateUsesIndependentAndOptionalEvidence(t *testing.T) {
	start := time.Now()
	for _, tc := range []struct {
		name, kana string
		score      float64
		want       bool
	}{
		{"independently verified", "あ", .99, true},
		{"optional fields disagree", "い", .99, false},
		{"second crop lacks independent evidence", "あ", .64, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reading := plate.Reading{Number: "123", Kana: "あ", Confidence: .94}
			p := EnginePlate{Score: .8, Reading: reading, Verifier: &reading, SequenceNumber: "123",
				NumberScores: map[string]float64{"123": .99}}
			first := &plateTrack{first: start}
			for i, box := range [][4]int{{1240, 515, 45, 29}, {1330, 542, 45, 29}} {
				at := start.Add(time.Duration(i) * 125 * time.Millisecond)
				p.Box = box
				first.observe(at, at, p, "earlier", .60)
			}
			at := start.Add(250 * time.Millisecond)
			next := &plateTrack{first: at}
			reading.Kana = tc.kana
			p.Box, p.Reading = [4]int{1438, 573, 50, 35}, reading
			p.NumberScores = map[string]float64{"123": tc.score}
			next.observe(at, at, p, "later", .60)

			if joined := first.canRejoin(next); joined != tc.want {
				t.Fatalf("rejoin = %v, want %v", joined, tc.want)
			}
		})
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
		a.Reading.Class != "" || a.Reading.Kana != "あ" || b.Reading.Class != "" || a.Frame != "4" {
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
		s.observeFrame(at, at, []EnginePlate{{Box: [4]int{x, 500, 72, 36}, Score: .8, Reading: reading}}, id, nil)
	}
	reading := plate.Reading{Number: "1234", Confidence: .90, Region: "品川", Class: "300", Kana: "あ"}
	observe(0, "1", 320, reading)
	// More than one plate width per frame exceeds the narrow geometry gate.
	observe(.25, "2", 445, plate.Reading{Number: "1234", Confidence: .88})
	// A gap between fragments and one conflicting optional field.
	observe(3.75, "3", 545, plate.Reading{Number: "1234", Confidence: .86, Region: "品川", Class: "300", Kana: "い"})
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
		!detection.At.Equal(start) || detection.Reading.Number != "1234" || detection.Reading.Region != "品川" ||
		detection.Reading.Class != "300" || detection.Reading.Kana != "" {
		t.Fatalf("joined evidence: %+v %+v", detection.Reading, detection.Track)
	}
}

func TestOptionalFieldsUseConfiguredWidthAndShare(t *testing.T) {
	start := time.Now()
	track := &plateTrack{first: start, fieldPolicy: fieldPolicy{MinWidth: 85, MinShare: .75}}
	for i, width := range []int{78, 78, 85, 85, 85} {
		at := start.Add(time.Duration(i) * 250 * time.Millisecond)
		reading := plate.Reading{Number: "1234", Confidence: .90, Region: "品川", Kana: "あ"}
		if i == 4 {
			reading.Region, reading.Kana = "横浜", ""
		}
		track.observe(at, at, EnginePlate{Box: [4]int{500, 500, width, 40}, Score: .9, Reading: reading}, "", .6)
	}
	d, ok := track.detection(true)
	if !ok || !d.Track.FieldsChecked || d.Reading.Number != "1234" || d.Reading.Region != "" || d.Reading.Kana != "あ" {
		t.Fatalf("optional field evidence = %+v", d)
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

func TestStableSmallPlateRequiresSpacedMatchingReads(t *testing.T) {
	const number = "1234"
	start := time.Unix(100, 0)
	edges := .32
	first := EnginePlate{Box: [4]int{1758, 581, 35, 31}, Score: .46,
		Reading:    plate.Reading{Number: number, Confidence: .75},
		Verifier:   &plate.Reading{Number: number, Confidence: .79},
		DigitEdges: &edges, SequenceNumber: number, NumberScores: map[string]float64{number: .988}}
	later := EnginePlate{Box: [4]int{1758, 576, 36, 35}, Score: .35,
		Reading:    plate.Reading{Number: number, Confidence: .87},
		Verifier:   &plate.Reading{Number: number, Confidence: .85},
		Localized:  &plate.Reading{Number: number, Confidence: .83},
		DigitEdges: &edges, SequenceNumber: number, NumberScores: map[string]float64{number: .992}}
	for _, tc := range []struct {
		name  string
		alter func(*EnginePlate, *EnginePlate)
		short bool
		reuse bool
		saved bool
	}{
		{name: "separate readable frames on the same trajectory", saved: true},
		{name: "frames too close in time", short: true},
		{name: "same image used twice", reuse: true},
		{name: "later box in a different position", alter: func(_, b *EnginePlate) { b.Box[0] += 28 }},
		{name: "independent digit read disagrees", alter: func(_, b *EnginePlate) {
			b.Localized = &plate.Reading{Number: "1239", Confidence: .84}
		}},
		{name: "older numeric verifier disagrees", alter: func(a, _ *EnginePlate) {
			a.Verifier = &plate.Reading{Number: "1239", Confidence: .79}
		}},
		{name: "small frame text near the bottom", alter: func(a, b *EnginePlate) {
			a.Box[1], b.Box[1] = 995, 997
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := first, later
			if tc.alter != nil {
				tc.alter(&a, &b)
			}
			track := &plateTrack{first: start}
			track.observe(start, start, a, "first", .60)
			for i := 1; i <= 20; i++ {
				at := start.Add(time.Duration(i) * 125 * time.Millisecond)
				track.observe(at, at, EnginePlate{Box: a.Box}, strconv.Itoa(i), .60)
			}
			end := start.Add(4 * time.Second)
			if tc.short {
				end = start.Add(2750 * time.Millisecond)
			}
			id := "later"
			if tc.reuse {
				id = "first"
			}
			track.observe(end, end, b, id, .60)
			d, saved := track.detection(true)
			if saved != tc.saved {
				t.Fatalf("saved = %t, want %t", saved, tc.saved)
			}
			if saved && (d.Reading.Number != number || d.At != start || d.Track.LastSeen != end || d.Track.Frames != 22) {
				t.Fatalf("unexpected stored track: %+v", d)
			}
			if saved {
				b.Box[0] += 28
				movedAt := end.Add(250 * time.Millisecond)
				track.observe(movedAt, movedAt, b, "after-movement", .60)
				if !track.numberVerified() {
					t.Fatal("later movement discarded the earlier corroborated reading")
				}
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

func TestMissedFramesRespectElapsedTimeAtHigherFrameRate(t *testing.T) {
	start := time.Now()
	track := &plateTrack{first: start}
	p := EnginePlate{Box: [4]int{910, 480, 64, 32}, Score: .8}
	track.observe(start, start, p, "1", .6)
	track.misses = trackMaxMisses
	if track.match(start.Add(500*time.Millisecond), p) <= 0 {
		t.Fatal("short outage split a higher-rate track")
	}
	if track.match(start.Add(time.Second), p) > 0 {
		t.Fatal("expired the track too late")
	}
}

func TestTentativeTrackDoesNotFollowNextVehicleAfterGap(t *testing.T) {
	at := time.Now()
	first := EnginePlate{Box: [4]int{1450, 580, 45, 30}, Score: .8,
		Reading: plate.Reading{Number: "123", Confidence: .85}}
	track := &plateTrack{first: at}
	track.observe(at, at, first, "1", .6)
	track.misses = 4
	other := EnginePlate{Box: [4]int{1433, 589, 50, 42}, Score: .6,
		Reading: plate.Reading{Number: "678", Confidence: .39}}
	next := at.Add(625 * time.Millisecond)
	if track.match(next, other) != 0 {
		t.Fatal("extended the single-frame track onto the following vehicle")
	}
	other.Reading = first.Reading
	if track.match(next, other) <= 0 {
		t.Fatal("split a matching strong serial after a brief outage")
	}
	first.Box[0] += 5
	first.Reading = plate.Reading{}
	if track.match(next, first) <= 0 {
		t.Fatal("split a nearly stationary plate with an unreadable frame")
	}
}

func TestProjectedPassContinuesEstablishedTrajectory(t *testing.T) {
	at := time.Now()
	track := &plateTrack{
		last: at, finished: at, box: [4]int{299, 471, 55, 33}, velocity: [2]float64{-400, 20},
		frames: 11, support: 3, best: observation{reading: plate.Reading{Number: "1234", Confidence: .91}},
	}
	p := EnginePlate{Box: [4]int{131, 487, 50, 42}, Reading: plate.Reading{Number: "2234", Confidence: .82}}
	next := at.Add(250 * time.Millisecond)
	if track.fastPassingMatch(.25, p) != 0 || track.match(next, p) <= 0 {
		t.Fatal("did not continue the fast-moving track across one missed frame")
	}
	for _, number := range []string{"6253", "523"} {
		other := p
		other.Reading.Number = number
		if track.match(next, other) > 0 {
			t.Fatalf("attached a distant detection with unrelated serial %q", number)
		}
	}
	p.Reading.Confidence = .94
	if track.match(next, p) > 0 || track.projectedPass(.5, p) > 0 {
		t.Fatal("accepted an uncorroborated confident mismatch or an excessive gap")
	}
}

func TestInwardCorrectionUsesTwoFramesAndMeasuredMotion(t *testing.T) {
	at := time.Now()
	reading := plate.Reading{Number: "1234", Confidence: .90}
	last := EnginePlate{Box: [4]int{389, 480, 53, 39}, Reading: reading, Verifier: &reading,
		SequenceNumber: reading.Number, NumberScores: map[string]float64{reading.Number: .99}}
	next := last
	next.Box = [4]int{443, 476, 51, 33}
	track := plateTrack{
		last: at, finished: at, box: last.Box, velocity: [2]float64{650, -100}, frames: 3, support: 2,
		best:         observation{reading: plate.Reading{Number: "1237", Confidence: .92}},
		recentFrames: []trackFrame{{at: at, plate: last}},
	}
	stamp := at.Add(125 * time.Millisecond)
	if got := assignTracks([]*plateTrack{&track}, stamp, []EnginePlate{next}); !slices.Equal(got, []int{0}) {
		t.Fatalf("split the approaching continuation: assignments %v", got)
	}
	for _, name := range []string{"weaker current read", "missing previous verifier", "another lane", "already published"} {
		t.Run(name, func(t *testing.T) {
			prior, detection := track, next
			prior.recentFrames = slices.Clone(track.recentFrames)
			switch name {
			case "weaker current read":
				detection.SequenceNumber = ""
			case "missing previous verifier":
				prior.recentFrames[0].plate.Verifier = nil
			case "another lane":
				detection.Box[1] += 45
			case "already published":
				prior.sent = 3
			}
			if score := prior.match(stamp, detection); score != 0 {
				t.Fatalf("attached the detection with score %.2f", score)
			}
		})
	}
	other := plateTrack{last: at, finished: at, box: next.Box, frames: 3}
	if got := assignTracks([]*plateTrack{&track, &other}, stamp, []EnginePlate{next}); !slices.Equal(got, []int{1}) {
		t.Fatalf("a stronger nearby track lost its detection: assignments %v", got)
	}
}

func TestWiderReadDefersUncertainPassingPlate(t *testing.T) {
	for _, tc := range []struct {
		name          string
		first, second [4]int
		gap           time.Duration
		joined        bool
	}{
		{"missed intermediate frame", [4]int{1227, 527, 36, 22}, [4]int{1270, 539, 36, 22}, 250 * time.Millisecond, false},
		{"joined pair", [4]int{1270, 539, 36, 22}, [4]int{1317, 555, 46, 27}, 125 * time.Millisecond, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Now()
			strong := plate.Reading{Number: "1234", Confidence: .91}
			short := EnginePlate{Box: tc.first, Score: .60, Reading: strong, Verifier: &strong, Localized: &strong,
				SequenceNumber: strong.Number, NumberScores: map[string]float64{strong.Number: .99}}
			track := &plateTrack{first: at}
			track.observe(at, at, short, "1", .60)
			at = at.Add(125 * time.Millisecond)
			short.Box = tc.second
			if tc.joined {
				fragment := &plateTrack{first: at}
				fragment.observe(at, at, short, "2", .60)
				track.join(fragment)
			} else {
				track.observe(at, at, short, "2", .60)
			}
			if !track.numberVerified() {
				t.Fatal("the initial reading should be verified before the wider frame")
			}
			weak := plate.Reading{Number: "1237", Confidence: .44}
			localized := plate.Reading{Number: weak.Number, Confidence: .89}
			wide := EnginePlate{Box: [4]int{1416, 584, 56, 32}, Score: .64, Reading: weak, Verifier: &weak, Localized: &localized}
			stamp := at.Add(tc.gap)
			other := wide
			other.Box[1] += 45
			if score := track.match(stamp, other); score != 0 {
				t.Fatalf("attached a detection off the trajectory: score %.2f", score)
			}
			other = wide
			other.Localized = nil
			if score := track.match(stamp, other); score != 0 {
				t.Fatalf("attached a weak detection without a separate digit read: score %.2f", score)
			}
			if got := assignTracks([]*plateTrack{track}, stamp, []EnginePlate{wide}); !slices.Equal(got, []int{0}) {
				t.Fatalf("split the wider continuation: assignments %v", got)
			}
			track.observe(stamp, stamp, wide, "3", .60)
			if track.best.reading.Number != strong.Number || track.support != 2 {
				t.Fatal("the weak wider reading altered the votes")
			}
			if _, saved := track.detection(true); saved {
				t.Fatal("saved an unresolved serial")
			}
		})
	}
}

func TestUncertainGeometryNeedsAWholePlateOrAnotherRead(t *testing.T) {
	start := time.Now()
	p := EnginePlate{Box: [4]int{-5, 480, 60, 36}, Score: .9,
		Reading: plate.Reading{Number: "9876", Confidence: .99}}
	track := &plateTrack{first: start}
	track.observe(start, start, p, "1", .6)
	if _, ok := track.detection(true); ok {
		t.Fatal("saved the serial from a clipped detection")
	}
	p.Box, p.Score = [4]int{10, 480, 151, 90}, .26
	at := start.Add(time.Second)
	track.observe(at, at, p, "2", .6)
	if _, ok := track.detection(true); ok {
		t.Fatal("saved a large low-score detection from a single read")
	}
	at = at.Add(time.Second)
	track.observe(at, at, p, "3", .6)
	d, ok := track.detection(true)
	if !ok || d.Track.Support != 2 || d.Track.Frames != 3 || !d.At.Equal(start) || !d.Track.LastSeen.Equal(at) {
		t.Fatalf("a confirmed read lost its full interval: %+v", d)
	}
}

func TestUnusualProportionsNeedAnotherReadExceptNearFrameEdge(t *testing.T) {
	for _, tc := range []struct {
		box  [4]int
		want bool
	}{
		{[4]int{1480, 500, 70, 76}, false},
		{[4]int{1840, 500, 50, 56}, true},
		{[4]int{1400, 380, 110, 40}, false},
	} {
		at := time.Now()
		track := &plateTrack{first: at}
		p := EnginePlate{Box: tc.box, Score: .85, Reading: plate.Reading{Number: "815", Confidence: .91}}
		track.observe(at, at, p, "1", .6)
		if _, got := track.detection(true); got != tc.want {
			t.Fatalf("first frame with box %v: got %v, want %v", tc.box, got, tc.want)
		}
		at = at.Add(250 * time.Millisecond)
		track.observe(at, at, p, "2", .6)
		if _, got := track.detection(true); !got {
			t.Fatalf("repeated read with box %v did not qualify", tc.box)
		}
	}
}

func TestSerialConsensusKeepsEarlierStrongReads(t *testing.T) {
	start := time.Now()
	track := &plateTrack{first: start}
	p := EnginePlate{Box: [4]int{910, 480, 64, 32}, Score: .8,
		Reading: plate.Reading{Number: "1234", Confidence: .94}}
	for i := range 20 {
		at := start.Add(time.Duration(i) * 250 * time.Millisecond)
		track.observe(at, at, p, "clear", .6)
	}
	p.Box, p.Score = [4]int{918, 481, 44, 24}, .5
	p.Reading = plate.Reading{Number: "1284", Confidence: .86}
	for i := range trackOCRWindow {
		at := start.Add(time.Duration(i+20) * 250 * time.Millisecond)
		track.observe(at, at, p, "blurred", .6)
	}
	d, ok := track.detection(true)
	if !ok || d.Reading.Number != "1234" || d.Frame != "clear" || d.Track.Frames != 20+trackOCRWindow {
		t.Fatalf("lost earlier strong readings: %+v", d)
	}
}

func TestShortReadRequiresVisibleWidthAndIndependentEvidence(t *testing.T) {
	at := time.Now()
	for _, tc := range []struct {
		name string
		w    int
		alt  string
		want bool
	}{
		{"consistent readable crop", 45, "1234", true},
		{"small crop", 40, "1234", false},
		{"independent disagreement", 45, "1284", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := EnginePlate{Box: [4]int{800, 510, tc.w, 26}, Score: .64,
				Reading:        plate.Reading{Number: "1234", Confidence: .91},
				Verifier:       &plate.Reading{Number: tc.alt, Confidence: .68},
				Localized:      &plate.Reading{Number: "1234", Confidence: .91},
				SequenceNumber: "1234", NumberScores: map[string]float64{"1234": .98}}
			track := &plateTrack{first: at}
			track.observe(at, at, p, "1", .6)
			if _, ok := track.detection(true); ok != tc.want {
				t.Fatalf("stored single frame = %v, want %v", ok, tc.want)
			}
		})
	}
}

func TestVerifierHeadsRequireCorroborationAcrossReaders(t *testing.T) {
	at := time.Now()
	for _, tc := range []struct {
		name      string
		localized *plate.Reading
		want      bool
	}{
		{"shared model only", nil, false},
		{"digit crop agrees", &plate.Reading{Number: "1234", Confidence: .89}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			track := &plateTrack{first: at}
			p := EnginePlate{Box: [4]int{820, 510, 50, 30}, Score: .54,
				Reading:        plate.Reading{Number: "1234", Confidence: .51},
				Verifier:       &plate.Reading{Number: "1234", Confidence: .92},
				Localized:      tc.localized,
				SequenceNumber: "1234", NumberScores: map[string]float64{"1234": .99}}
			for i := range 9 {
				seen := at.Add(time.Duration(i) * 125 * time.Millisecond)
				track.observe(seen, seen, p, "1", .60)
			}
			if _, ok := track.detection(true); ok != tc.want {
				t.Fatalf("stored serial = %v, want %v", ok, tc.want)
			}
		})
	}
}

func TestNarrowPlateNeedsTwoFramesWithIndependentAgreement(t *testing.T) {
	at := time.Now()
	for _, tc := range []struct {
		name      string
		frames    int
		localized string
		want      bool
	}{
		{"one frame", 1, "1234", false},
		{"local reader disagrees", 2, "1284", false},
		{"two frames agree", 2, "1234", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := EnginePlate{Box: [4]int{800, 510, 40, 23}, Score: .38,
				Reading:        plate.Reading{Number: "1234", Confidence: .72},
				Verifier:       &plate.Reading{Number: "1234", Confidence: .78},
				Localized:      &plate.Reading{Number: tc.localized, Confidence: .89},
				SequenceNumber: "1234", NumberScores: map[string]float64{"1234": .97}}
			track := &plateTrack{first: at}
			for i := range tc.frames {
				seen := at.Add(time.Duration(i) * 125 * time.Millisecond)
				track.observe(seen, seen, p, "crop", .60)
			}
			d, ok := track.detection(true)
			if ok != tc.want || ok && (d.Reading.Number != "1234" || d.Track.Frames != 2) {
				t.Fatalf("stored narrow crop = %v, record = %+v", ok, d)
			}
		})
	}
}

func TestForeshortenedBestReadNeedsTwoDistinctWellShapedViews(t *testing.T) {
	start := time.Unix(100, 0)
	const number = "1234"
	tall := verifiedPlateForRejoin(number, [4]int{1580, 510, 39, 36})
	tall.Localized = &plate.Reading{Number: number, Confidence: .91}
	clear := verifiedPlateForRejoin(number, [4]int{1581, 510, 39, 28})
	clear.Reading.Confidence = .83
	clear.Verifier.Confidence = .76
	clear.Localized = &plate.Reading{Number: number, Confidence: .87}
	clear.NumberScores[number] = .987
	for _, tc := range []struct {
		name   string
		frames []string
		want   bool
	}{
		{name: "one supporting image", frames: []string{"clear-1"}},
		{name: "same image again", frames: []string{"clear-1", "clear-1"}},
		{name: "two distinct supporting images", frames: []string{"clear-1", "clear-2"}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			track := &plateTrack{first: start}
			track.observe(start, start, tall, "tall", .60)
			for i, frame := range tc.frames {
				at := start.Add(time.Duration(i+1) * 125 * time.Millisecond)
				track.observe(at, at, clear, frame, .60)
			}
			d, saved := track.detection(true)
			if saved != tc.want {
				t.Fatalf("saved = %v, want %v", saved, tc.want)
			}
			if saved && (d.Box != clear.Box || d.Frame != "clear-1" || d.Reading.Number != number) {
				t.Fatalf("wrong representative detection: %+v", d)
			}
		})
	}
}

func TestSmallPlateNeedsThreeCorroboratedFramesWithTwoWideViews(t *testing.T) {
	start := time.Unix(100, 0)
	for _, tc := range []struct {
		name           string
		number         string
		heights        []int
		weakLastRead   bool
		lowOneReader   bool
		localizedRival bool
		wideThinCrop   bool
		want           bool
	}{
		{name: "three agreeing frames", number: "1234", heights: []int{28, 36, 22}, want: true},
		{name: "two frames", number: "1234", heights: []int{28, 22}},
		{name: "one wider view", number: "1234", heights: []int{28, 36, 36}},
		{name: "weak corroboration", number: "1234", heights: []int{28, 36, 22}, weakLastRead: true},
		{name: "weak reader despite strong partner", number: "1234", heights: []int{28, 36, 22}, lowOneReader: true},
		{name: "localized disagreement", number: "1234", heights: []int{28, 36, 22}, localizedRival: true},
		{name: "wide thin representative", number: "1234", heights: []int{28, 36, 22}, wideThinCrop: true},
		{name: "unproven short number", number: "234", heights: []int{28, 36, 22}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			track := &plateTrack{first: start}
			for i, h := range tc.heights {
				p := EnginePlate{Box: [4]int{800, 510, 39, h}, Score: .49,
					Reading:        plate.Reading{Number: tc.number, Confidence: .88},
					Verifier:       &plate.Reading{Number: tc.number, Confidence: .80},
					SequenceNumber: tc.number, NumberScores: map[string]float64{tc.number: .99}}
				if i == len(tc.heights)-1 {
					switch {
					case tc.weakLastRead:
						p.Reading.Confidence, p.Verifier.Confidence = .79, .79
					case tc.lowOneReader:
						p.Reading.Confidence, p.Verifier.Confidence = .99, .74
					default:
						p.Reading.Confidence, p.Verifier.Confidence = .896, .759
					}
				}
				seen := start.Add(time.Duration(i) * 250 * time.Millisecond)
				track.observe(seen, seen, p, strconv.Itoa(i), .60)
			}
			if tc.localizedRival {
				seen := start.Add(time.Second)
				p := EnginePlate{Box: [4]int{800, 510, 42, 23}, Score: .49,
					Localized: &plate.Reading{Number: "1284", Confidence: .94}}
				track.observe(seen, seen, p, "rival", .60)
			}
			if tc.wideThinCrop {
				seen := start.Add(time.Second)
				p := EnginePlate{Box: [4]int{800, 510, 90, 25}, Score: .49,
					Reading:        plate.Reading{Number: tc.number, Confidence: .92},
					Verifier:       &plate.Reading{Number: tc.number, Confidence: .83},
					SequenceNumber: tc.number, NumberScores: map[string]float64{tc.number: .99}}
				track.observe(seen, seen, p, "wide", .60)
			}
			d, ok := track.detection(true)
			if ok != tc.want {
				t.Fatalf("saved small plate = %v, want %v", ok, tc.want)
			}
			if ok && (d.Reading.Number != tc.number || d.At != start || d.Track.LastSeen != start.Add(500*time.Millisecond)) {
				t.Fatalf("unexpected reading or time range: %+v", d)
			}
		})
	}
}

func TestForeshortenedPlateNeedsWiderViewsOfSameNumber(t *testing.T) {
	at := time.Unix(100, 0)
	for _, tc := range []struct {
		name       string
		widerReads []string
		wantSave   bool
	}{
		{name: "only distorted crops"},
		{name: "one wider crop", widerReads: []string{"1234"}},
		{name: "one wider crop for each number", widerReads: []string{"1234", "1284"}},
		{name: "two wider crops for the same number", widerReads: []string{"1234", "1234"}, wantSave: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			track := &plateTrack{first: at}
			for i := range 4 + len(tc.widerReads) {
				box, number, confidence := [4]int{800, 510, 39, 36}, "1234", .94
				if i >= 4 {
					box, number, confidence = [4]int{800, 510, 41, 29}, tc.widerReads[i-4], .75
				}
				reading := plate.Reading{Number: number, Confidence: confidence}
				p := EnginePlate{Box: box, Score: .62, Reading: reading, Verifier: &reading,
					Localized:      &plate.Reading{Number: number, Confidence: .90},
					SequenceNumber: number, NumberScores: map[string]float64{number: .98}}
				seen := at.Add(time.Duration(i) * 125 * time.Millisecond)
				track.observe(seen, seen, p, strconv.Itoa(i), .60)
			}
			d, ok := track.detection(true)
			if ok != tc.wantSave || ok && (d.Reading.Number != "1234" || d.Box[2] != 39) {
				t.Fatalf("stored = %v, record = %+v", ok, d)
			}
		})
	}
}

func TestSmallSerialNeedsTwoDistinctFramesAndLocalizedAgreement(t *testing.T) {
	tests := []struct {
		name       string
		secondID   string
		localized  string
		wantRecord bool
	}{
		{name: "two frames and localized check", secondID: "2", localized: "1234", wantRecord: true},
		{name: "same frame", secondID: "1", localized: "1234"},
		{name: "missing localized check", secondID: "2"},
		{name: "different localized number", secondID: "2", localized: "1238"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Unix(100, 0)
			track := &plateTrack{id: "pt-test", first: at}
			track.observe(at, at, EnginePlate{Box: [4]int{700, 500, 41, 33}, Score: .54,
				Reading:      plate.Reading{Number: "1238", Confidence: .40},
				Verifier:     &plate.Reading{Number: "1234", Confidence: .80},
				NumberScores: map[string]float64{"1234": .963}, SequenceNumber: "1234"}, "1", .60)
			read := EnginePlate{Box: [4]int{701, 502, 43, 27}, Score: .68,
				Reading:      plate.Reading{Number: "1234", Confidence: .754},
				Verifier:     &plate.Reading{Number: "1234", Confidence: .754},
				NumberScores: map[string]float64{"1234": .984}, SequenceNumber: "1234"}
			if tc.localized != "" {
				read.Localized = &plate.Reading{Number: tc.localized, Confidence: .83}
			}
			later := at.Add(125 * time.Millisecond)
			track.observe(later, later, read, tc.secondID, .60)
			d, ok := track.detection(true)
			if ok != tc.wantRecord || ok && (d.Reading.Number != "1234" || !d.At.Equal(at) || !d.Track.LastSeen.Equal(later)) {
				t.Fatalf("saved %+v (%t), expected record: %t", d, ok, tc.wantRecord)
			}
		})
	}
}

func TestConflictingOpeningDigitNeedsTwoIndependentImages(t *testing.T) {
	const number = "6195"
	for _, tc := range []struct {
		name, conflict, firstCTC, thirdID string
		leadingFrame                      int
		rejoin                            bool
		wantSave                          bool
	}{
		{name: "unmarked older input", firstCTC: number, leadingFrame: 0, wantSave: true},
		{name: "unresolved opening conflict", conflict: number, firstCTC: number, leadingFrame: 0},
		{name: "joined fragments retain the opening conflict", conflict: number, firstCTC: number, rejoin: true},
		{name: "weak observations extend but do not release", conflict: number, firstCTC: number, thirdID: "weak"},
		{name: "same digit image reused", conflict: number, firstCTC: number, thirdID: "second"},
		{name: "first image lacks sequence agreement", conflict: number, wantSave: true},
		{name: "distinct digit image corroborates", conflict: number, firstCTC: number, thirdID: "third", wantSave: true},
		{name: "conflict outside the first two frames", conflict: number, firstCTC: number, thirdID: "third", leadingFrame: 2, wantSave: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Unix(100, 0)
			track := &plateTrack{id: "pt-test", first: at}
			detail := .1
			reading := plate.Reading{Number: number, Confidence: .92}
			p := EnginePlate{Box: [4]int{700, 500, 42, 26}, Score: .74, DigitEdges: &detail,
				Reading: reading, Verifier: &reading, SequenceNumber: tc.firstCTC, NumberScores: map[string]float64{number: .99}}
			if tc.leadingFrame == 0 {
				p.LocalizedLeadConflict = tc.conflict
			}
			track.observe(at, at, p, "first", .60)
			p.Box[2] = 47
			p.LocalizedLeadConflict = ""
			p.SequenceNumber = number
			p.Localized = &reading
			later := at.Add(125 * time.Millisecond)
			second := track
			if tc.rejoin {
				second = &plateTrack{first: later}
			}
			second.observe(later, later, p, "second", .60)
			if tc.rejoin {
				track.join(second)
			}
			if tc.thirdID != "" {
				later = later.Add(125 * time.Millisecond)
				if tc.thirdID == "weak" {
					p.Localized, p.Verifier = nil, nil
					p.Reading = plate.Reading{}
				}
				if tc.leadingFrame == 2 {
					p.LocalizedLeadConflict = tc.conflict
				}
				track.observe(later, later, p, tc.thirdID, .60)
			}
			d, saved := track.detection(true)
			if saved != tc.wantSave || saved && (d.Reading.Number != number || !d.At.Equal(at) || !d.Track.LastSeen.Equal(later)) {
				t.Fatalf("saved %t as %+v, want save %t", saved, d, tc.wantSave)
			}
		})
	}
}

func TestCompetingSerialFramesNeedLocalizedConfirmation(t *testing.T) {
	tests := []struct {
		name       string
		competing  int
		localized  bool
		wantRecord bool
	}{
		{name: "occasional alternative", competing: 1, wantRecord: true},
		{name: "repeated alternative", competing: 2},
		{name: "separate digit crop corroborates winner", competing: 2, localized: true, wantRecord: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			track := &plateTrack{id: "pt-test"}
			at := time.Unix(100, 0)
			for i := range tc.competing + 4 {
				p := EnginePlate{Box: [4]int{700, 500, 66, 34}, Score: .72,
					Reading:      plate.Reading{Number: "1234", Confidence: .92},
					NumberScores: map[string]float64{"1234": .99}, SequenceNumber: "1234"}
				if i < tc.competing {
					p.Reading = plate.Reading{Number: "1238", Confidence: .66}
					p.NumberScores = map[string]float64{"1238": .85}
					p.SequenceNumber = "1238"
				} else if tc.localized && i == tc.competing {
					p.Localized = &plate.Reading{Number: "1234", Confidence: .79}
				}
				p.Verifier = &p.Reading
				track.observe(at, at, p, string(rune('a'+i)), .60)
				at = at.Add(200 * time.Millisecond)
			}
			if d, ok := track.detection(true); ok != tc.wantRecord || ok && d.Reading.Number != "1234" {
				t.Fatalf("saved %q (%t), want a record: %t", d.Reading.Number, ok, tc.wantRecord)
			}
		})
	}
}

func TestSingleDistantDetectionNeedsStrongLocalizedRead(t *testing.T) {
	at := time.Unix(100, 0)
	for _, tc := range []struct {
		name       string
		width      int
		localized  float64
		secondBox  bool
		wantRecord bool
	}{
		{name: "weak localized read", width: 47, localized: .86},
		{name: "strong localized read", width: 47, localized: .93, wantRecord: true},
		{name: "another observation", width: 47, localized: .86, secondBox: true, wantRecord: true},
		{name: "wider crop", width: 48, localized: .86, wantRecord: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := EnginePlate{Box: [4]int{800, 510, tc.width, 29}, Score: .69,
				Reading:        plate.Reading{Number: "1234", Confidence: .94},
				Verifier:       &plate.Reading{Number: "1234", Confidence: .91},
				Localized:      &plate.Reading{Number: "1234", Confidence: tc.localized},
				SequenceNumber: "1234", NumberScores: map[string]float64{"1234": .99}}
			track := &plateTrack{first: at}
			track.observe(at, at, p, "1", .60)
			last := at
			if tc.secondBox {
				last = at.Add(125 * time.Millisecond)
				track.observe(last, last, EnginePlate{Box: p.Box, Score: .28}, "2", .60)
			}
			d, ok := track.detection(true)
			if ok != tc.wantRecord || ok && (d.Reading.Number != "1234" || !d.Track.LastSeen.Equal(last)) {
				t.Fatalf("stored = %v, record = %+v", ok, d)
			}
		})
	}
}

func TestDistantRepeatedSerialNeedsCorroborationNearVerificationFloor(t *testing.T) {
	at := time.Unix(100, 0)
	for _, tc := range []struct {
		name                        string
		width                       int
		verification, joint, digits float64
		wantSave                    bool
	}{
		{name: "one faint reading keeps repeating", width: 48, verification: .803, joint: .67},
		{name: "strong verification", width: 48, verification: .87, joint: .67, wantSave: true},
		{name: "two readers agree strongly", width: 48, verification: .803, joint: .75, wantSave: true},
		{name: "digit crops corroborate", width: 48, verification: .803, joint: .67, digits: .88, wantSave: true},
		{name: "wider view", width: 55, verification: .803, joint: .67, wantSave: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := EnginePlate{Box: [4]int{800, 510, tc.width, 32}, Score: .52,
				Reading:      plate.Reading{Number: "1234", Confidence: .82},
				Verifier:     &plate.Reading{Number: "1234", Confidence: tc.joint},
				NumberScores: map[string]float64{"1234": tc.verification}}
			if tc.digits > 0 {
				p.Localized = &plate.Reading{Number: "1234", Confidence: tc.digits}
			}
			track := &plateTrack{first: at}
			for i := range 8 {
				seen := at.Add(time.Duration(i) * 125 * time.Millisecond)
				track.observe(seen, seen, p, strconv.Itoa(i), .60)
			}
			d, ok := track.detection(true)
			if ok != tc.wantSave || ok && (d.Reading.Number != "1234" || d.Track.Frames != 8) {
				t.Fatalf("stored = %v, record = %+v", ok, d)
			}
		})
	}
}

func TestShortSerialNeedsToResolveIndependentDigitDisagreement(t *testing.T) {
	start := time.Unix(100, 0)
	const chosen, alternative = "123", "923"
	for _, tc := range []struct {
		name                  string
		repeatFrame           bool
		sequenceScore         float64
		chosenDigitConfidence float64
		wantSave              bool
	}{
		{name: "different digit crops and verifier agree", sequenceScore: .95},
		{name: "same crop seen twice", repeatFrame: true, sequenceScore: .95, wantSave: true},
		{name: "alternate verifier is weak", sequenceScore: .90, wantSave: true},
		{name: "digit crop also supports winning read", sequenceScore: .95, chosenDigitConfidence: .87, wantSave: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			track := &plateTrack{first: start}
			for i := range 8 {
				p := verifiedPlateForRejoin(chosen, [4]int{1080, 500, 58, 35})
				frame := strconv.Itoa(i)
				switch i {
				case 0:
					p.Verifier = &plate.Reading{Number: alternative, Confidence: .72}
					p.SequenceNumber = alternative
					p.NumberScores = map[string]float64{alternative: tc.sequenceScore}
				case 1, 2:
					p.Localized = &plate.Reading{Number: alternative, Confidence: .87}
					if tc.repeatFrame {
						frame = "1"
					}
				case 3:
					if tc.chosenDigitConfidence != 0 {
						p.Localized = &plate.Reading{Number: chosen, Confidence: tc.chosenDigitConfidence}
					}
				}
				seen := start.Add(time.Duration(i) * 125 * time.Millisecond)
				track.observe(seen, seen, p, frame, .60)
			}
			if track.best.reading.Number != chosen {
				t.Fatalf("whole-plate vote = %q, want %q", track.best.reading.Number, chosen)
			}
			if d, saved := track.detection(false); saved != tc.wantSave {
				t.Fatalf("publication = %v, want %v: %+v", saved, tc.wantSave, d)
			}
		})
	}
}

func TestObliqueEdgeApproachNeedsClearerSerialEvidence(t *testing.T) {
	start := time.Unix(100, 0)
	for _, tc := range []struct {
		name       string
		firstWidth int
		clearer    bool
		localized  float64
		wantSave   bool
	}{
		{name: "foreshortened before early publication", firstWidth: 27},
		{name: "less oblique confirming crop", firstWidth: 27, clearer: true, wantSave: true},
		{name: "strong separate digit crop", firstWidth: 27, localized: .92, wantSave: true},
		{name: "similar width throughout the pass", firstWidth: 36, wantSave: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			track := &plateTrack{first: start}
			firstRead := verifiedPlateForRejoin("123", [4]int{1360, 546, 43, 26})
			firstRead.Verifier.Confidence = .815
			if tc.clearer {
				firstRead.Box[2] = 46
			}
			firstRead.Localized = &plate.Reading{Number: "123", Confidence: .825}
			newestRead := verifiedPlateForRejoin("123", [4]int{1430, 565, 48, 32})
			newestRead.Reading.Confidence = .89
			newestRead.Verifier.Confidence = .81
			newestRead.Localized = &plate.Reading{Number: "123", Confidence: max(.82, tc.localized)}
			for i, p := range []EnginePlate{
				{Box: [4]int{1189, 506, tc.firstWidth, 14}, Score: .55},
				{Box: [4]int{1224, 514, 30, 17}, Score: .58},
				{Box: [4]int{1245, 519, 31, 17}, Score: .59},
				{Box: [4]int{1268, 525, 35, 20}, Score: .65, Reading: plate.Reading{Number: "123", Confidence: .78}},
				firstRead, newestRead,
			} {
				seen := start.Add(time.Duration(i) * 175 * time.Millisecond)
				track.observe(seen, seen, p, strconv.Itoa(i), .60)
			}
			if track.best.box != firstRead.Box {
				t.Fatalf("representative crop = %v, want earlier crop %v", track.best.box, firstRead.Box)
			}
			if d, saved := track.detection(false); saved != tc.wantSave {
				t.Fatalf("early publication = %v, want %v: %+v", saved, tc.wantSave, d)
			}
		})
	}
}

func TestWeakOpeningPublicationAllowsLaterDigitEvidence(t *testing.T) {
	start := time.Unix(100, 0)
	for _, tc := range []struct {
		name         string
		verification float64
		last         time.Duration
		contradicts  bool
		final        bool
		wantSave     bool
	}{
		{name: "weak votes still waiting", verification: .925},
		{name: "digit reader disproves weak consensus", verification: .925, last: 4750 * time.Millisecond, contradicts: true, final: true},
		{name: "conflict-free short track can finalize", verification: .925, final: true, wantSave: true},
		{name: "confirmation window expires", verification: .925, last: 5 * time.Second, wantSave: true},
		{name: "strong sequence corroborates the early read", verification: .97, wantSave: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			track := &plateTrack{first: start}
			p := EnginePlate{Box: [4]int{1028, 495, 58, 32}, Score: .41,
				Reading:        plate.Reading{Number: "1234", Class: "301", Confidence: .844},
				Verifier:       &plate.Reading{Number: "1234", Confidence: .55},
				SequenceNumber: "1234",
				NumberScores:   map[string]float64{"1234": tc.verification},
			}
			for i := range 3 {
				at := start.Add(time.Duration(i) * 2 * time.Second)
				track.observe(at, at, p, strconv.Itoa(i), .60)
			}
			if tc.last != 0 {
				at := start.Add(tc.last)
				last := EnginePlate{Box: p.Box, Score: p.Score}
				if tc.contradicts {
					last.Localized = &plate.Reading{Number: "1235", Confidence: .91}
				}
				track.observe(at, at, last, "3", .60)
			}
			detection, saved := track.detection(tc.final)
			if saved != tc.wantSave || saved && detection.Reading.Number != "1234" {
				t.Fatalf("saved = %v, want %v with unchanged serial: %+v", saved, tc.wantSave, detection)
			}
		})
	}
}

func TestNarrowPlateCombinesChecksFromSeparateFrames(t *testing.T) {
	start := time.Unix(100, 0)
	for _, tc := range []struct {
		name            string
		localizedFrames int
		primary         string
		want            bool
	}{
		{"consistent independent readings", 2, "1234", true},
		{"one localized reading", 1, "1234", false},
		{"different primary number", 2, "1284", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			track := &plateTrack{first: start}
			for i := range 6 {
				p := EnginePlate{Box: [4]int{800, 510, 40, 24}, Score: .49,
					Reading:        plate.Reading{Number: "1284", Confidence: .63},
					Verifier:       &plate.Reading{Number: "1234", Confidence: .75},
					SequenceNumber: "1234", NumberScores: map[string]float64{"1234": .97, "1284": .1}}
				if i >= 4 {
					p.Reading = plate.Reading{Number: tc.primary, Confidence: .56}
					p.Verifier.Confidence = .54
					p.NumberScores["1234"] = .91
					if i-4 < tc.localizedFrames {
						p.Localized = &plate.Reading{Number: "1234", Confidence: .84}
					}
				}
				seen := start.Add(time.Duration(i) * 125 * time.Millisecond)
				track.observe(seen, seen, p, strconv.Itoa(i), .60)
			}
			d, ok := track.detection(true)
			if ok != tc.want {
				t.Fatalf("stored narrow crop = %v, want %v", ok, tc.want)
			}
			if ok && (d.Reading.Number != "1234" || d.At != start || d.Track.LastSeen != start.Add(625*time.Millisecond)) {
				t.Fatalf("unexpected reading or span: %+v", d)
			}
		})
	}
}

func TestNarrowPlateWithWeakPrimaryCueNeedsExtraFrames(t *testing.T) {
	start := time.Unix(100, 0)
	for _, tc := range []struct {
		name              string
		sequenceFrames    int
		localizedFrames   int
		primaryConfidence float64
		want              bool
	}{
		{"matching weak cue with extra support", 5, 3, .44, true},
		{"missing sequence frame", 4, 3, .44, false},
		{"missing localized frame", 5, 2, .44, false},
		{"primary cue too weak", 5, 3, .39, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			track := &plateTrack{first: start}
			for i := range tc.sequenceFrames {
				p := EnginePlate{
					Box: [4]int{800, 510, 40, 24}, Score: .49,
					Reading:        plate.Reading{Number: "1284", Confidence: .43},
					Verifier:       &plate.Reading{Number: "1234", Confidence: .75},
					SequenceNumber: "1234",
					NumberScores:   map[string]float64{"1234": .97, "1284": .1},
				}
				if i == 0 {
					p.Reading = plate.Reading{Number: "1234", Confidence: tc.primaryConfidence}
				}
				if i < tc.localizedFrames {
					p.Localized = &plate.Reading{Number: "1234", Confidence: .89}
				}
				seen := start.Add(time.Duration(i) * 125 * time.Millisecond)
				track.observe(seen, seen, p, strconv.Itoa(i), .60)
			}
			d, ok := track.detection(true)
			if ok != tc.want || (ok && d.Reading.Number != "1234") {
				t.Fatalf("stored expected plate = %v, want %v", ok && d.Reading.Number == "1234", tc.want)
			}
		})
	}
}

func TestIndependentAgreementOutweighsRepeatedWeakEvidence(t *testing.T) {
	start := time.Now()
	track := &plateTrack{first: start}
	p := EnginePlate{Box: [4]int{800, 510, 65, 33}, Score: .70,
		Reading:      plate.Reading{Number: "1284", Confidence: .95},
		Verifier:     &plate.Reading{Number: "1284", Confidence: .65},
		NumberScores: map[string]float64{"1284": .89}}
	for i := range 9 {
		seen := start.Add(time.Duration(i) * 125 * time.Millisecond)
		track.observe(seen, seen, p, "glare", .60)
	}
	p.Reading = plate.Reading{Number: "1234", Confidence: .88}
	p.Verifier = &plate.Reading{Number: "1234", Confidence: .86}
	p.Localized = &plate.Reading{Number: "1234", Confidence: .90}
	p.SequenceNumber = "1234"
	p.NumberScores = map[string]float64{"1234": .99}
	for i := range 3 {
		seen := start.Add(time.Duration(i+9) * 125 * time.Millisecond)
		track.observe(seen, seen, p, "clear", .60)
	}
	d, ok := track.detection(true)
	if !ok || d.Reading.Number != "1234" || d.Frame != "clear" || !d.Track.Conflict || d.Track.Support != 3 {
		t.Fatalf("lost independently supported serial: %+v", d)
	}
}

func TestWideShortReadNeedsStrongDetectionOrIndependentConfirmation(t *testing.T) {
	at := time.Now()
	for _, tc := range []struct {
		name      string
		detector  float64
		localized *plate.Reading
		want      bool
	}{
		{"weak object with optional fields", .33, nil, false},
		{"clear short plate", .60, nil, true},
		{"weak detection confirmed by digit positions", .36, &plate.Reading{Number: "17", Confidence: .90}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := EnginePlate{Box: [4]int{800, 510, 180, 96}, Score: tc.detector,
				Reading:        plate.Reading{Number: "17", Region: "品川", Class: "300", Kana: "あ", Confidence: .91},
				Verifier:       &plate.Reading{Number: "17", Confidence: .88},
				Localized:      tc.localized,
				SequenceNumber: "17", NumberScores: map[string]float64{"17": .89}}
			track := &plateTrack{first: at}
			track.observe(at, at, p, "crop", .60)
			if _, ok := track.detection(true); ok != tc.want {
				t.Fatalf("stored wide short crop = %v, want %v", ok, tc.want)
			}
		})
	}
}

func TestWideThinSignNeedsACorroboratingCrop(t *testing.T) {
	start := time.Now()
	track := &plateTrack{first: start}
	reading := plate.Reading{Number: "1234", Region: "相模", Class: "300", Kana: "さ", Confidence: .74}
	verifier := plate.Reading{Number: "1234", Confidence: .90}
	p := EnginePlate{Box: [4]int{900, 500, 126, 46}, Score: .55,
		Reading: reading, Verifier: &verifier, SequenceNumber: "1234", NumberScores: map[string]float64{"1234": .99}}
	for i := range 8 {
		at := start.Add(time.Duration(i) * 125 * time.Millisecond)
		track.observe(at, at, p, "thin", .60)
	}
	if _, ok := track.detection(true); ok {
		t.Fatal("stored a wide, thin crop supported only by repeated weak primary reads")
	}
	p.Box = [4]int{900, 500, 115, 55}
	p.Reading.Confidence = .89
	at := start.Add(time.Second)
	track.observe(at, at, p, "clear", .60)
	if d, ok := track.detection(true); !ok || d.Reading.Number != "1234" || !d.At.Equal(start) || !d.Track.LastSeen.Equal(at) {
		t.Fatalf("missed serial confirmed in a better crop: %+v", d)
	}
}

func TestNarrowPlateCanUseThreeIndependentAlignedReads(t *testing.T) {
	start := time.Now()
	track := &plateTrack{first: start}
	p := EnginePlate{Box: [4]int{900, 500, 35, 26}, Score: .55,
		Reading:        plate.Reading{Number: "1234", Confidence: .81},
		Verifier:       &plate.Reading{Number: "1234", Confidence: .58},
		Localized:      &plate.Reading{Number: "1234", Confidence: .92},
		SequenceNumber: "1234", NumberScores: map[string]float64{"1234": .90}}
	for i, frame := range [...]string{"first", "second", "third"} {
		at := start.Add(time.Duration(i) * 125 * time.Millisecond)
		track.observe(at, at, p, frame, .60)
		if d, ok := track.detection(true); ok != (i == 2) {
			t.Fatalf("after %d frames, detection = %+v, saved = %v", i+1, d, ok)
		}
	}
}

func TestWideLowConfidenceObjectNeedsIndependentConfirmation(t *testing.T) {
	start := time.Now()
	for _, tc := range []struct {
		name      string
		localized *plate.Reading
		want      bool
	}{
		{"two weak reads", nil, false},
		{"confirmed digit positions", &plate.Reading{Number: "9000", Confidence: .90}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := EnginePlate{Box: [4]int{1200, 510, 158, 63}, Score: .37,
				Reading:      plate.Reading{Number: "9000", Confidence: .65},
				Localized:    tc.localized,
				NumberScores: map[string]float64{"9000": .91}}
			track := &plateTrack{first: start}
			track.observe(start, start, p, "one", .60)
			next := start.Add(125 * time.Millisecond)
			track.observe(next, next, p, "two", .60)
			if d, ok := track.detection(true); ok != tc.want {
				t.Fatalf("stored wide crop = %v, record = %+v", ok, d)
			}
		})
	}
}

func TestWeakPlateHoldsSerialWhenTwoReadersSupportACompetingDigit(t *testing.T) {
	start := time.Now()
	for _, tc := range []struct {
		name       string
		detector   float64
		challenger string
		want       bool
	}{
		{"one reader disagrees", .35, "1274", true},
		{"readers support the same alternative", .35, "1284", false},
		{"clear crop at higher detector confidence", .55, "1284", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			track := &plateTrack{first: start}
			box := [4]int{800, 510, 60, 33}
			track.observe(start, start, EnginePlate{Box: box, Score: .31,
				Localized: &plate.Reading{Number: "1284", Confidence: .80}}, "1", .60)
			next := start.Add(125 * time.Millisecond)
			track.observe(next, next, EnginePlate{Box: box, Score: .49,
				Verifier:       &plate.Reading{Number: tc.challenger, Confidence: .55},
				SequenceNumber: tc.challenger, NumberScores: map[string]float64{tc.challenger: .82}}, "2", .60)
			p := EnginePlate{Box: box, Score: tc.detector,
				Reading:        plate.Reading{Number: "1234", Confidence: .92},
				Verifier:       &plate.Reading{Number: "1234", Confidence: .84},
				Localized:      &plate.Reading{Number: "1234", Confidence: .83},
				SequenceNumber: "1234", NumberScores: map[string]float64{"1234": .98}}
			for i := range 3 {
				seen := start.Add(time.Duration(i+2) * 125 * time.Millisecond)
				track.observe(seen, seen, p, "main", .60)
			}
			if d, ok := track.detection(true); ok != tc.want {
				t.Fatalf("stored serial = %v, record = %+v", ok, d)
			}
		})
	}
}

func TestRepeatedLocalizedDisagreementHoldsAFullWidthSerial(t *testing.T) {
	start := time.Now()
	for _, tc := range []struct {
		name      string
		frames    [2]string
		alternate string
		confirmed bool
		wantSaved bool
	}{
		{"same frame twice", [2]string{"one", "one"}, "1284", false, true},
		{"two separate frames", [2]string{"one", "two"}, "1284", false, false},
		{"more than one different digit", [2]string{"one", "two"}, "1874", false, true},
		{"winner also confirmed", [2]string{"one", "two"}, "1284", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			track := &plateTrack{first: start}
			box := [4]int{800, 510, 75, 37}
			for i, frame := range tc.frames {
				at := start.Add(time.Duration(i) * 125 * time.Millisecond)
				track.observe(at, at, EnginePlate{Box: box, Score: .47,
					Localized: &plate.Reading{Number: tc.alternate, Confidence: .82}}, frame, .60)
			}
			p := EnginePlate{Box: box, Score: .47,
				Reading:        plate.Reading{Number: "1234", Confidence: .92},
				Verifier:       &plate.Reading{Number: "1234", Confidence: .84},
				SequenceNumber: "1234", NumberScores: map[string]float64{"1234": .98}}
			if tc.confirmed {
				p.Localized = &plate.Reading{Number: "1234", Confidence: .81}
			}
			for i := range 3 {
				at := start.Add(time.Duration(i+2) * 125 * time.Millisecond)
				track.observe(at, at, p, strconv.Itoa(i), .60)
			}
			d, ok := track.detection(true)
			if ok != tc.wantSaved || (ok && d.Reading.Number != "1234") {
				t.Fatalf("stored serial = %v, reading = %+v", ok, d.Reading)
			}
		})
	}
}

func TestJoinPreservesDistinctLocalizedEvidence(t *testing.T) {
	best := observation{reading: plate.Reading{Number: "1234"}, box: [4]int{800, 510, 75, 37}}
	for _, nextFrame := range []string{"same", "second"} {
		t.Run(nextFrame, func(t *testing.T) {
			prior, next := &plateTrack{frames: 2}, &plateTrack{frames: 2}
			prior.rememberLocalizedFrame("1284", "same")
			next.rememberLocalizedFrame("1284", nextFrame)
			prior.join(next)
			if got := prior.localizedConflicts(best); got != (nextFrame != "same") {
				t.Fatalf("joined evidence conflict = %v", got)
			}
		})
	}
}

func TestRejoinOverlappingCropsCountsEachFrameOnce(t *testing.T) {
	start := time.Now()
	for _, tc := range []struct {
		name, laterClass string
		wantTracks       int
	}{
		{"matching plate", "300", 1},
		{"classification conflicts", "500", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first, second := &plateTrack{first: start}, &plateTrack{first: start.Add(250 * time.Millisecond)}
			reading := plate.Reading{Number: "1234", Region: "品川", Class: "300", Kana: "あ", Confidence: .94}
			p := EnginePlate{Box: [4]int{800, 510, 55, 28}, Score: .83, Reading: reading,
				Verifier: &reading, SequenceNumber: "1234", NumberScores: map[string]float64{"1234": .99}}
			for i := range 4 {
				at := start.Add(time.Duration(i) * 125 * time.Millisecond)
				first.observe(at, at, p, "earlier", .6)
			}
			first.misses = 1
			p.Box[1] += 6
			p.Reading.Class = tc.laterClass
			later := p.Reading
			p.Verifier = &later
			for i := range 4 {
				at := second.first.Add(time.Duration(i) * 125 * time.Millisecond)
				second.observe(at, at, p, "later", .6)
			}

			tracks := rejoinTracks([]*plateTrack{first, second}, nil)
			if len(tracks) != tc.wantTracks {
				t.Fatalf("got %d tracks, want %d", len(tracks), tc.wantTracks)
			}
			if tc.wantTracks == 1 && (tracks[0] != first || first.frames != 6 || first.support != 6 ||
				!first.last.Equal(second.last) || !first.first.Equal(start)) {
				t.Fatalf("expected six unique observations over the combined interval: %+v", first)
			}
		})
	}
}

func TestFastPassRejoinsIndependentlyVerifiedFourDigitFragments(t *testing.T) {
	at := time.Now()
	for _, tc := range []struct {
		name        string
		x           int
		independent bool
		want        bool
	}{
		{"outward at the edge", 1555, true, true},
		{"inward movement", 1345, true, false},
		{"lacks independent evidence", 1555, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := &plateTrack{first: at}
			p := EnginePlate{Box: [4]int{1450, 580, 50, 30}, Score: .64,
				Reading:        plate.Reading{Number: "1234", Confidence: .95},
				Verifier:       &plate.Reading{Number: "1234", Confidence: .89},
				Localized:      &plate.Reading{Number: "1234", Confidence: .91},
				SequenceNumber: "1234", NumberScores: map[string]float64{"1234": .98}}
			first.observe(at, at, p, "one", .60)
			next := at.Add(125 * time.Millisecond)
			second := &plateTrack{first: next}
			p.Box = [4]int{tc.x, 608, 55, 35}
			p.Reading.Confidence = .81
			p.Verifier.Confidence = .80
			if !tc.independent {
				p.Verifier = nil
				p.Localized = nil
			}
			second.observe(next, next, p, "two", .60)
			if got := first.canRejoin(second); got != tc.want {
				t.Fatalf("rejoin fast pass = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestInwardPassRejoinsSingleFourDigitReadWithMeasuredContinuation(t *testing.T) {
	start := time.Unix(100, 0)
	for _, tc := range []struct {
		name       string
		finalX     int
		verified   bool
		joinedTail bool
		wantJoined bool
	}{
		{"continuing inward", 790, true, false, true},
		{"continuation already absorbed a fragment", 790, true, true, true},
		{"second fragment turns back", 710, true, false, false},
		{"first reading lacks independent verification", 790, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reading := plate.Reading{Number: "1234", Confidence: .95}
			p := EnginePlate{Box: [4]int{395, 490, 55, 42}, Score: .65,
				Reading: reading, SequenceNumber: reading.Number,
				NumberScores: map[string]float64{reading.Number: .99}}
			if tc.verified {
				p.Verifier = &reading
			}
			first := &plateTrack{first: start}
			first.observe(start, start, p, "1", .60)
			next := &plateTrack{first: start.Add(125 * time.Millisecond)}
			p.Verifier = &reading
			for i, sample := range []struct {
				frame string
				at    time.Duration
				box   [4]int
			}{
				{"2", 125 * time.Millisecond, [4]int{526, 481, 50, 32}},
				{"5", 500 * time.Millisecond, [4]int{745, 471, 46, 27}},
				{"6", 625 * time.Millisecond, [4]int{tc.finalX, 471, 45, 26}},
			} {
				at := start.Add(sample.at)
				p.Box = sample.box
				next.observe(at, at, p, sample.frame, .60)
				if i < 2 && first.canRejoin(next) {
					t.Fatal("joined before there were two later motion samples")
				}
			}
			if tc.joinedTail {
				next.startFrames = nil
				next.recentFrames = next.recentFrames[1:]
			}
			if got := first.canRejoin(next); got != tc.wantJoined {
				t.Fatalf("inward pass rejoined = %t, want %t", got, tc.wantJoined)
			}
		})
	}
}

func TestWeakReplacementStillExtendsVerifiedInterval(t *testing.T) {
	at := time.Now()
	track := &plateTrack{first: at}
	p := EnginePlate{Box: [4]int{800, 510, 62, 32}, Score: .84,
		Reading:        plate.Reading{Number: "1234", Confidence: .94},
		SequenceNumber: "1234", NumberScores: map[string]float64{"1234": .99}}
	track.observe(at, at, p, "1", .6)
	if _, ok := track.detection(true); !ok {
		t.Fatal("did not publish the verified first reading")
	}
	p.Reading.Number = "1284"
	p.SequenceNumber = ""
	p.NumberScores = map[string]float64{"1284": .12}
	var last time.Time
	for i := 1; i <= 12; i++ {
		last = at.Add(time.Duration(i) * 250 * time.Millisecond)
		track.observe(last, last, p, "later", .6)
	}
	d, ok := track.detection(true)
	if !ok || !d.Track.Update || !d.Track.Ambiguous || !d.Track.LastSeen.Equal(last) {
		t.Fatalf("expected a duration update without publishing the weak replacement: %+v", d)
	}
}

func TestRecoverMovingSerialWithIndependentDigits(t *testing.T) {
	for _, tc := range []struct {
		name          string
		stationary    bool
		competingRead bool
		oneFrame      bool
		glare         bool
		motionEdges   bool
		weakSequence  bool
		shiftedRead   bool
		minConfidence float64
		want          bool
	}{
		{name: "separate localized crops", minConfidence: .60, want: true},
		{name: "corners measured for motion corroboration", motionEdges: true, minConfidence: .60, want: true},
		{name: "weak sequence without crop corroboration", weakSequence: true, minConfidence: .60},
		{name: "sequence corroborated by shifted crop", weakSequence: true, shiftedRead: true, minConfidence: .60, want: true},
		{name: "stationary object", stationary: true, minConfidence: .60},
		{name: "competing localized serial", competingRead: true, minConfidence: .60},
		{name: "same frame reused", oneFrame: true, minConfidence: .60},
		{name: "glare hides outer digit", glare: true, minConfidence: .60},
		{name: "owner confidence floor", minConfidence: .90},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			track := &plateTrack{first: start}
			sharp := .25
			for i := range 4 {
				x, y := 1380+35*i, 540+9*i
				if tc.stationary {
					x, y = 1380, 540
				}
				p := EnginePlate{Box: [4]int{x, y, 38, 25}, Score: .62, DigitEdges: &sharp,
					Reading:        plate.Reading{Number: "1746", Confidence: .91},
					Verifier:       &plate.Reading{Number: "1246", Confidence: .59},
					SequenceNumber: "1246", NumberScores: map[string]float64{"1246": .96}}
				if tc.motionEdges {
					p.DigitEdges = nil
					p.MotionDigitEdges = &sharp
				}
				if tc.weakSequence && i >= 2 {
					p.Verifier.Confidence = .48
					p.NumberScores["1246"] = .80
					p.MotionSequenceVerified = tc.shiftedRead && i == 3
				}
				if i%2 == 0 {
					p.Localized = &plate.Reading{Number: "1246", Confidence: .92}
				}
				if tc.competingRead && i == 1 {
					p.Localized = &plate.Reading{Number: "1746", Confidence: .86}
				}
				if tc.glare {
					p.appearance = digitBandWithGlare()
				}
				frame := strconv.Itoa(i)
				if tc.oneFrame {
					frame = "0"
				}
				at := start.Add(time.Duration(i) * 125 * time.Millisecond)
				track.observe(at, at, p, frame, tc.minConfidence)
				if d, saved := track.detection(false); saved {
					t.Fatalf("published before the pass ended: %+v", d)
				}
			}
			d, saved := track.detection(true)
			if saved != tc.want || saved && (d.Reading.Number != "1246" || d.Track.Support != 4 || d.Track.Ambiguous) {
				t.Fatalf("detection = %+v, saved = %v, want %v", d, saved, tc.want)
			}
		})
	}
}
