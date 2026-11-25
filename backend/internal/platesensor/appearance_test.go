package platesensor

import (
	"math/rand/v2"
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
