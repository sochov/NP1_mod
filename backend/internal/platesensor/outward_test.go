package platesensor

import (
	"slices"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

func TestWideOutwardLinkRequiresAUniqueCorroboratedPair(t *testing.T) {
	at := time.Unix(100, 0)
	first := EnginePlate{
		Box: [4]int{1500, 550, 43, 29}, Score: .67,
		Reading:        plate.Reading{Number: "1238", Confidence: .62},
		Verifier:       &plate.Reading{Number: "1234", Confidence: .76},
		SequenceNumber: "1234", NumberScores: map[string]float64{"1234": .96},
	}
	second := EnginePlate{
		Box: [4]int{1583, 572, 47, 34}, Score: .65,
		Reading:        plate.Reading{Number: "1234", Confidence: .62},
		Verifier:       &plate.Reading{Number: "1234", Confidence: .85},
		Localized:      &plate.Reading{Number: "1234", Confidence: .94},
		SequenceNumber: "1234", NumberScores: map[string]float64{"1234": .98},
	}
	track := &plateTrack{id: "earlier", first: at}
	track.observe(at, at, first, "earlier-frame", .60)
	next := at.Add(125 * time.Millisecond)
	if got := assignTracks([]*plateTrack{track}, next, []EnginePlate{second}); !slices.Equal(got, []int{0}) {
		t.Fatalf("unique corroborated pair split: %v", got)
	}
	otherTrack := &plateTrack{id: "other", first: at}
	otherTrack.observe(at, at, first, "other-frame", .60)
	if got := assignTracks([]*plateTrack{track, otherTrack}, next, []EnginePlate{second}); !slices.Equal(got, []int{-1}) {
		t.Fatalf("matched an ambiguous prior track: %v", got)
	}
	otherDetection := second
	otherDetection.Box[0] += 5
	if got := assignTracks([]*plateTrack{track}, next, []EnginePlate{second, otherDetection}); !slices.Equal(got, []int{-1, -1}) {
		t.Fatalf("matched an ambiguous new detection: %v", got)
	}
	weak := second
	weak.Localized = nil
	if got := assignTracks([]*plateTrack{track}, next, []EnginePlate{weak}); !slices.Equal(got, []int{-1}) {
		t.Fatalf("matched without digit reader corroboration: %v", got)
	}
	track.observe(next, next, second, "later-frame", .60)
	if got, ok := track.detection(true); !ok || got.Reading.Number != "1234" || !got.At.Equal(at) || !got.Track.LastSeen.Equal(next) {
		t.Fatalf("could not save the confirmed pair: %+v (%t)", got, ok)
	}
}
