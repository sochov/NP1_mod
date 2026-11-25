package platesensor

import (
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

func TestInwardDigitsRecoverAReadFromDistinctFrames(t *testing.T) {
	tests := []struct {
		name         string
		secondID     string
		otherLane    bool
		sequence     bool
		contradicts  bool
		wantRecovery bool
	}{
		{name: "distinct frames on one trajectory", secondID: "frame-2", sequence: true, wantRecovery: true},
		{name: "same frame", secondID: "frame-1", sequence: true},
		{name: "another lane", secondID: "frame-2", sequence: true, otherLane: true},
		{name: "no corroborating sequence", secondID: "frame-2"},
		{name: "credible competing reading", secondID: "frame-2", sequence: true, contradicts: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Unix(100, 0)
			first := EnginePlate{
				Box: [4]int{440, 489, 44, 27}, Score: .71,
				Reading:      plate.Reading{Number: "1238", Confidence: .47},
				Verifier:     &plate.Reading{Number: "1234", Confidence: .54},
				Localized:    &plate.Reading{Number: "1234", Confidence: .89},
				NumberScores: map[string]float64{"1234": .85, "1238": .05},
			}
			if tc.sequence {
				first.SequenceNumber = "1234"
			}
			second := first
			second.Box = [4]int{502, 484, 43, 25}
			second.SequenceNumber = ""
			second.NumberScores = map[string]float64{"1234": .56, "1238": .03}
			if tc.otherLane {
				second.Box[1] += 45
			}
			track := &plateTrack{id: "test-inward", first: at}
			track.observe(at, at, first, "frame-1", .60)
			next := at.Add(167 * time.Millisecond)
			if tc.wantRecovery && track.match(next, second) == 0 {
				t.Fatal("split the independently corroborated next observation")
			}
			track.observe(next, next, second, tc.secondID, .60)
			if tc.contradicts {
				other := second
				other.SequenceNumber = "1238"
				other.Verifier = &plate.Reading{Number: "1238", Confidence: .55}
				other.Localized = &plate.Reading{Number: "1238", Confidence: .89}
				other.NumberScores = map[string]float64{"1238": .94}
				later := next.Add(167 * time.Millisecond)
				track.observe(later, later, other, "frame-3", .60)
			}
			got, ok := track.detection(true)
			if ok != tc.wantRecovery || ok && (got.Reading.Number != "1234" || got.Track.Support != 2 ||
				got.Frame != "frame-1" || !got.At.Equal(at) || !got.Track.LastSeen.Equal(next)) {
				t.Fatalf("saved %+v (%t), want recovery: %t", got, ok, tc.wantRecovery)
			}
		})
	}
}
