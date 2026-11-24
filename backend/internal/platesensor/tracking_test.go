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
				Localized:      &plate.Reading{Number: "1234", Confidence: .85},
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
