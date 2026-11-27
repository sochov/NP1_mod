package platesensor

import (
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

func TestSettledSerialNeedsSeparateReadersAndExposures(t *testing.T) {
	const number = "1212"
	for _, c := range []struct {
		name   string
		mutate func([]trackFrame)
		want   bool
	}{
		{"same serial on distinct views", func([]trackFrame) {}, true},
		{"repeat of a single view", func(v []trackFrame) {
			for i := 1; i < len(v); i++ {
				v[i].plate.appearance = v[0].plate.appearance
			}
		}, false},
		{"same frame ID under a new timestamp", func(v []trackFrame) { v[3].id = v[0].id }, false},
		{"only primary reads", func(v []trackFrame) {
			for i := range v {
				v[i].plate.Verifier = nil
			}
		}, false},
		{"no exact localized digits", func(v []trackFrame) { v[3].plate.Localized = nil }, false},
		{"similar repeated alternative", func(v []trackFrame) { v[1].plate.Localized = &plate.Reading{Number: "1717", Confidence: .91} }, false},
		{"reliable disagreeing primary", func(v []trackFrame) { v[0].plate.Reading = plate.Reading{Number: "1717", Confidence: .80} }, false},
		{"disagreeing sequence and numeric verifier", func(v []trackFrame) {
			p := &v[1].plate
			p.SequenceNumber = "1217"
			p.NumberScores["1217"] = .99
			p.Verifier = &plate.Reading{Number: "1217", Confidence: .81}
		}, false},
		{"similar plate in the adjacent lane", func(v []trackFrame) { v[1].plate.Box[0] += 80 }, false},
		{"too short to corroborate", func(v []trackFrame) {
			for i := range v {
				v[i].at = v[0].at.Add(time.Duration(i) * 100 * time.Millisecond)
			}
		}, false},
		{"configured minimum", func(v []trackFrame) {
			for i := range v {
				v[i].minConfidence = .90
			}
		}, false},
		{"no second primary", func(v []trackFrame) { v[0].plate.Reading.Number = "7212" }, false},
		{"weak second verifier", func(v []trackFrame) { v[2].plate.Verifier.Confidence = .45 }, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			at := time.Unix(100, 0)
			edges := .20
			frames := make([]trackFrame, 4)
			for i := range frames {
				q := EnginePlate{Box: [4]int{1050 + i*2, 555, 45, 26}, Score: .68, Reading: plate.Reading{Number: "7212", Confidence: .48}, Verifier: &plate.Reading{Number: number, Confidence: .48}, SequenceNumber: number, NumberScores: map[string]float64{number: .82}, appearance: make([]byte, appearanceWidth*appearanceHeight)}
				q.appearance[0] = byte(i + 1)
				if i == 0 {
					q.Reading = plate.Reading{Number: number, Confidence: .42}
				}
				if i >= 2 {
					q.Verifier.Confidence = .78
					q.NumberScores[number] = .96
				}
				if i == 3 {
					q.Reading = plate.Reading{Number: number, Confidence: .55}
					q.Localized = &plate.Reading{Number: number, Confidence: .91}
					q.DigitEdges = &edges
				}
				frames[i] = trackFrame{id: string(rune('a' + i)), at: at.Add(time.Duration(i) * 400 * time.Millisecond), plate: q, minConfidence: .60}
			}
			best := observation{reading: plate.Reading{Number: number, Confidence: .78}, box: frames[3].plate.Box, at: frames[3].at}
			tr := plateTrack{hasVerification: true, recentFrames: frames}
			c.mutate(frames)
			if got := tr.confirmsSettledSerial(best); got != c.want {
				t.Fatalf("settled serial confirmation=%v want %v", got, c.want)
			}
		})
	}
}
