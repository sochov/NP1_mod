package platesensor

import (
	"math/rand/v2"
	"strconv"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

func verifiedPlateForRejoin(number string, box [4]int) EnginePlate {
	reading := plate.Reading{Number: number, Confidence: .95}
	return EnginePlate{
		Box: box, Score: .83, Reading: reading, Verifier: &reading,
		SequenceNumber: number, NumberScores: map[string]float64{number: .99},
	}
}

func makeAppearanceForTest(seed uint64) []byte {
	random := rand.New(rand.NewPCG(seed, 0))
	frame := make([]byte, FrameWidth*FrameHeight)
	for i := range frame {
		frame[i] = byte(random.Uint32())
	}
	return plateAppearance(frame, [4]int{730, 494, 90, 45})
}

func TestNearbyShortPlateWaitsForAnUncertainDigit(t *testing.T) {
	oldBox := [4]int{700, 500, 48, 22}
	newBox := [4]int{690, 504, 48, 21}
	view := makeAppearanceForTest(140)
	previous := verifiedPlateForRejoin("123", oldBox)
	old := observeFollowingTestTrack(time.Unix(100, 0), previous, 12)
	old.sent = old.frames

	for _, tc := range []struct {
		name   string
		number string
		gap    time.Duration
		box    [4]int
		wait   bool
	}{
		{"nearby difference", "923", 1250 * time.Millisecond, newBox, true},
		{"two different digits", "983", 1250 * time.Millisecond, newBox, false},
		{"outside the short gap", "923", 2 * time.Second, newBox, false},
		{"different position", "923", 1250 * time.Millisecond, [4]int{660, 504, 48, 21}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read := verifiedPlateForRejoin(tc.number, tc.box)
			read.appearance = view
			next := observeFollowingTestTrack(old.last.Add(tc.gap), read, 12)
			recent := []*plateTrack{old}
			if got := waitingForAppearance(next, nil, recent); got != tc.wait {
				t.Fatalf("waiting = %v, want %v", got, tc.wait)
			}
			if old.canRejoin(next) {
				t.Fatal("joined tracks with different numbers")
			}
			next.last = next.first.Add(trackRejoinGap)
			if waitingForAppearance(next, nil, recent) {
				t.Fatal("still waiting after the initial read window")
			}
		})
	}

	next := observeFollowingTestTrack(old.last.Add(1250*time.Millisecond), verifiedPlateForRejoin("123", newBox), 12)
	if !old.canRejoin(next) {
		t.Fatal("failed to rejoin after the serial agreed")
	}
}

func TestOutwardReadNeedsTwoDistinctFramesFromTheSameVehicle(t *testing.T) {
	car, other := makeAppearanceForTest(91), makeAppearanceForTest(92)
	for _, tc := range []struct {
		name       string
		second     []byte
		secondX    int
		reuseFrame bool
		wantNumber string
	}{
		{"two outward frames", car, 132, false, "1234"},
		{"second image unavailable", nil, 132, false, "8234"},
		{"different vehicle", other, 132, false, "8234"},
		{"reversed motion", car, 218, false, "8234"},
		{"same frame twice", car, 132, true, "8234"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Unix(100, 0)
			last := at.Add(750 * time.Millisecond)
			tr := &plateTrack{
				id: "car", first: at.Add(-2 * time.Second), last: at,
				firstBox: [4]int{704, 452, 27, 13}, box: [4]int{368, 475, 43, 27},
				velocity: [2]float64{-230, 12}, frames: 16, support: 3, reads: 3, hasVerification: true,
				best: observation{reading: plate.Reading{Number: "8234", Confidence: .95},
					box: [4]int{368, 475, 43, 27}, frame: "prior", at: at, appearance: car},
				votes: map[string]serialEvidence{"8234": {primaryProof: true, narrowSupport: 3}},
			}
			if _, saved := tr.detection(false); saved {
				t.Fatal("published before checking the later outward views")
			}

			first := verifiedPlateForRejoin("1234", [4]int{177, 484, 41, 38})
			first.Localized = &plate.Reading{Number: "1234", Confidence: .80}
			first.appearance = car
			second := verifiedPlateForRejoin("1234", [4]int{tc.secondX, 486, 36, 47})
			second.Localized = &plate.Reading{Number: "1234", Confidence: .89}
			second.appearance = tc.second
			secondID := "later-2"
			if tc.reuseFrame {
				secondID = "later-1"
			}
			tr.recentFrames = []trackFrame{
				{at: last, id: "later-1", plate: first},
				{at: last.Add(125 * time.Millisecond), id: secondID, plate: second},
			}
			tr.last, tr.box = tr.recentFrames[1].at, second.Box
			tr.frames += 2
			reading, published := tr.detection(false)
			if tc.wantNumber == "1234" {
				if !published || reading.Reading.Number != "1234" || reading.Frame != "later-1" {
					t.Fatalf("later serial was not published with its image: %+v", reading)
				}
				// A later update can arrive after the proof has left the recent window.
				tr.recentFrames = nil
				tr.last = tr.last.Add(650 * time.Millisecond)
				tr.frames++
			} else if published {
				t.Fatalf("published before two agreeing later views: %+v", reading)
			}
			d, ok := tr.detection(true)
			if !ok || d.Reading.Number != tc.wantNumber || !d.At.Equal(tr.first) || !d.Track.LastSeen.Equal(tr.last) {
				t.Fatalf("number or interval mismatch: %+v", d)
			}
		})
	}
}

