package platesensor

import (
	"strconv"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

func TestFollowingJoinsOnlyCorroboratedSerials(t *testing.T) {
	position := [4]int{860, 565, 85, 46}
	at := time.Unix(100, 0)
	oldImage, nextImage := makeAppearanceForTest(77), makeAppearanceForTest(79)
	for _, tc := range []struct {
		name   string
		gap    time.Duration
		edit   func(previous, next *EnginePlate)
		joined bool
	}{
		{name: "continued despite different lighting", gap: 138 * time.Second, joined: true},
		{name: "one weak field can differ", gap: 40 * time.Second, joined: true, edit: func(a, b *EnginePlate) {
			a.Reading.Kana, b.Reading.Kana = "あ", "か"
		}},
		{name: "different well-read metadata", gap: 40 * time.Second, edit: func(a, b *EnginePlate) {
			a.Reading.Region, b.Reading.Region = "横浜", "相模"
			a.Reading.Class, b.Reading.Class = "505", "300"
		}},
		{name: "late return", gap: 15 * time.Minute},
		{name: "different position", gap: 40 * time.Second, edit: func(_, b *EnginePlate) {
			b.Box[0] = 1500
		}},
		{name: "number differs by one digit", gap: 40 * time.Second, edit: func(_, b *EnginePlate) {
			box := b.Box
			*b = verifiedPlateForRejoin("1239", box)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous := verifiedPlateForRejoin("1234", position)
			next := verifiedPlateForRejoin("1234", position)
			if tc.edit != nil {
				tc.edit(&previous, &next)
			}
			previous.appearance, next.appearance = oldImage, nextImage
			old := observeFollowingTestTrack(at, previous, 25)
			old.sent = old.frames
			following := observeFollowingTestTrack(old.last.Add(tc.gap), next, 5)
			if got := old.canRejoin(following); got != tc.joined {
				t.Fatalf("continuation = %v, want %v", got, tc.joined)
			}
			if previous.Reading.Number != next.Reading.Number && !waitingForAppearance(following, nil, []*plateTrack{old}) {
				t.Fatal("published an uncertain initial reading without giving later OCR a chance")
			}
		})
	}
}

func TestFollowingKeepsLegibleFieldsWhenLaterFramesAreClipped(t *testing.T) {
	box := [4]int{860, 565, 85, 46}
	at := time.Unix(100, 0)
	clear := verifiedPlateForRejoin("4112", box)
	clear.appearance = makeAppearanceForTest(51)
	clear.Reading.Region, clear.Reading.Class, clear.Reading.Kana = "横浜", "505", "ら"
	old := observeFollowingTestTrack(at, clear, 25)
	old.sent = old.frames
	glare := clear
	glare.Reading.Kana = "ち"
	glare.appearance = append([]byte(nil), clear.appearance...)
	for row := 19; row < 24; row++ {
		for col := 13; col < 17; col++ {
			glare.appearance[row*appearanceWidth+col] = 245
		}
	}
	later := observeFollowingTestTrack(old.last.Add(43*time.Second), glare, 100)
	if !old.canRejoin(later) {
		t.Fatal("clipping prevented a continuation with the same verified serial")
	}
	old.join(later)
	d, ok := old.detection(true)
	if !ok || d.Reading.Region != "横浜" || d.Reading.Class != "505" || d.Reading.Kana != "ら" ||
		d.Track.Frames != 125 || !d.At.Equal(at) || !d.Track.LastSeen.Equal(later.last) {
		t.Fatalf("merged detection = %+v", d)
	}
}

func TestFollowingCanFinishOutsideTheForwardView(t *testing.T) {
	for _, tc := range []struct {
		name       string
		gap        time.Duration
		startX     int
		otherClass bool
		joined     bool
	}{
		{name: "resumes ahead and moves to the side", gap: 26 * time.Second, startX: 806, joined: true},
		{name: "first appears far to the side", gap: 26 * time.Second, startX: 388},
		{name: "different confirmed classification", gap: 26 * time.Second, startX: 806, otherClass: true},
		{name: "a later encounter", gap: 15 * time.Minute, startX: 806},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Unix(100, 0)
			first := verifiedPlateForRejoin("1234", [4]int{900, 515, 72, 47})
			first.Reading.Class = "505"
			old := observeFollowingTestTrack(start, first, 25)
			old.sent = old.frames
			firstAt := old.last.Add(tc.gap)
			resumed := &plateTrack{first: firstAt}
			resumed.observe(firstAt, firstAt, EnginePlate{Box: [4]int{tc.startX, 482, 45, 30}}, "opening", .60)
			for i := range 7 {
				p := verifiedPlateForRejoin("1234", [4]int{390 - 15*i, 524, 72, 48})
				p.Localized = &plate.Reading{Number: "1234", Confidence: .95}
				p.Reading.Class = "505"
				if tc.otherClass {
					p.Reading.Class = "300"
				}
				seen := firstAt.Add(time.Duration(i+1) * 125 * time.Millisecond)
				resumed.observe(seen, seen, p, strconv.Itoa(i+25), .60)
			}
			if joined := old.canRejoin(resumed); joined != tc.joined {
				t.Fatalf("continuation = %v, want %v", joined, tc.joined)
			}
		})
	}
}

