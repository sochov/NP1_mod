package platesensor

import (
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

func TestGlareRegionRegistration(t *testing.T) {
	start := time.Unix(10, 0)
	for _, tc := range []struct {
		name            string
		scores          []float64
		contrastingRows int
		step            time.Duration
		repeatFrame     bool
		conflict        int
		want            string
	}{
		{"spaced consistent reads", []float64{.79, .51, .48}, 2, time.Second, false, -1, "横浜"},
		{"all white", []float64{.90, .90, .90}, 0, time.Second, false, -1, ""},
		{"one dark edge", []float64{.90, .90, .90}, 1, time.Second, false, -1, ""},
		{"one exposure repeated", []float64{.90, .90, .90}, 2, time.Second, true, -1, ""},
		{"too soon", []float64{.90, .90, .90}, 2, 100 * time.Millisecond, false, -1, ""},
		{"two exposures", []float64{.90, .90}, 2, time.Second, false, -1, ""},
		{"weak field scores", []float64{.74, .74, .74}, 2, time.Second, false, -1, ""},
		{"no independent field score", []float64{math.NaN(), .90, .90}, 2, time.Second, false, -1, ""},
		{"first weak contradictory read", []float64{.20, .90, .90, .90}, 2, time.Second, false, 0, ""},
		{"conflicting third field", []float64{.90, .90, .90, .90}, 2, time.Second, false, 2, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			track := plateTrack{first: start}
			view := make([]byte, appearanceWidth*appearanceHeight)
			for i := range view {
				view[i] = 245
			}
			for i := 0; i < tc.contrastingRows; i++ {
				view[(15+i)*appearanceWidth+20] = 190
			}
			if fieldVisible(view, 0) {
				t.Fatal("fixture must trigger the ordinary glare filter")
			}
			for i, score := range tc.scores {
				at := start.Add(time.Duration(i) * tc.step)
				frame := strconv.Itoa(i)
				if tc.repeatFrame {
					frame = "same"
				}
				region := "横浜"
				if i == tc.conflict {
					region = "多摩"
				}
				result := plate.Reading{Number: "1234", Region: region, Confidence: .9}
				input := EnginePlate{Box: [4]int{700, 450, 80, 36}, Score: .50, Reading: result, FieldScores: &[3]float64{score, 0, 0}, appearance: view}
				track.observe(at, at, input, frame, .6)
			}
			detection, ok := track.detection(true)
			if !ok || detection.Reading.Region != tc.want {
				t.Fatalf("registered=%t region=%q want=%q", ok, detection.Reading.Region, tc.want)
			}
		})
	}
}

func TestGlareRegionFragments(t *testing.T) {
	start := time.Unix(10, 0)
	left := glareRegionEvidence{region: "横浜", first: start, last: start.Add(time.Second), count: 2, peak: .8}
	right := glareRegionEvidence{region: "横浜", first: start.Add(2 * time.Second), last: start.Add(2 * time.Second), count: 1, peak: .5}
	shared := left
	shared.merge(left)
	if shared.consensus(nil) != "" {
		t.Fatal("overlapping fragments double-counted")
	}
	left.merge(right)
	if left.consensus(nil) != "横浜" || left.consensus(map[string]int{"多摩": 1}) != "" {
		t.Fatal("inconsistent fragment or regular-width evidence")
	}
}
