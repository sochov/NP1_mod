package platesensor

import (
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

func TestFastSingleFrameRejoinUsesIndependentReadsAndAppearance(t *testing.T) {
	car, otherCar := makeAppearanceForTest(31), makeAppearanceForTest(32)
	noisyCar := make([]byte, len(car))
	for i := range car {
		noisyCar[i] = byte((17*int(car[i]) + 8*int(otherCar[i])) / 25)
	}
	if appearanceMatches(car, noisyCar, 7, 1, 34, 16, 14, 4, .90) {
		t.Fatal("the aligned crop must fall below the original body correlation threshold")
	}
	for _, tc := range []struct {
		name       string
		number     string
		appearance []byte
		localized  bool
		wantJoin   bool
	}{
		{"same four-digit vehicle", "1234", car, true, true},
		{"same three-digit vehicle", "123", car, true, true},
		{"same three-digit vehicle with aligned noisy images", "123", noisyCar, true, true},
		{"different vehicle with matching three digits", "123", otherCar, true, false},
		{"different vehicle with matching digits", "1234", otherCar, true, false},
		{"missing vehicle image", "1234", nil, true, false},
		{"no independent localized read", "1234", car, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Unix(100, 0)
			end := start.Add(125 * time.Millisecond)
			first, next := &plateTrack{first: start}, &plateTrack{first: end}
			older := verifiedPlateForRejoin(tc.number, [4]int{1450, 580, 50, 30})
			older.Reading.Confidence = .90
			older.Verifier.Confidence = .86
			older.Localized = &plate.Reading{Number: tc.number, Confidence: .93}
			older.appearance = car
			newer := verifiedPlateForRejoin(tc.number, [4]int{1555, 608, 55, 35})
			newer.Reading.Confidence = .83
			newer.Verifier.Confidence = .79
			if tc.localized {
				newer.Localized = older.Localized
			}
			newer.appearance = tc.appearance
			first.observe(start, start, older, "one", .60)
			next.observe(end, end, newer, "two", .60)
			tracks := rejoinTracks([]*plateTrack{next}, []*plateTrack{first})
			if got := tracks[0] == first; got != tc.wantJoin {
				t.Fatalf("rejoined = %v, want %v", got, tc.wantJoin)
			}
			if tc.wantJoin && (!first.first.Equal(start) || !first.last.Equal(end) || first.frames != 2) {
				t.Fatalf("rejoin did not preserve the observation interval and both frames")
			}
		})
	}
}

func TestFastOutwardRejoinRequiresOneConsistentEarlierView(t *testing.T) {
	vehicle, otherVehicle := makeAppearanceForTest(51), makeAppearanceForTest(52)
	for _, tc := range []struct {
		name         string
		earlierViews int
		latest       []byte
		wantJoin     bool
	}{
		{"unique earlier view", 1, vehicle, true},
		{"missing earlier view", 0, vehicle, false},
		{"two plausible earlier vehicles", 2, vehicle, false},
		{"different vehicle with matching digits", 1, otherVehicle, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Unix(100, 0)
			middleAt, lastAt := start.Add(375*time.Millisecond), start.Add(500*time.Millisecond)
			const number = "123"
			middle := &plateTrack{first: middleAt}
			last := &plateTrack{first: lastAt}
			p := verifiedPlateForRejoin(number, [4]int{1477, 570, 48, 31})
			p.appearance = vehicle
			middle.observe(middleAt, middleAt, p, "four", .60)
			middle.misses = 1
			p.Box, p.appearance = [4]int{1619, 609, 52, 38}, tc.latest
			p.Localized = &plate.Reading{Number: number, Confidence: .84}
			last.observe(lastAt, lastAt, p, "five", .60)
			tracks := []*plateTrack{middle, last}
			for range tc.earlierViews {
				earlier := &plateTrack{first: start}
				weak := verifiedPlateForRejoin(number, [4]int{1245, 509, 32, 19})
				weak.Reading.Number, weak.Reading.Confidence = "125", .67
				weak.Verifier.Confidence = .82
				weak.NumberScores[number] = .967
				weak.appearance = vehicle
				for i, box := range [][4]int{{1245, 509, 32, 19}, {1288, 522, 39, 24}, {1351, 539, 43, 26}} {
					at := start.Add(time.Duration(i) * 125 * time.Millisecond)
					weak.Box = box
					earlier.observe(at, at, weak, string(rune('1'+i)), .60)
				}
				earlier.misses = 2
				tracks = append(tracks, earlier)
			}
			tracks = rejoinTracks(tracks, nil)
			wantTracks := tc.earlierViews + 2
			if tc.wantJoin {
				wantTracks--
			}
			if len(tracks) != wantTracks || tc.wantJoin && (middle.frames != 2 || !middle.last.Equal(lastAt)) {
				t.Fatalf("found %d tracks and %d observations on the middle track, expected %d tracks", len(tracks), middle.frames, wantTracks)
			}
		})
	}
}

func TestRejoinRechecksEarlierFragmentAfterJoiningLaterReads(t *testing.T) {
	vehicle, otherVehicle := makeAppearanceForTest(41), makeAppearanceForTest(42)
	for _, tt := range []struct {
		name       string
		appearance []byte
		wantTracks int
	}{
		{"same vehicle", vehicle, 1},
		{"different vehicle with the same serial", otherVehicle, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Unix(100, 0)
			middle := start.Add(125 * time.Millisecond)
			end := middle.Add(500 * time.Millisecond)
			older, newer, latest := &plateTrack{first: start}, &plateTrack{first: middle}, &plateTrack{first: end}
			p := verifiedPlateForRejoin("1234", [4]int{295, 512, 49, 35})
			p.appearance = tt.appearance
			older.observe(start, start, p, "one", .60)
			p = verifiedPlateForRejoin("1234", [4]int{407, 500, 48, 28})
			p.appearance = vehicle
			newer.observe(middle, middle, p, "two", .60)
			p = verifiedPlateForRejoin("1234", [4]int{647, 475, 36, 17})
			p.Reading.Confidence = .80
			p.appearance = vehicle
			latest.observe(end, end, p, "three", .60)
			older.misses, newer.misses = 2, 1
			if older.canRejoin(newer) || older.canRejoin(latest) || !newer.canRejoin(latest) {
				t.Fatal("test setup must require the two later fragments to join first")
			}

			tracks := rejoinTracks([]*plateTrack{older, newer, latest}, nil)
			if len(tracks) != tt.wantTracks {
				t.Fatalf("found %d tracks, want %d", len(tracks), tt.wantTracks)
			}
			if tt.wantTracks == 1 && (tracks[0] != older || older.frames != 3 || !older.first.Equal(start) || !older.last.Equal(end)) {
				t.Fatal("joining the fragments lost an observation or the full detection interval")
			}
		})
	}
}
