package platesensor

import (
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

func TestOpeningRejectsAmbiguousHandoffs(t *testing.T) {
	start := time.Unix(1000, 0)
	box := [4]int{960, 500, 60, 32}
	view := make([]byte, appearanceWidth*appearanceHeight)
	otherView := make([]byte, len(view))
	for i := range view {
		view[i] = byte((i*i + i/11) % 251)
		otherView[i] = 255 - view[i]
	}
	for _, test := range []struct {
		name        string
		gap         time.Duration
		neighbor    bool
		secondDonor bool
		priorSaved  bool
		other       bool
		noView      bool
		want        bool
	}{
		{name: "single stationary handoff", gap: time.Second, want: true},
		{name: "nearby detection in the new frame", gap: time.Second, neighbor: true},
		{name: "two equally close earlier tracks", gap: time.Second, secondDonor: true},
		{name: "number already saved in the earlier track", gap: time.Second, priorSaved: true},
		{name: "different appearance in the same position", gap: time.Second, other: true},
		{name: "same position with missing appearance", gap: time.Second, noView: true},
		{name: "long absence", gap: 2 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			prior := &plateTrack{first: start, last: start.Add(time.Second),
				firstBox: box, box: box, frames: 6, misses: 6, openingView: view}
			if test.priorSaved {
				prior.sent = 6
			}
			at := prior.last.Add(test.gap)
			next := &plateTrack{first: at, last: at, firstBox: box, box: box, frames: 1, openingView: view}
			if test.other {
				next.openingView = otherView
			}
			if test.noView {
				next.openingView = nil
			}
			tracks := []*plateTrack{prior, next}
			if test.secondDonor {
				copy := *prior
				copy.box[0] += 8
				tracks = append(tracks, &copy)
			}
			detections := []EnginePlate{{Box: box}}
			if test.neighbor {
				near := box
				near[0] += 22
				detections = append(detections, EnginePlate{Box: near})
			}
			sensor := &Sensor{tracks: tracks}
			sensor.linkOpeningFragments(at, detections)
			if hasOpening := !next.openingStart.IsZero(); hasOpening != test.want {
				t.Fatalf("earlier observation adopted = %t, want %t", hasOpening, test.want)
			}
		})
	}
}

func TestOpeningOnlyExtendsTimestampOfConfirmedRead(t *testing.T) {
	first := time.Unix(1000, 0)
	at := first.Add(4 * time.Second)
	tr := &plateTrack{openingStart: first, localized: map[string]float64{"1234": .92}}
	d := plate.Detection{At: at, Reading: plate.Reading{Number: "1234"}, Frame: "representative",
		Track: &plate.TrackSpan{Support: 3, Frames: 13, LastSeen: at.Add(time.Second)}}
	tr.applyOpeningStart(&d)
	if !d.At.Equal(first) || d.Reading.Number != "1234" || d.Frame != "representative" || d.Track.Frames != 13 || d.Track.Support != 3 {
		t.Fatalf("changed more than the start time: %+v", d)
	}
	d.At, d.Track.Update = at, true
	tr.applyOpeningStart(&d)
	if !d.At.Equal(first) {
		t.Fatal("the next snapshot must still point to the published start")
	}
}

func TestOpeningKeepsPublishedStartWhenLaterFramesConfirmNumber(t *testing.T) {
	at := time.Unix(1000, 0)
	tr := &plateTrack{openingStart: at.Add(-4 * time.Second), localized: map[string]float64{"1234": .88}}
	d := plate.Detection{At: at, Reading: plate.Reading{Number: "1234"},
		Track: &plate.TrackSpan{Support: 3, Frames: 13, LastSeen: at.Add(time.Second)}}
	tr.applyOpeningStart(&d)
	if !d.At.Equal(at) {
		t.Fatal("the first snapshot cannot include a weakly confirmed opening")
	}
	tr.localized["1234"] = .96
	d.Track.Update = true
	tr.applyOpeningStart(&d)
	if !d.At.Equal(at) {
		t.Fatal("an update cannot move an already published record to an earlier date")
	}
}

func TestOpeningDoesNotReuseEarlierObservations(t *testing.T) {
	start := time.Unix(1000, 0)
	box := [4]int{960, 500, 60, 32}
	view := make([]byte, appearanceWidth*appearanceHeight)
	for i := range view {
		view[i] = byte((i*i + i/11) % 251)
	}
	for _, buffered := range []bool{false, true} {
		sensor := &Sensor{}
		prior := &plateTrack{first: start, last: start.Add(time.Second),
			firstBox: box, box: box, frames: 6, misses: 6, openingView: view}
		if buffered {
			sensor.keepOpeningFragment(prior, start.Add(1750*time.Millisecond))
		} else {
			sensor.tracks = append(sensor.tracks, prior)
		}
		for i := range 2 {
			at := start.Add(2*time.Second + time.Duration(i)*125*time.Millisecond)
			next := &plateTrack{first: at, last: at, firstBox: box, box: box, frames: 1, openingView: view}
			sensor.tracks = append(sensor.tracks, next)
			sensor.linkOpeningFragments(at, []EnginePlate{{Box: box}})
			if got := !next.openingStart.IsZero(); got != (i == 0) {
				t.Fatalf("buffered=%v successor=%d: previous observations reused=%v", buffered, i, got)
			}
		}
	}
}

func TestOpeningNeedsOneAlignment(t *testing.T) {
	a := make([]byte, appearanceWidth*appearanceHeight)
	b := make([]byte, len(a))
	for i := range a {
		noise := (i*i + i/11) % 251
		if i/appearanceWidth < 13 {
			a[i] = byte(106 + noise%44)
		} else {
			a[i] = byte(noise)
		}
		b[i] = 128
	}
	for y := range appearanceHeight {
		shift := 2
		if y < 13 {
			shift = 0
		}
		for x := 2; x < appearanceWidth-2; x++ {
			b[y*appearanceWidth+x+shift] = a[y*appearanceWidth+x]
		}
	}
	window := b[appearanceWidth+4:]
	if !appearanceMatches(a, window, 6, 3, 36, 10, 4, 4, .80) ||
		!appearanceMatches(a, window, 6, 3, 36, 22, 4, 4, .72) {
		t.Fatal("fixture must match both regions when each is shifted independently")
	}
	if sameOpeningVehicle(a, b) {
		t.Fatal("accepted one vehicle alignment from mutually different shifts")
	}
}

func BenchmarkOpeningViewComparison(b *testing.B) {
	view := make([]byte, appearanceWidth*appearanceHeight)
	other := make([]byte, len(view))
	for i := range view {
		view[i] = byte((i*i + i/11) % 251)
		other[i] = 255 - view[i]
	}
	for _, sample := range []struct {
		name string
		view []byte
	}{
		{"match", view},
		{"reject", other},
	} {
		b.Run(sample.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				sameOpeningVehicle(view, sample.view)
			}
		})
	}
}

func BenchmarkOpeningViewSample(b *testing.B) {
	pixels := make([]byte, FrameWidth*FrameHeight)
	box := [4]int{968, 490, 60, 32}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		sampleVehicleView(pixels, box)
	}
}
