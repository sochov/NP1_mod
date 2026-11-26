package platesensor

import (
	"strconv"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

func TestCompletePassingTailsRequiresUniqueConfirmedPaths(t *testing.T) {
	at := time.Unix(400, 0)
	const interval = 125 * time.Millisecond
	build := func(number string, mirrored, withLocalized bool) []*plateTrack {
		var tracks []*plateTrack
		for i, box := range [][4]int{{1529, 486, 49, 40}, {1655, 505, 51, 49}, {1825, 524, 38, 65}} {
			if mirrored {
				box[0] = FrameWidth - box[0] - box[2]
			}
			when := at.Add(time.Duration(i) * interval)
			p := EnginePlate{Box: box, Score: .62,
				Reading:        plate.Reading{Number: number, Confidence: [...]float64{.83, .47, .86}[i]},
				Verifier:       &plate.Reading{Number: number, Confidence: .92},
				SequenceNumber: number, NumberScores: map[string]float64{number: .99}}
			if i == 1 && withLocalized {
				p.Localized = &plate.Reading{Number: number, Confidence: .75}
			}
			track := &plateTrack{first: when}
			track.observe(when, when, p, strconv.Itoa(i), .60)
			tracks = append(tracks, track)
		}
		return tracks
	}
	last := at.Add(2 * interval)
	tracks := append(build("123", false, true), build("456", true, true)...)
	resolved := completePassingTails(tracks, last)
	if len(resolved) != 2 {
		t.Fatalf("simultaneous passes produced %d tracks, want 2", len(resolved))
	}
	for i, tail := range resolved {
		if tail.best.reading.Number != [...]string{"123", "456"}[i] || tail.frames != 3 || tail.support != 1 ||
			!tail.first.Equal(at) || !tail.last.Equal(last) || tail.best.frame != "0" {
			t.Fatalf("continuation altered the confirmed read: %+v", tail)
		}
	}
	weak := build("123", false, false)
	if len(completePassingTails(weak, last)) != 3 {
		t.Fatal("joined without separate digit confirmation")
	}
	end := build("123", false, true)
	lastReading := &end[2].recentFrames[0].plate
	lastReading.Verifier.Confidence = .89
	if len(completePassingTails(end, last)) != 3 {
		t.Fatal("joined a weaker last frame without independent confirmation")
	}
	lastReading.Localized = &plate.Reading{Number: "123", Confidence: .75}
	if len(completePassingTails(end, last)) != 1 {
		t.Fatal("separate digits did not corroborate the slightly weaker verifier")
	}
	conflicting := append(build("123", false, true), build("123", true, true)...)
	if len(completePassingTails(conflicting, last)) != 6 {
		t.Fatal("joined despite a simultaneous matching number")
	}
}