func TestSteepOutwardRejoinRequiresOneCorroboratedApproach(t *testing.T) {
	const interval = time.Second / 6
	image, differentImage := makeAppearanceForTest(101), makeAppearanceForTest(102)
	for _, tc := range []struct {
		name                              string
		missing, competing, otherImage    bool
		weakProof, differentKana          bool
		reverse, differentPrecedingNumber bool
		gap                               time.Duration
		wantJoin                          bool
	}{
		{name: "one accelerating path", gap: interval, wantJoin: true},
		{name: "missing approach", gap: interval, missing: true},
		{name: "two possible approaches", gap: interval, competing: true},
		{name: "different vehicle view", gap: interval, otherImage: true},
		{name: "weak digit evidence", gap: interval, weakProof: true},
		{name: "conflicting kana", gap: interval, differentKana: true},
		{name: "earlier vehicle moving inward", gap: interval, reverse: true},
		{name: "different preceding serial", gap: interval, differentPrecedingNumber: true},
		{name: "a later encounter", gap: 15 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Unix(100, 0)
			firstAt := start.Add(2 * interval)
			lastAt := firstAt.Add(tc.gap)
			makeRead := func(at time.Time, box [4]int, frame string, view []byte) *plateTrack {
				p := verifiedPlateForRejoin("381", box)
				p.appearance = view
				track := &plateTrack{first: at}
				track.observe(at, at, p, frame, .60)
				return track
			}
			old := makeRead(firstAt, [4]int{1533, 489, 50, 39}, "first", image)
			tailView := image
			if tc.otherImage {
				tailView = differentImage
			}
			next := makeRead(lastAt, [4]int{1765, 522, 49, 58}, "last", tailView)
			if tc.weakProof {
				vote := next.votes["381"]
				vote.independentProof = false
				next.votes["381"] = vote
			}
			if tc.differentKana {
				old.best.reading.Kana, next.best.reading.Kana = "あ", "か"
			}
			priorAt := start.Add(interval)
			prior := &plateTrack{
				first: priorAt, last: priorAt, frames: 1, box: [4]int{1360, 474, 45, 29},
				recentFrames: []trackFrame{{at: priorAt, plate: EnginePlate{appearance: image}}},
			}
			if tc.differentPrecedingNumber {
				prior.recentFrames[0].plate.SequenceNumber = "781"
				prior.recentFrames[0].plate.NumberScores = map[string]float64{"781": .95}
			}
			early := &plateTrack{
				first: start.Add(-interval), last: start, frames: 2, box: [4]int{1265, 469, 33, 18},
				velocity: [2]float64{300, 25},
			}
			if tc.reverse {
				early.velocity[0] = -early.velocity[0]
			}
			tracks := []*plateTrack{old, next, prior}
			if !tc.missing {
				tracks = append(tracks, early)
			}
			if tc.competing {
				other := *early
				other.box[0] -= 5
				tracks = append(tracks, &other)
			}
			if got := old.canRejoinFastOutward(next, tracks); got != tc.wantJoin {
				t.Fatalf("can rejoin = %v, want %v", got, tc.wantJoin)
			}
			_, joined := rejoinTracksOnce(tracks, nil)
			if joined != tc.wantJoin {
				t.Fatalf("tracks joined = %v, want %v", joined, tc.wantJoin)
			}
			if joined && (old.frames != 2 || !old.first.Equal(firstAt) || !old.last.Equal(lastAt)) {
				t.Fatal("join did not preserve both numbered frames and their interval")
			}
		})
	}
}

