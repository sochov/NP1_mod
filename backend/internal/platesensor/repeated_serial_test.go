package platesensor

import (
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

func TestRepeatedSerialRequiresDistinctConsistentViews(t *testing.T) {
	const number = "1234"
	for _, tc := range []struct {
		name   string
		mutate func([]trackFrame)
		want   bool
	}{
		{"complementary evidence", func([]trackFrame) {}, true},
		{"same crop", func(v []trackFrame) {
			for i := 1; i < len(v); i++ {
				v[i].plate.Box = v[0].plate.Box
			}
		}, false},
		{"same image twice", func(v []trackFrame) { v[1].id = v[0].id }, false},
		{"another confident digit crop", func(v []trackFrame) { v[1].plate.Localized = &plate.Reading{Number: "1254", Confidence: .95} }, false},
		{"no second primary read", func(v []trackFrame) { v[0].plate.Reading.Number = "1274" }, false},
		{"corroboration arrives too late", func(v []trackFrame) { v[2].at = v[2].at.Add(time.Second) }, false},
		{"anchor below configured minimum", func(v []trackFrame) { v[2].minConfidence = .9 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Unix(100, 0)
			edges := .35
			var frames []trackFrame
			for i, id := range []string{"a", "b", "c"} {
				r := plate.Reading{Number: number, Confidence: .52}
				p := EnginePlate{Box: [4]int{400 - 35*i, 500, 56, 31}, Score: .65, Reading: r, Verifier: &plate.Reading{Number: number, Confidence: .47}}
				if i > 0 {
					p.SequenceNumber = number
					p.NumberScores = map[string]float64{number: .83}
				}
				if i == 1 {
					p.Reading.Number = "1254"
				}
				if i == 2 {
					p.Reading.Confidence = .80
					p.Verifier.Confidence = .63
					p.NumberScores[number] = .96
					p.DigitEdges = &edges
				}
				frames = append(frames, trackFrame{id: id, at: at.Add(time.Duration(i) * 140 * time.Millisecond), plate: p, minConfidence: .6})
			}
			tc.mutate(frames)
			tr := plateTrack{reads: 1, recentFrames: frames}
			best := observation{frame: "c", reading: plate.Reading{Number: number, Confidence: .80}, box: frames[2].plate.Box}
			if got := tr.confirmsRepeatedSerial(best); got != tc.want {
				t.Errorf("repeated serial confirmation = %v, want %v", got, tc.want)
			}
		})
	}
}
