package platesensor

import (
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

func TestUniquePassingPrimarySuppliesSecondObservation(t *testing.T) {
	start := time.Unix(100, 0)
	for _, tc := range []struct {
		name string
		edit func(*EnginePlate, *EnginePlate)
		want bool
	}{
		{"right outgoing", func(a, b *EnginePlate) {}, true},
		{"left outgoing", func(a, b *EnginePlate) {
			a.Box[0] = FrameWidth - a.Box[0] - a.Box[2]
			b.Box[0] = FrameWidth - b.Box[0] - b.Box[2]
		}, true},
		{"inward", func(a, b *EnginePlate) { b.Box[0] = a.Box[0] - 110 }, false},
		{"unrelated class", func(a, b *EnginePlate) { a.Reading.Class = "330"; b.Reading.Class = "501" }, false},
		{"number differs", func(a, b *EnginePlate) { b.Reading.Number = "1935" }, false},
		{"short number", func(a, b *EnginePlate) { a.Reading.Number = "123"; b.Reading.Number = "123" }, false},
		{"small plate", func(a, b *EnginePlate) { a.Box[2] = 32; b.Box[2] = 32 }, false},
		{"uncertain reread", func(a, b *EnginePlate) { b.Reading.Confidence = .35 }, false},
		{"farther than passage", func(a, b *EnginePlate) { b.Box[0] += 170 }, false},
		{"date overlay band", func(a, b *EnginePlate) { a.Box[1] = 930; b.Box[1] = 958 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := EnginePlate{Box: [4]int{1420, 560, 46, 29}, Score: .65, Reading: plate.Reading{Number: "1235", Confidence: .64}}
			b := EnginePlate{Box: [4]int{1530, 583, 46, 38}, Score: .59, Reading: plate.Reading{Number: "1235", Confidence: .53}}
			tc.edit(&a, &b)
			tr := &plateTrack{id: "pt-test"}
			tr.observe(start, start, a, "a", .60)
			next := start.Add(165 * time.Millisecond)
			det := []EnginePlate{b}
			assigned := assignTracks([]*plateTrack{tr}, next, det)
			serialSupport := assigned[0] == 0 && det[0].passingPrimaryFrame == "a"
			if serialSupport != tc.want {
				t.Fatalf("assignment %v, corroboration %v, want %v", assigned, serialSupport, tc.want)
			}
			if tc.want {
				tr.observe(next, next, det[0], "b", .60)
				d, ok := tr.detection(true)
				if !ok || d.Reading.Number != a.Reading.Number || d.Frame != "a" || d.Track.Support != 2 {
					t.Fatalf("verified passage = %+v, %v", d, ok)
				}
			}
		})
	}
}

func TestPassingPrimaryDeclinesMultipleVehicles(t *testing.T) {
	at := time.Unix(100, 0)
	later := at.Add(165 * time.Millisecond)
	a := EnginePlate{Box: [4]int{1420, 560, 46, 29}, Score: .65, Reading: plate.Reading{Number: "1235", Confidence: .64}}
	b := EnginePlate{Box: [4]int{1530, 583, 46, 38}, Score: .59, Reading: plate.Reading{Number: "1235", Confidence: .53}}
	for _, kind := range []string{"two previous vehicles", "two new detections", "other plate occupies original trajectory", "same frame used twice"} {
		t.Run(kind, func(t *testing.T) {
			tr := &plateTrack{}
			tr.observe(at, at, a, "a", .60)
			tracks := []*plateTrack{tr}
			det := []EnginePlate{b}
			switch kind {
			case "two previous vehicles":
				other := &plateTrack{}
				prior := a
				prior.Box[0] += 12
				other.observe(at, at, prior, "other", .60)
				tracks = append(tracks, other)
			case "two new detections":
				alt := b
				alt.Box[1] += 7
				det = append(det, alt)
			case "other plate occupies original trajectory":
				samePosition := a
				samePosition.Reading.Number = "9876"
				det = append(det, samePosition)
			}
			assignTracks(tracks, later, det)
			if kind == "same frame used twice" {
				tr.observe(later, later, det[0], "a", .60)
				if d, ok := tr.detection(true); ok && d.Track.Support >= 2 {
					t.Fatal("saved repeated frame as corroboration")
				}
				return
			}
			for _, d := range det {
				if d.passingPrimaryFrame != "" {
					t.Fatalf("ambiguous association endorsed %+v", d)
				}
			}
		})
	}
}