func TestTwoNarrowReadsPreserveTheSeedWhenTracksJoin(t *testing.T) {
	for _, tc := range []struct {
		name, lastFrame, lastNumber string
		want                        bool
	}{
		{"two matching reads", "third", "1234", true},
		{"same frame reused", "second", "1234", false},
		{"different number", "third", "3234", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Unix(100, 0)
			seed := &plateTrack{first: start}
			first := verifiedPlateForRejoin("1234", [4]int{551, 483, 37, 20})
			first.Localized = &plate.Reading{Number: "1234", Confidence: .90}
			seed.observe(start, start, first, "first", .60)

			secondAt := start.Add(125 * time.Millisecond)
			tail := &plateTrack{first: secondAt}
			second := verifiedPlateForRejoin("1234", [4]int{587, 481, 33, 19})
			second.Localized = first.Localized
			tail.observe(secondAt, secondAt, second, "second", .60)
			lastAt := start.Add(250 * time.Millisecond)
			third := verifiedPlateForRejoin(tc.lastNumber, [4]int{609, 480, 32, 16})
			third.Localized = &plate.Reading{Number: tc.lastNumber, Confidence: .93}
			tail.observe(lastAt, lastAt, third, tc.lastFrame, .60)

			seed.join(tail)
			d, saved := seed.detection(true)
			if saved != tc.want || saved && (d.Reading.Number != "1234" || d.Track.Support != 3 ||
				!d.At.Equal(start) || !d.Track.LastSeen.Equal(lastAt)) {
				t.Fatalf("published = %v with %+v, want published = %v", saved, d, tc.want)
			}
		})
	}
}

func TestNarrowDigitDetailCanBeCorroboratedByALaterImage(t *testing.T) {
	first := time.Unix(100, 0)
	track := &plateTrack{first: first}
	observed := verifiedPlateForRejoin("1234", [4]int{850, 480, 46, 26})
	observed.Localized = &plate.Reading{Number: "1234", Confidence: .91}
	blurred := .04
	observed.DigitEdges = &blurred
	for i, frame := range []string{"first", "second", "third"} {
		at := first.Add(time.Duration(i) * 125 * time.Millisecond)
		track.observe(at, at, observed, frame, .60)
	}
	if _, saved := track.detection(true); saved {
		t.Fatal("repeated blurry reads were published")
	}

	sharp := .12
	observed.DigitEdges = &sharp
	at := first.Add(375 * time.Millisecond)
	track.observe(at, at, observed, "fourth", .60)
	d, saved := track.detection(true)
	if !saved || d.Reading.Number != "1234" || !d.At.Equal(first) || !d.Track.LastSeen.Equal(at) {
		t.Fatalf("later frame did not corroborate the serial and the interval: %+v", d)
	}
}

func TestNumberWithGlareNeedsAFrameWithVisibleDigitStrokes(t *testing.T) {
	first := time.Unix(100, 0)
	track := &plateTrack{first: first}
	observed := verifiedPlateForRejoin("1234", [4]int{850, 480, 46, 26})
	observed.Localized = &plate.Reading{Number: "1234", Confidence: .91}
	edges := .28
	observed.DigitEdges = &edges
	observed.appearance = digitBandWithGlare()
	for i := range 3 {
		at := first.Add(time.Duration(i) * 125 * time.Millisecond)
		track.observe(at, at, observed, strconv.Itoa(i), .60)
	}
	if _, saved := track.detection(true); saved {
		t.Fatal("repeated reads through glare were published")
	}

	observed.appearance = digitBandWithGlare()
	for row := 17; row < 23; row++ {
		observed.appearance[row*appearanceWidth+28] = 70
	}
	at := first.Add(375 * time.Millisecond)
	track.observe(at, at, observed, "3", .60)
	d, saved := track.detection(true)
	if !saved || d.Reading.Number != "1234" || !d.At.Equal(first) || !d.Track.LastSeen.Equal(at) {
		t.Fatalf("visible digit strokes did not corroborate the serial and the interval: %+v", d)
	}
}

