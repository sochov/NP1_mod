package platesensor

import (
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

const openingHandoffGap = 1250 * time.Millisecond
const openingLookback = 20 * time.Second

type openingFragment struct {
	first, last time.Time
	box         [4]int
	view        []byte
}

func settledOpeningBox(box [4]int) bool {
	x, _ := center(box)
	return forwardPlate(box) && x >= .4*FrameWidth && x <= .6*FrameWidth &&
		box[2] >= 45 && box[2] <= 100 && 5*box[2] >= 7*box[3]
}

func (t *plateTrack) openingFragment() (openingFragment, bool) {
	if t.openingClaimed || t.sent != 0 || t.frames < 5 || t.last.Sub(t.first) < 750*time.Millisecond ||
		!settledOpeningBox(t.firstBox) || !settledOpeningBox(t.box) ||
		stationaryDistance(t.firstBox, t.box) > .32 || t.support > 0 && (!t.hasVerification || t.numberVerified()) {
		return openingFragment{}, false
	}
	first := t.first
	if !t.openingStart.IsZero() && t.last.Sub(t.openingStart) <= openingLookback {
		first = t.openingStart
	}
	return openingFragment{first, t.last, t.box, t.openingView}, true
}

func sameOpeningVehicle(a, b []byte) bool {
	if len(a) != appearanceWidth*appearanceHeight || len(b) != len(a) {
		return false
	}
	// Both regions must support the same alignment.
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			shifted := b[(3+dy)*appearanceWidth+6+dx:]
			if appearanceMatches(a, shifted, 6, 3, 36, 10, 0, 0, .80) &&
				appearanceMatches(a, shifted, 6, 3, 36, 22, 0, 0, .72) {
				return true
			}
		}
	}
	return false
}

func (s *Sensor) keepOpeningFragment(t *plateTrack, now time.Time) {
	if frag, ok := t.openingFragment(); ok && now.Sub(frag.last) <= openingHandoffGap {
		if len(s.openingHistory) == trackLimit {
			copy(s.openingHistory, s.openingHistory[1:])
			s.openingHistory = s.openingHistory[:trackLimit-1]
		}
		s.openingHistory = append(s.openingHistory, frag)
	}
}

func (s *Sensor) linkOpeningFragments(at time.Time, detections []EnginePlate) {
	for _, next := range s.tracks {
		if next.frames != 1 || !next.last.Equal(at) || !settledOpeningBox(next.box) {
			continue
		}
		// Even an unreadable second detection makes this handoff ambiguous.
		closeDetections := 0
		for _, found := range detections {
			if stationaryDistance(next.box, found.Box) <= .6 {
				closeDetections++
			}
		}
		if closeDetections != 1 {
			continue
		}
		var first time.Time
		choices := 0
		var view []byte
		var donor *plateTrack
		donorIndex := -1
		consider := func(frag openingFragment, active *plateTrack, index int) {
			gap := at.Sub(frag.last)
			if gap < trackMissWindow || gap > openingHandoffGap || at.Sub(frag.first) > openingLookback ||
				stationaryDistance(frag.box, next.box) > .22 {
				return
			}
			first = frag.first
			view = frag.view
			donor, donorIndex = active, index
			choices++
		}
		for _, prior := range s.tracks {
			if prior == next || !prior.missed(at) || at.Sub(prior.last) > openingHandoffGap {
				continue
			}
			if frag, ok := prior.openingFragment(); ok {
				consider(frag, prior, -1)
			}
		}
		for i, frag := range s.openingHistory {
			consider(frag, nil, i)
		}
		if choices == 1 && sameOpeningVehicle(view, next.openingView) {
			next.openingStart = first
			if donor != nil {
				donor.openingClaimed = true
			} else {
				last := len(s.openingHistory) - 1
				copy(s.openingHistory[donorIndex:], s.openingHistory[donorIndex+1:])
				s.openingHistory[last] = openingFragment{}
				s.openingHistory = s.openingHistory[:last]
			}
		}
	}
}

func (t *plateTrack) applyOpeningStart(d *plate.Detection) {
	// Pick the earlier time before the record is first published. Subsequent
	// snapshots use that time so persistence still finds the same day's row.
	if !d.Track.Update && !t.openingStart.IsZero() && d.At.After(t.openingStart) &&
		d.At.Sub(t.openingStart) <= openingLookback && len(d.Reading.Number) == 4 &&
		d.Track.Support >= 3 && !d.Track.Ambiguous && t.localized[d.Reading.Number] >= .90 {
		t.reportedOpening = t.openingStart
	}
	if !t.reportedOpening.IsZero() {
		d.At = t.reportedOpening
	}
}
