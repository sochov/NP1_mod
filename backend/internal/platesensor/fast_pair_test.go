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