func digitBandWithGlare() []byte {
	view := make([]byte, appearanceWidth*appearanceHeight)
	for i := range view {
		view[i] = 205
	}
	for _, x := range [...]int{17, 27, 31} {
		for row := 14; row < 17; row++ {
			for col := x; col < x+4; col++ {
				view[row*appearanceWidth+col] = 255
			}
		}
	}
	for row := 17; row < 21; row++ {
		for col := 27; col < 31; col++ {
			view[row*appearanceWidth+col] = 255
		}
	}
	return view
}

func TestInterleavedRejoinNeedsDistinctViewsAtBothTransitions(t *testing.T) {
	same, different := makeAppearanceForTest(87), makeAppearanceForTest(88)
	for _, test := range []struct {
		name          string
		first, second []byte
		simultaneous  bool
		wantJoin      bool
	}{
		{"same vehicle", same, same, false, true},
		{"different vehicle", different, different, false, false},
		{"only one transition matches", same, different, false, false},
		{"same-frame observations", same, same, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			start := time.Unix(100, 0)
			older, newer := &plateTrack{first: start}, &plateTrack{first: start.Add(625 * time.Millisecond)}
			read := verifiedPlateForRejoin("123", [4]int{850, 475, 59, 30})
			observe := func(track *plateTrack, ms int, p EnginePlate, frame string) {
				at := start.Add(time.Duration(ms) * time.Millisecond)
				track.observe(at, at, p, frame, .60)
			}
			for i, frame := range []string{"1", "2", "3"} {
				observe(older, i*125, read, frame)
			}
			if _, ok := older.detection(true); !ok {
				t.Fatal("the earlier verified track did not publish")
			}
			weak := EnginePlate{Box: [4]int{860, 470, 28, 15}, Score: .35, continuityView: same}
			observe(older, 500, weak, "4")
			starting := EnginePlate{Box: [4]int{862, 479, 28, 13}, Score: .35, continuityView: test.first}
			observe(newer, 625, starting, "5")
			starting.continuityView = test.second
			observe(newer, 750, starting, "6")
			if test.simultaneous {
				observe(older, 625, weak, "5")
				observe(older, 750, weak, "6")
			}
			observe(older, 1000, weak, "7")
			for i, frame := range []string{"8", "9", "10"} {
				observe(newer, 1125+i*125, read, frame)
			}
			older.misses = 3
			tracks := rejoinTracks([]*plateTrack{older, newer}, nil)
			if joined := len(tracks) == 1; joined != test.wantJoin {
				t.Fatalf("joined = %v, want %v", joined, test.wantJoin)
			}
			if test.wantJoin && (tracks[0] != older || older.frames != 10 || !older.last.Equal(newer.last)) {
				t.Fatalf("join lost the earlier ID, distinct observations, or end time: %d frames", older.frames)
			}
		})
	}
}

func TestInwardReadNeedsThreeCorroboratingViews(t *testing.T) {
	start := time.Unix(100, 0)
	vehicle := makeAppearanceForTest(79)
	const accepted, competing = "5273", "5279"
	for _, test := range []struct {
		name                string
		missingLocalization bool
		middleLocalization  bool
		differentVehicle    bool
		verifiedCompetitor  bool
		want                bool
	}{
		{name: "three views", want: true},
		{name: "the first two views have localized digits", middleLocalization: true, want: true},
		{name: "one localized view", missingLocalization: true},
		{name: "intervening view is another vehicle", differentVehicle: true},
		{name: "independently verified competing read", verifiedCompetitor: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			boxes := [][4]int{{305, 487, 54, 47}, {440, 476, 51, 33}, {490, 471, 48, 30}}
			times := []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, 625 * time.Millisecond}
			var views []trackFrame
			for i, box := range boxes {
				p := verifiedPlateForRejoin(accepted, box)
				p.Localized = p.Verifier
				p.appearance = vehicle
				if i == 1 {
					if !test.middleLocalization {
						p.Localized = nil
					}
					if test.differentVehicle {
						p.appearance = makeAppearanceForTest(80)
					}
				}
				if i == 2 && (test.missingLocalization || test.middleLocalization) {
					p.Localized = nil
				}
				at := start.Add(times[i])
				views = append(views, trackFrame{at: at, finished: at, plate: p, id: strconv.Itoa(i + 3), minConfidence: .60})
			}
			best := observation{reading: views[2].plate.Reading, box: boxes[2], frame: views[2].id,
				at: views[2].at, detector: .67, appearance: vehicle}
			alternative := serialEvidence{support: 2}
			if test.verifiedCompetitor {
				alternative.narrowSupport = 1
			}
			tr := &plateTrack{id: "test", first: start, firstBox: [4]int{145, 508, 46, 57},
				last: start.Add(750 * time.Millisecond), frames: 6, support: 4, reads: 6, best: best, recentFrames: views,
				conflict: true, ambiguous: true, hasVerification: true, localized: map[string]float64{accepted: .90},
				votes: map[string]serialEvidence{accepted: {support: 4, narrowSupport: 2}, competing: alternative}}
			if d, ok := tr.detection(false); ok {
				t.Fatalf("ambiguous track published before final review: %+v", d)
			}
			if d, ok := tr.detection(true); ok != test.want || ok && (d.Reading.Number != accepted || d.Track.Ambiguous) {
				t.Fatalf("final detection returned %+v, %v, want accepted=%v", d, ok, test.want)
			}
		})
	}
}

