package platesensor

import (
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

func TestRecedingPlateExtendsVerifiedGeometryOnly(t *testing.T) {
	at := time.Unix(100, 0)
	interval := time.Second / 6
	sharp := 9.0
	verified := EnginePlate{
		Score: .65, Reading: plate.Reading{Number: "1234", Confidence: .9},
		Verifier:       &plate.Reading{Number: "1234", Confidence: .9},
		Localized:      &plate.Reading{Number: "1234", Confidence: .92},
		SequenceNumber: "1234", NumberScores: map[string]float64{"1234": .96}, DigitEdges: &sharp,
	}
	makeTrack := func() *plateTrack {
		track := &plateTrack{first: at}
		for i, box := range [][4]int{{415, 454, 54, 34}, {505, 456, 49, 30}, {590, 464, 43, 23}} {
			p := verified
			p.Box = box
			when := at.Add(time.Duration(i) * interval)
			track.observe(when, when, p, []string{"a", "b", "c"}[i], .60)
		}
		if !track.numberVerified() || track.support != 3 {
			t.Fatalf("fixture did not confirm the plate: %+v", track.best.reading)
		}
		return track
	}
	when := at.Add(3 * interval)
	last := EnginePlate{
		Box: [4]int{642, 467, 37, 18}, Score: .59,
		Reading: plate.Reading{Number: "1934", Class: "500", Confidence: .42},
	}
	track := makeTrack()
	if score := track.match(when, last); score != 0 {
		t.Fatalf("ordinary matching unexpectedly succeeded: %f", score)
	}
	input := []EnginePlate{last}
	assigned := []int{-1}
	continueRecedingTracks([]*plateTrack{track}, when, input, assigned)
	if assigned[0] != 0 || input[0].Reading.Readable() || input[0].Box != last.Box {
		t.Fatalf("expected only geometry to extend the confirmed track: %v %+v", assigned, input)
	}
	before := track.best
	track.observe(when, when, input[0], "d", .60)
	if !track.last.Equal(when) || track.frames != 4 || track.support != 3 || track.best.frame != before.frame ||
		track.best.reading != before.reading {
		t.Fatalf("geometry altered the established reading: %+v", track.best.reading)
	}
	for _, name := range []string{"competing crop", "competing track", "different reading", "long gap", "missed capture"} {
		t.Run(name, func(t *testing.T) {
			track := makeTrack()
			tracks := []*plateTrack{track}
			crops := []EnginePlate{last}
			now := when
			switch name {
			case "competing crop":
				other := last
				other.Box[0] += 6
				crops = append(crops, other)
			case "competing track":
				tracks = append(tracks, makeTrack())
			case "different reading":
				crops[0].Reading.Confidence = .91
			case "long gap":
				now = now.Add(100 * time.Millisecond)
			case "missed capture":
				track.misses = 1
			}
			assignment := make([]int, len(crops))
			for i := range assignment {
				assignment[i] = -1
			}
			continueRecedingTracks(tracks, now, crops, assignment)
			for _, id := range assignment {
				if id >= 0 {
					t.Fatalf("extended despite %s: %v", name, assignment)
				}
			}
		})
	}
}
