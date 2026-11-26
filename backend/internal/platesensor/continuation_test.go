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

func TestShortFollowingWithMissingFieldsRequiresRepeatedDigitEvidence(t *testing.T) {
	const number = "531"
	observe := func(start time.Time, p EnginePlate, count int) *plateTrack {
		p.Localized = &plate.Reading{Number: p.Reading.Number, Confidence: .95}
		track := &plateTrack{first: start}
		for i := range count {
			sample := p
			if i >= 3 && i <= 5 && p.Reading.Class != "" {
				sample.Box[2] = plate.FieldMinWidth
			}
			at := start.Add(time.Duration(i) * 250 * time.Millisecond)
			track.observe(at, at, sample, strconv.Itoa(i), .60)
		}
		return track
	}
	for _, tc := range []struct {
		name            string
		gap             time.Duration
		changedNumber   string
		conflicting     bool
		tooFewFrames    bool
		singleDigitRead bool
		farther         bool
		joined          bool
	}{
		{name: "same serial and nearby return", gap: 20 * time.Second, joined: true},
		{name: "classification contradicts", gap: 20 * time.Second, conflicting: true},
		{name: "different serial", gap: 20 * time.Second, changedNumber: "581"},
		{name: "brief return", gap: 20 * time.Second, tooFewFrames: true},
		{name: "one independent digit check", gap: 20 * time.Second, singleDigitRead: true},
		{name: "separate positions", gap: 20 * time.Second, farther: true},
		{name: "new encounter much later", gap: 15 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Unix(100, 0)
			previous := verifiedPlateForRejoin(number, [4]int{860, 525, 58, 33})
			reading := number
			if tc.changedNumber != "" {
				reading = tc.changedNumber
			}
			current := verifiedPlateForRejoin(reading, [4]int{922, 532, 61, 35})
			if tc.conflicting {
				previous.Reading.Class, current.Reading.Class = "351", "751"
			}
			if tc.farther {
				current.Box[0] += 170
			}
			old := observe(start, previous, 40)
			old.sent = old.frames
			count := 10
			if tc.tooFewFrames {
				count = 6
			}
			next := observe(old.last.Add(tc.gap), current, count)
			if tc.conflicting {
				if _, conflicts := old.compareConfirmedFields(next); conflicts != 1 {
					t.Fatal("expected one confirmed classification conflict")
				}
			}
			if tc.singleDigitRead {
				next.localizedFrames[number] = [2]string{"0"}
			}
			from, until := old.first, next.last
			_, joined := rejoinTracksOnce([]*plateTrack{next}, []*plateTrack{old})
			if joined != tc.joined {
				t.Fatalf("continuation = %v, want %v", joined, tc.joined)
			}
			if joined && (!old.first.Equal(from) || !old.last.Equal(until) || old.frames != 50) {
				t.Fatal("following continuation lost part of the original interval")
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

func TestTurnReturnRequiresIndependentReadsAndCompatibleMovement(t *testing.T) {
	const number = "1234"
	for _, tc := range []struct {
		name             string
		gap              time.Duration
		move             int
		differentClass   bool
		withoutDigitRead bool
		twoCandidates    bool
		want             bool
	}{
		{name: "verified return across the frame", gap: 77 * time.Second, move: -18, want: true},
		{name: "two possible returning vehicles", gap: 77 * time.Second, move: -18, twoCandidates: true, want: true},
		{name: "moving away from the center", gap: 77 * time.Second, move: 12},
		{name: "different confirmed classification", gap: 77 * time.Second, move: -18, differentClass: true},
		{name: "no independent digit observation", gap: 77 * time.Second, move: -18, withoutDigitRead: true},
		{name: "number returns much later", gap: 15 * time.Minute, move: -18},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := verifiedPlateForRejoin(number, [4]int{920, 504, 82, 43})
			first.Reading.Confidence, first.Reading.Class = .96, "505"
			first.Localized = &plate.Reading{Number: number, Confidence: .97}
			first.NumberScores[number] = .99
			old := observeFollowingTestTrack(time.Unix(100, 0), first, 42)
			old.sent, old.box = old.frames, [4]int{420, 491, 72, 40}
			if !old.hasStrongTurnRead() {
				t.Fatal("outgoing fixture does not meet the strong-read prerequisite")
			}
			start := old.last.Add(tc.gap)
			next := &plateTrack{first: start}
			for i := range 10 {
				nextAt := start.Add(time.Duration(i) * 125 * time.Millisecond)
				p := first
				p.Box[0] = 1572 + tc.move*i
				if tc.differentClass {
					p.Reading.Class = "301"
				}
				if tc.withoutDigitRead {
					p.Localized = nil
				}
				next.observe(nextAt, nextAt, p, strconv.Itoa(i+100), .60)
			}
			if tc.differentClass {
				if _, conflicts := old.compareConfirmedFields(next); conflicts == 0 {
					t.Fatal("the negative fixture does not contain confirmed conflicting fields")
				}
			}
			if got := old.rejoinAfterTurn(next, tc.gap); got != tc.want {
				t.Fatalf("turn continuation = %t, want %t", got, tc.want)
			}
			if tc.twoCandidates {
				alternate := *next
				alternate.firstBox[0] += 100
				alternate.box[0] += 100
				if !old.rejoinAfterTurn(&alternate, tc.gap) {
					t.Fatal("the second candidate must qualify on its own")
				}
				recent := []*plateTrack{old}
				active := rejoinTracks([]*plateTrack{next, &alternate}, recent)
				if len(active) != 2 || recent[0] != old {
					t.Fatal("a track with two possible successors must remain separate")
				}
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
	if !matchingTrackPoses("1234", first, last, .80, .80) {
		t.Fatal("two distinct pairs should corroborate the returned plate")
	}
	if matchingTrackPoses("1234", first, last[:1], .80, .80) || matchingTrackPoses("1234", first[:1], last, .80, .80) {
		t.Fatal("reusing the same frame should not establish continuity")
	}
	last[1].plate.appearance = makeAppearanceForTest(145)
	if matchingTrackPoses("1234", first, last, .80, .80) {
		t.Fatal("a conflicting vehicle view should not supply the second pair")
	}
}

func TestWeakSideFragmentHasShortRetentionAndNeedsFreshProof(t *testing.T) {
	box := [4]int{1642, 496, 53, 44}
	read := verifiedPlateForRejoin("1234", box)
	read.appearance = makeAppearanceForTest(82)
	old := observeFollowingTestTrack(time.Unix(100, 0), read, 5)
	old.sent = old.frames
	old.best.reading.Confidence = .67
	newer := observeFollowingTestTrack(old.last.Add(14*time.Second), read, 5)
	if !old.canRemainRecent() || trackRetention(old) != trackRecentGap || !old.rejoinWeakSideFragment(newer, 14*time.Second) {
		t.Fatal("verified side fragment should rejoin within its short retention")
	}
	if old.rejoinWeakSideFragment(newer, trackRecentGap+time.Second) {
		t.Fatal("the weak fragment must expire at the short retention limit")
	}
	newer.localizedFrames["1234"] = [2]string{"one"}
	if old.rejoinWeakSideFragment(newer, 14*time.Second) {
		t.Fatal("a single localized frame of the returned car must not rejoin a weak fragment")
	}
	old.localized["1234"] = .70
	if old.canRemainRecent() {
		t.Fatal("weak evidence without corroborated digits must not survive")
	}
}

func TestLongFollowingRequiresEstablishedTrackAndMatchingViews(t *testing.T) {
	read := verifiedPlateForRejoin("1234", [4]int{905, 489, 65, 40})
	read.appearance = makeAppearanceForTest(81)
	start := time.Unix(100, 0)
	gap := 215 * time.Second
	old := observeFollowingTestTrack(start, read, 100)
	old.sent = old.frames
	returned := observeFollowingTestTrack(old.last.Add(gap), read, 10)
	if trackRetention(old) != longFollowingGap || !old.canRejoin(returned) {
		t.Fatal("established lead vehicle should rejoin when separately read views match")
	}
	if retain := trackRetention(observeFollowingTestTrack(start, read, 25)); retain == longFollowingGap {
		t.Fatal("a short fragment must not qualify for longer retention")
	}
	read.appearance = makeAppearanceForTest(145)
	foreign := observeFollowingTestTrack(old.last.Add(gap), read, 10)
	if old.canRejoin(foreign) {
		t.Fatal("a different vehicle view must not rejoin despite the same serial")
	}
	late := observeFollowingTestTrack(old.last.Add(15*time.Minute), read, 10)
	if old.canRejoin(late) {
		t.Fatal("a later encounter must not rejoin the original lead vehicle")
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