func TestRejoinAfterLongGapRequiresVehicleAppearance(t *testing.T) {
	first := time.Now()
	car, otherCar := makeAppearanceForTest(1), makeAppearanceForTest(2)
	for _, tc := range []struct {
		name       string
		appearance []byte
		join       bool
	}{
		{"consistent vehicle appearance", car, true},
		{"different vehicle with the same serial", otherCar, false},
		{"missing camera image", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := &plateTrack{first: first}
			newer := &plateTrack{first: first.Add(24 * time.Second)}
			prior := verifiedPlateForRejoin("1234", [4]int{730, 494, 90, 45})
			prior.appearance = car
			following := verifiedPlateForRejoin("1234", [4]int{751, 496, 90, 45})
			following.appearance = tc.appearance
			for i := range 3 {
				offset := time.Duration(i) * 125 * time.Millisecond
				a, b := old.first.Add(offset), newer.first.Add(offset)
				old.observe(a, a, prior, "older", .60)
				newer.observe(b, b, following, "newer", .60)
			}
			old.detection(false)
			last := newer.last
			joined := rejoinTracks([]*plateTrack{newer}, []*plateTrack{old})
			if got := joined[0] == old; got != tc.join {
				t.Fatalf("rejoined = %v, want %v", got, tc.join)
			}
			if tc.join && (!old.first.Equal(first) || !old.last.Equal(last) || old.frames != 6) {
				t.Fatalf("rejoin lost the original interval or evidence: %v through %v, %d frames", old.first, old.last, old.frames)
			}
		})
	}
}

func TestShortInwardRejoinNeedsMotionAndTwoVehicleViews(t *testing.T) {
	car, other := makeAppearanceForTest(61), makeAppearanceForTest(62)
	for _, tc := range []struct {
		name               string
		initial, following []byte
		sequence           string
		endX               int
		wantJoin           bool
	}{
		{"same vehicle on one trajectory", car, car, "9", 619, true},
		{"different vehicle with the same serial", other, other, "9", 619, false},
		{"different vehicle arrives on the later frame", car, other, "9", 619, false},
		{"second read lacks sequence proof", car, car, "", 619, false},
		{"reversed motion", car, car, "9", 571, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Unix(100, 0)
			first := &plateTrack{first: at}
			initial := verifiedPlateForRejoin("9", [4]int{284, 519, 60, 41})
			initial.Reading.Confidence, initial.Verifier.Confidence = .78, .88
			initial.Localized = &plate.Reading{Number: "9", Confidence: .93}
			initial.appearance = car
			first.observe(at, at, initial, "1", .60)

			secondAt := at.Add(125 * time.Millisecond)
			second := &plateTrack{first: secondAt}
			near := verifiedPlateForRejoin("9", [4]int{394, 502, 58, 35})
			near.Reading.Confidence, near.Verifier.Confidence = .64, .84
			near.SequenceNumber, near.NumberScores["9"] = "", .68
			near.appearance = tc.initial
			second.observe(secondAt, secondAt, near, "2", .60)

			laterAt := at.Add(500 * time.Millisecond)
			later := &plateTrack{first: laterAt}
			follow := verifiedPlateForRejoin("9", [4]int{575, 487, 45, 22})
			follow.Reading.Confidence, follow.Verifier.Confidence = .67, .79
			follow.SequenceNumber = tc.sequence
			follow.appearance = tc.following
			later.observe(laterAt, laterAt, follow, "3", .60)
			second.join(later)

			end := at.Add(625 * time.Millisecond)
			second.observe(end, end, EnginePlate{Box: [4]int{tc.endX, 481, 49, 21}}, "4", .60)
			tracks := rejoinTracks([]*plateTrack{second}, []*plateTrack{first})
			if got := len(tracks) == 1 && tracks[0] == first; got != tc.wantJoin {
				t.Fatalf("rejoined = %v, want %v", got, tc.wantJoin)
			}
			if tc.wantJoin && (!first.first.Equal(at) || !first.last.Equal(end) || first.frames != 4 || first.support != 3) {
				t.Fatal("rejoin lost the first time, last time or distinct frame evidence")
			}
		})
	}
}

