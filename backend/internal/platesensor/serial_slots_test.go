package platesensor

import (
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

func TestCorroboratedDigitsRequireEvidenceForEachPosition(t *testing.T) {
	const number = "1234"
	for _, tc := range []struct {
		name   string
		mutate func(*plateTrack)
		want   bool
	}{
		{"different strokes lost", func(*plateTrack) {}, true},
		{"stronger rival", func(tr *plateTrack) {
			p := &tr.recentFrames[1].plate
			p.Reading.Confidence = .90
			p.Verifier.Confidence = .90
		}, false},
		{"other plate in image", func(tr *plateTrack) { tr.recentFrames[1].plate.Reading.Number = "2434" }, false},
		{"conflicting digit crop", func(tr *plateTrack) {
			tr.recentFrames[1].plate.Localized = &plate.Reading{Number: "1274", Confidence: .9}
		}, false},
		{"repeated same stroke error", func(tr *plateTrack) {
			p := &tr.recentFrames[1].plate
			p.Reading.Number = "1274"
			p.Verifier.Number = "1274"
			p.SequenceNumber = "1274"
			p.NumberScores = map[string]float64{"1274": .95}
		}, false},
		{"single image of full number", func(tr *plateTrack) { tr.recentFrames[0].plate.Reading.Number = "1274" }, false},
		{"higher confidence configured", func(tr *plateTrack) { tr.recentFrames[3].minConfidence = .90 }, false},
		{"static text", func(tr *plateTrack) { tr.firstBox = tr.recentFrames[3].plate.Box }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Unix(100, 0)
			edges := .22
			tr := plateTrack{frames: 4, first: start, last: start.Add(375 * time.Millisecond), conflict: true}
			for i, read := range [][3]string{{number, "1274", "1834"}, {"1834", "1834", "1834"}, {"1274", "1274", "1274"}, {number, number, number}} {
				p := EnginePlate{Reading: plate.Reading{Number: read[0], Confidence: .59}, Verifier: &plate.Reading{Number: read[1], Confidence: .62}, SequenceNumber: read[2], NumberScores: map[string]float64{read[2]: .96}, Box: [4]int{1050 + 45*i, 450 + 5*i, 38 + 4*i, 25 + 2*i}, Score: .7, DigitEdges: &edges}
				tr.recentFrames = append(tr.recentFrames, trackFrame{id: string(rune('a' + i)), at: start.Add(time.Duration(i) * 125 * time.Millisecond), plate: p, minConfidence: .6})
			}
			tr.firstBox = tr.recentFrames[0].plate.Box
			end := tr.recentFrames[3]
			tr.votes = map[string]serialEvidence{number: {best: observation{frame: end.id, reading: *end.plate.Verifier}}}
			tc.mutate(&tr)
			obs, got := tr.recoverCorroboratedDigits()
			if got != tc.want || got && obs.reading.Number != number {
				t.Fatalf("corroborated digits = %q, %v, want acceptance %v", obs.reading.Number, got, tc.want)
			}
		})
	}
}