func TestReadRejoinsAfterDistantBoxes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		gap      time.Duration
		firstX   int
		firstCTC string
		want     bool
	}{
		{"two independent confirmations", 9 * time.Second, 1148, "1234", true},
		{"one confirmation", 9 * time.Second, 1148, "1239", false},
		{"distant opening box", 9 * time.Second, 1270, "1234", false},
		{"new encounter", 20 * time.Second, 1148, "1234", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Unix(100, 0)
			old := observeFollowingTestTrack(start, verifiedPlateForRejoin("1234", [4]int{1410, 529, 57, 35}), 12)
			old.box, old.sent = [4]int{1105, 478, 26, 15}, old.frames
			at := old.last.Add(tc.gap)
			next := &plateTrack{first: at}
			next.observe(at, at, EnginePlate{Box: [4]int{tc.firstX, 492, 28, 16}}, "start", .60)
			weak := EnginePlate{Box: [4]int{1380, 529, 54, 32}, Score: .72,
				Reading:   plate.Reading{Number: "4234", Confidence: .56},
				Verifier:  &plate.Reading{Number: tc.firstCTC, Confidence: .56},
				Localized: &plate.Reading{Number: tc.firstCTC, Confidence: .85}, SequenceNumber: tc.firstCTC,
				NumberScores: map[string]float64{tc.firstCTC: .94}}
			strong := EnginePlate{Box: [4]int{1420, 537, 56, 32}, Score: .72,
				Reading:   plate.Reading{Number: "1234", Confidence: .94},
				Verifier:  &plate.Reading{Number: "1234", Confidence: .59},
				Localized: &plate.Reading{Number: "1234", Confidence: .97}, SequenceNumber: "1234",
				NumberScores: map[string]float64{"1234": .98}}
			for i, p := range []EnginePlate{weak, strong} {
				seen := at.Add(time.Duration(i+1) * 125 * time.Millisecond)
				next.observe(seen, seen, p, strconv.Itoa(i+20), .60)
			}
			if next.numberVerified() || next.support != 1 {
				t.Fatal("the isolated second fragment should still lack enough evidence for its own record")
			}
			if joined := old.canRejoin(next); joined != tc.want {
				t.Fatalf("continuation = %v, want %v", joined, tc.want)
			}
		})
	}
}

func TestReturningAtEdgeNeedsTwoPairsFromDistinctFrames(t *testing.T) {
	now := time.Unix(100, 0)
	a := verifiedPlateForRejoin("1234", [4]int{1569, 510, 55, 47})
	a.Localized = &plate.Reading{Number: "1234", Confidence: .95}
	a.appearance = makeAppearanceForTest(82)
	b := a
	b.Box = [4]int{1550, 507, 52, 42}
	first := []trackFrame{{at: now, plate: a}, {at: now.Add(125 * time.Millisecond), plate: b}}
	last := []trackFrame{{at: now.Add(time.Minute), plate: a}, {at: now.Add(time.Minute + 125*time.Millisecond), plate: b}}
	if !matchingEdgePoses("1234", first, last) {
		t.Fatal("two distinct pairs should corroborate the returned plate")
	}
	if matchingEdgePoses("1234", first, last[:1]) || matchingEdgePoses("1234", first[:1], last) {
		t.Fatal("reusing the same frame should not establish continuity")
	}
	last[1].plate.appearance = makeAppearanceForTest(145)
	if matchingEdgePoses("1234", first, last) {
		t.Fatal("a conflicting vehicle view should not supply the second pair")
	}
}

func observeFollowingTestTrack(at time.Time, p EnginePlate, count int) *plateTrack {
	p.Localized = &plate.Reading{Number: p.Reading.Number, Confidence: .95}
	result := &plateTrack{first: at}
	for i := range count {
		seen := at.Add(time.Duration(i) * 250 * time.Millisecond)
		result.observe(seen, seen, p, strconv.Itoa(i), .60)
	}
	return result
}