func TestRejoinSameFrameCropsOnlyCountsEachFrameOnce(t *testing.T) {
	start := time.Now()
	for _, tc := range []struct {
		name  string
		shift int
		join  bool
	}{
		{"the same bounding boxes", 4, true},
		{"distinct plates in the same frames", 61, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			older := &plateTrack{first: start}
			newer := &plateTrack{first: start.Add(250 * time.Millisecond)}
			p := verifiedPlateForRejoin("1234", [4]int{800, 480, 75, 39})
			for i := range 8 {
				at := start.Add(time.Duration(i) * 125 * time.Millisecond)
				older.observe(at, at, p, "prior", .60)
			}
			older.misses = 3
			p.Box[0] += tc.shift
			for i := range 9 {
				at := newer.first.Add(time.Duration(i) * 125 * time.Millisecond)
				newer.observe(at, at, p, "following", .60)
			}
			joined := rejoinTracks([]*plateTrack{newer}, []*plateTrack{older})
			if got := joined[0] == older; got != tc.join {
				t.Fatalf("rejoined = %v, want %v", got, tc.join)
			}
			if tc.join && (older.frames != 11 || older.support != 11 || !older.last.Equal(newer.last)) {
				t.Fatalf("overlap counted more than once: %d frames, %d reads, last %v", older.frames, older.support, older.last)
			}
		})
	}
}

func TestSingleReadInwardPassRequiresVerifiedSequence(t *testing.T) {
	start := time.Now()
	for _, tc := range []struct {
		name      string
		confirmed bool
		reversed  bool
		join      bool
	}{
		{"verified continuing motion", true, false, true},
		{"direction changes", true, true, false},
		{"independent read missing", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			older := &plateTrack{first: start}
			anchor := verifiedPlateForRejoin("123", [4]int{241, 492, 50, 45})
			if !tc.confirmed {
				anchor.SequenceNumber = ""
			}
			older.observe(start, start, anchor, "before", .60)
			newer := &plateTrack{first: start.Add(125 * time.Millisecond)}
			boxes := [][4]int{{360, 483, 50, 39}, {423, 478, 55, 36}, {486, 475, 50, 30}}
			if tc.reversed {
				boxes[2][0] = 322
			}
			for i, box := range boxes {
				at := newer.first.Add(time.Duration(i) * 125 * time.Millisecond)
				newer.observe(at, at, verifiedPlateForRejoin("123", box), "after", .60)
			}
			if got := older.canRejoin(newer); got != tc.join {
				t.Fatalf("rejoined = %v, want %v", got, tc.join)
			}
		})
	}
}

