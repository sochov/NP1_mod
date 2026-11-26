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

func TestAcceleratingOutwardReadUsesAnEarlierIndependentView(t *testing.T) {
	const number = "1234"
	start := time.Unix(100, 0)
	interval := 166 * time.Millisecond
	edges := .22
	prior := EnginePlate{Box: [4]int{1338, 538, 43, 25}, Score: .72,
		Verifier: &plate.Reading{Number: number, Confidence: .40}, SequenceNumber: number,
		NumberScores: map[string]float64{number: .65}}
	first := EnginePlate{Box: [4]int{1434, 561, 46, 31}, Score: .72,
		Reading:    plate.Reading{Number: number, Confidence: .49},
		Verifier:   &plate.Reading{Number: number, Confidence: .75},
		Localized:  &plate.Reading{Number: number, Confidence: .75},
		DigitEdges: &edges, SequenceNumber: number, NumberScores: map[string]float64{number: .98}}
	last := EnginePlate{Box: [4]int{1543, 587, 48, 40}, Score: .70,
		Reading:    plate.Reading{Number: number, Confidence: .73},
		Verifier:   &plate.Reading{Number: number, Confidence: .74},
		Localized:  &plate.Reading{Number: number, Confidence: .92},
		DigitEdges: &edges, SequenceNumber: number, NumberScores: map[string]float64{number: .99}}
	for _, tc := range []struct {
		name    string
		alter   func(*EnginePlate)
		priorID string
		extra   bool
		want    []int
	}{
		{name: "consistent acceleration", priorID: "prior", want: []int{0}},
		{name: "different digits in the earlier view", priorID: "prior", want: []int{-1},
			alter: func(p *EnginePlate) { p.SequenceNumber = "1238" }},
		{name: "earlier box was moving toward the center", priorID: "prior", want: []int{-1},
			alter: func(p *EnginePlate) { p.Box[0] = 1500 }},
		{name: "same image used for two boxes", priorID: "first", want: []int{-1}},
		{name: "other detection can use the same trajectory", priorID: "prior", extra: true, want: []int{-1, -1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := prior
			if tc.alter != nil {
				tc.alter(&p)
			}
			track := &plateTrack{id: "passing", first: start}
			track.observe(start, start, p, tc.priorID, .60)
			track.observe(start.Add(interval), start.Add(interval), first, "first", .60)
			track.velocity = [2]float64{250, 40}
			next := start.Add(2 * interval)
			if track.match(next, last) != 0 {
				t.Fatal("the ordinary motion match masks the outward fallback")
			}
			detections := []EnginePlate{last}
			if tc.extra {
				other := last
				other.Box[0] += 3
				detections = append(detections, other)
			}
			if assigned := assignTracks([]*plateTrack{track}, next, detections); !slices.Equal(assigned, tc.want) {
				t.Fatalf("assignments = %v, want %v", assigned, tc.want)
			}
			if tc.want[0] != 0 {
				return
			}
			track.observe(next, next, last, "last", .60)
			out, ok := track.detection(true)
			if !ok || out.Reading.Number != number || !out.At.Equal(start) || !out.Track.LastSeen.Equal(next) || out.Track.Frames != 3 {
				t.Fatalf("failed to save the supported interval: %+v (%t)", out, ok)
			}
		})
	}
}

func TestAcceleratingOutwardReadCombinesComplementaryReaders(t *testing.T) {
	const number = "1234"
	edges := .30
	start := time.Unix(120, 0)
	interval := 125 * time.Millisecond
	earliest := EnginePlate{Box: [4]int{1345, 545, 43, 25}, Score: .73, SequenceNumber: number,
		Verifier: &plate.Reading{Number: number, Confidence: .39}, NumberScores: map[string]float64{number: .72}}
	intermediate := EnginePlate{Box: [4]int{1397, 555, 44, 28}, Score: .72,
		Reading: plate.Reading{Number: "1839", Confidence: .40}}
	first := EnginePlate{Box: [4]int{1462, 570, 47, 31}, Score: .74,
		Reading:    plate.Reading{Number: "1239", Confidence: .75},
		Verifier:   &plate.Reading{Number: number, Confidence: .50},
		Localized:  &plate.Reading{Number: number, Confidence: .86},
		DigitEdges: &edges, SequenceNumber: number, NumberScores: map[string]float64{number: .91}}
	last := EnginePlate{Box: [4]int{1582, 605, 47, 36}, Score: .69,
		Reading:    plate.Reading{Number: number, Confidence: .81},
		Verifier:   &plate.Reading{Number: number, Confidence: .79},
		DigitEdges: &edges, SequenceNumber: number, NumberScores: map[string]float64{number: .99}}
	for _, tc := range []struct {
		name   string
		edits  func(*EnginePlate, *EnginePlate, *EnginePlate)
		linked bool
	}{
		{name: "independent confirmation on successive images", linked: true},
		{name: "no corroboration from the earlier approach", edits: func(a, _, _ *EnginePlate) { a.SequenceNumber = "1238" }},
		{name: "confident contrary primary read", edits: func(_, b, _ *EnginePlate) { b.Reading.Confidence = .86 }},
		{name: "independent digit detail unavailable", edits: func(_, b, _ *EnginePlate) { b.Localized = nil }},
		{name: "weak final primary read", edits: func(_, _, c *EnginePlate) { c.Reading.Confidence = .74 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b, c := earliest, first, last
			if tc.edits != nil {
				tc.edits(&a, &b, &c)
			}
			track := &plateTrack{id: "outgoing", first: start}
			track.observe(start, start, a, "a", .60)
			track.observe(start.Add(interval), start.Add(interval), intermediate, "b", .60)
			track.observe(start.Add(2*interval), start.Add(2*interval), b, "c", .60)
			end := start.Add(3 * interval)
			if linked := track.canMatchOutwardDigits(end, c); linked != tc.linked {
				t.Fatalf("outward match = %t, want %t", linked, tc.linked)
			}
			if !tc.linked {
				return
			}
			track.observe(end, end, c, "d", .60)
			d, ok := track.detection(true)
			if !ok || d.Reading.Number != number || d.Track.Frames != 4 || d.Frame != "d" {
				t.Fatalf("failed to save the jointly supported reading: %+v (%t)", d, ok)
			}
		})
	}
}