func TestLongerInwardGapRequiresStrongReadsAndContinuingMotion(t *testing.T) {
	start := time.Unix(100, 0)
	for _, tc := range []struct {
		name       string
		confidence float64
		end        int
		join       bool
	}{
		{"strong read and continued movement", .93, 646, true},
		{"lower confidence at the first read", .89, 646, false},
		{"vehicle reverses direction", .93, 586, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			older := &plateTrack{first: start}
			anchor := verifiedPlateForRejoin("123", [4]int{230, 490, 50, 44})
			anchor.Reading.Confidence = tc.confidence
			anchor.Verifier = &anchor.Reading
			older.observe(start, start, anchor, "initial", .60)
			newer := &plateTrack{first: start.Add(250 * time.Millisecond)}
			boxes := [][4]int{{407, 480, 54, 36}, {628, 468, 42, 22}, {tc.end, 467, 35, 20}}
			delays := []time.Duration{0, 625 * time.Millisecond, 750 * time.Millisecond}
			for i, box := range boxes {
				at := newer.first.Add(delays[i])
				newer.observe(at, at, verifiedPlateForRejoin("123", box), "continuation", .60)
			}
			if got := older.canRejoin(newer); got != tc.join {
				t.Fatalf("rejoined = %v, want %v", got, tc.join)
			}
		})
	}
}

func TestLateRejoinCanWaitForAPlateAtADifferentScale(t *testing.T) {
	old := observation{reading: plate.Reading{Number: "1234", Confidence: .95}, box: [4]int{869, 485, 120, 65}}
	near := observation{reading: old.reading, box: [4]int{909, 485, 40, 21}}
	far := near
	far.box[0] += 450
	if stationaryDistance(old.box, near.box) < 3.4 {
		t.Fatal("test crop should fall outside the size ratio for a direct join")
	}
	if !canWaitAtScale(old, near) || canWaitAtScale(old, far) {
		t.Fatal("publication wait should apply only to a nearby candidate")
	}
}

func TestLongAppearanceEvidenceRequiresDifferentFramesOnEachSide(t *testing.T) {
	at := time.Now()
	image := makeAppearanceForTest(3)
	older := observation{reading: plate.Reading{Number: "1234", Confidence: .94}, box: [4]int{800, 500, 90, 42}, appearance: image, at: at}
	newer := older
	newer.at = at.Add(100 * time.Second)
	old := &plateTrack{best: older, support: 6, appearanceSamples: []observation{older}}
	new := &plateTrack{best: newer, support: 6, appearanceSamples: []observation{newer}}
	old.appearanceSamples = append(old.appearanceSamples, older)
	new.appearanceSamples = append(new.appearanceSamples, newer)
	if old.rejoinWithTwoViews(new, 100*time.Second) {
		t.Fatal("repeated copies of a single frame qualified as independent images")
	}
	old.appearanceSamples[1].at = older.at.Add(time.Second)
	new.appearanceSamples[1].at = newer.at.Add(time.Second)
	if !old.rejoinWithTwoViews(new, 100*time.Second) {
		t.Fatal("independent matching images on both sides did not qualify")
	}
}

func TestModerateConfidenceContinuationRequiresRepeatedProofAndImages(t *testing.T) {
	start := time.Unix(100, 0)
	car, differentCar := makeAppearanceForTest(4), makeAppearanceForTest(5)
	for _, tc := range []struct {
		name     string
		image    []byte
		frames   int
		oneImage bool
		want     bool
	}{
		{"verified repeated continuation", car, 12, false, true},
		{"one image on the old track", car, 12, true, false},
		{"weak support on the old track", car, 8, false, false},
		{"same number on a different vehicle", differentCar, 12, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := &plateTrack{first: start}
			newer := &plateTrack{first: start.Add(80 * time.Second)}
			prior := verifiedPlateForRejoin("1234", [4]int{730, 494, 90, 45})
			prior.Reading.Confidence = .72
			prior.Verifier = &prior.Reading
			prior.appearance = car
			following := verifiedPlateForRejoin("1234", prior.Box)
			following.appearance = tc.image
			for i := range 12 {
				offset := time.Duration(i) * 150 * time.Millisecond
				if i < tc.frames {
					at := old.first.Add(offset)
					old.observe(at, at, prior, "older", .60)
				}
				at := newer.first.Add(offset)
				newer.observe(at, at, following, "newer", .60)
			}
			if tc.oneImage {
				old.appearanceSamples = old.appearanceSamples[:1]
			}
			if got := old.canRejoin(newer); got != tc.want {
				t.Fatalf("rejoined = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLongAppearanceFindsIndependentViewsBeyondTheFirstMatch(t *testing.T) {
	at := time.Unix(100, 0)
	base, contrast := makeAppearanceForTest(6), makeAppearanceForTest(7)
	center, light, dark := make([]byte, len(base)), make([]byte, len(base)), make([]byte, len(base))
	for i, b := range base {
		x, y := .6*float64(int(b)-128), .26*float64(int(contrast[i])-128)
		center[i], light[i], dark[i] = byte(128+x), byte(128+.90*x+y), byte(128+.90*x-y)
	}
	view := func(at time.Time, image []byte) observation {
		return observation{at: at, appearance: image, box: [4]int{730, 494, 90, 45},
			reading: plate.Reading{Number: "1234", Confidence: .94}}
	}
	old := &plateTrack{support: 10, best: view(at, light), appearanceSamples: []observation{
		view(at, light), view(at.Add(time.Second), dark),
	}}
	newAt := at.Add(80 * time.Second)
	newer := &plateTrack{support: 10, best: view(newAt, center), appearanceSamples: []observation{
		view(newAt, center), view(newAt.Add(time.Second), light),
	}}
	if !old.rejoinWithTwoViews(newer, 80*time.Second) {
		t.Fatal("first image match prevented choosing two independent pairs")
	}
	newer.appearanceSamples = newer.appearanceSamples[:1]
	if old.rejoinWithTwoViews(newer, 80*time.Second) {
		t.Fatal("more matches to a single image qualified as independent")
	}
}

func TestRejoinInChangedLightRequiresRepeatedKanaAndVehicleViews(t *testing.T) {
	before, noise := makeAppearanceForTest(8), makeAppearanceForTest(9)
	after := append([]byte(nil), before...)
	copy(after[:8*appearanceWidth], noise)
	box := [4]int{800, 500, 60, 35}
	start := time.Unix(100, 0)
	for _, tc := range []struct {
		name string
		edit func(*plateTrack)
		gap  time.Duration
		want bool
	}{
		{name: "same kana read on both sides", gap: 38500 * time.Millisecond, want: true},
		{name: "competing kana on a separate image", gap: 38500 * time.Millisecond, edit: func(track *plateTrack) {
			track.appearanceSamples[1].reading.Kana = "き"
		}},
		{name: "rereading the same frame cannot confirm kana", gap: 38500 * time.Millisecond, edit: func(track *plateTrack) {
			track.appearanceSamples[1].at = track.appearanceSamples[0].at
		}},
		{name: "vehicle is too far from the earlier position", gap: 38500 * time.Millisecond, edit: func(track *plateTrack) {
			for i := range track.appearanceSamples {
				track.appearanceSamples[i].box[0] += 42
			}
			track.best = track.appearanceSamples[0]
		}},
		{name: "unverified alternate digits", gap: 38500 * time.Millisecond, edit: func(track *plateTrack) {
			track.localized["1234"] = .86
		}},
		{name: "longer absence", gap: 46 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := makeVerifiedAppearanceTrack(start, before, box)
			newer := makeVerifiedAppearanceTrack(old.last.Add(tc.gap), after, box)
			if tc.edit != nil {
				tc.edit(newer)
			}
			if got := old.canRejoin(newer); got != tc.want {
				t.Fatalf("rejoined = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLongRejoinRejectsAppearanceDominatedByGlare(t *testing.T) {
	image := makeAppearanceForTest(10)
	for i := 13 * appearanceWidth; i < len(image); i++ {
		image[i] = 250
	}
	box := [4]int{800, 500, 60, 35}
	older := makeVerifiedAppearanceTrack(time.Unix(100, 0), image, box)
	newer := makeVerifiedAppearanceTrack(older.last.Add(38500*time.Millisecond), image, box)
	if older.canRejoin(newer) {
		t.Fatal("glare alone qualified as a long-gap vehicle match")
	}
	newer = makeVerifiedAppearanceTrack(older.last.Add(2*time.Second), image, box)
	if !older.canRejoin(newer) {
		t.Fatal("short-gap vehicle match was lost")
	}
}

func makeVerifiedAppearanceTrack(at time.Time, pixels []byte, box [4]int) *plateTrack {
	const serial = "1234"
	p := verifiedPlateForRejoin(serial, box)
	p.Reading.Kana, p.appearance = "か", pixels
	p.Localized = &plate.Reading{Number: serial, Confidence: .93}
	track := &plateTrack{first: at}
	for i := range 12 {
		seen := at.Add(time.Duration(i) * 250 * time.Millisecond)
		track.observe(seen, seen, p, seen.String(), .60)
	}
	return track
}
