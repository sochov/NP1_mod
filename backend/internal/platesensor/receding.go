package platesensor

import (
	"math"
	"slices"
	"time"
)

// A receding plate can cross more than its width between frames even after the
// camera projection starts to slow. Extend a recently verified observation
// through that motion when the new crop is too small to read reliably.
func (t *plateTrack) canContinueReceding(at time.Time, p EnginePlate) bool {
	dt := at.Sub(t.last)
	number := t.best.reading.Number
	if dt < 80*time.Millisecond || dt > 200*time.Millisecond || t.misses != 0 || t.support < 3 ||
		len(number) != 4 || !t.hasVerification || t.ambiguous || t.conflict || t.votes[number].leadingDigitConflict ||
		p.Score < .42 || p.Box[2] < 24 || p.Box[2] > 44 || p.Box[2] >= t.box[2] || p.Box[3] <= 0 {
		return false
	}
	if p.LocalizedLeadConflict != "" || p.Reading.Number != "" && p.Reading.Number != number && p.Reading.Confidence >= .60 ||
		p.Verifier != nil && p.Verifier.Number != number && p.Verifier.Confidence >= .60 ||
		p.Localized != nil && p.Localized.Number != number && p.Localized.Confidence >= .75 ||
		p.SequenceNumber != "" && p.SequenceNumber != number && p.NumberScores[p.SequenceNumber] >= .72 {
		return false
	}
	x, y := center(t.box)
	nx, ny := center(p.Box)
	dx, dy := nx-x, ny-y
	side := x - FrameWidth/2
	w, h := float64(t.box[2]), float64(t.box[3])
	nw, nh := float64(p.Box[2]), float64(p.Box[3])
	ex, ey := t.velocity[0]*dt.Seconds(), t.velocity[1]*dt.Seconds()
	if math.Abs(side) < .06*FrameWidth || side*dx >= 0 || side*(nx-FrameWidth/2) <= 0 ||
		math.Abs(t.velocity[0]) < 135 || nw < .70*w || nh < .63*h || nh > 1.12*h ||
		math.Abs(dx) > 1.7*w || math.Abs(dy) > .32*max(h, nh) ||
		ex*dx <= 0 || dx/ex < .55 || dx/ex > 1.08 || math.Abs(dx-ex) > .88*w || math.Abs(dy-ey) > .40*max(h, nh) {
		return false
	}
	for i := len(t.recentFrames) - 1; i >= 0; i-- {
		frame := t.recentFrames[i]
		if at.Sub(frame.at) > 900*time.Millisecond {
			break
		}
		q := frame.plate
		if q.Reading.Number == number && q.Reading.Confidence >= .75 && q.Verifier != nil &&
			q.Verifier.Number == number && q.Verifier.Confidence >= .74 &&
			q.SequenceNumber == number && q.NumberScores[number] >= .92 {
			return t.numberVerified()
		}
	}
	return false
}

// An unmatched crop may extend a confirmed track only when there is one
// plausible choice on each side. The crop contributes visibility and geometry.
func continueRecedingTracks(tracks []*plateTrack, at time.Time, detections []EnginePlate, assigned []int) {
	var confirmed [trackLimit]int
	geometryOnly := confirmed[:0]
	for i, p := range detections {
		if assigned[i] >= 0 || p.Score < .42 {
			continue
		}
		candidate := -1
		for j, track := range tracks {
			if slices.Contains(assigned, j) || !track.canContinueReceding(at, p) {
				continue
			}
			if candidate >= 0 {
				candidate = -1
				break
			}
			candidate = j
		}
		if candidate < 0 {
			continue
		}
		for j, alternative := range tracks {
			if j != candidate && alternative.match(at, p) > 0 {
				candidate = -1
				break
			}
		}
		if candidate < 0 {
			continue
		}
		track := tracks[candidate]
		for j, alternative := range detections {
			if j != i && (stationaryDistance(p.Box, alternative.Box) <= .8 ||
				track.match(at, alternative) > 0 || track.canContinueReceding(at, alternative)) {
				candidate = -1
				break
			}
		}
		if candidate >= 0 {
			assigned[i] = candidate
			geometryOnly = append(geometryOnly, i)
		}
	}
	for _, i := range geometryOnly {
		p := detections[i]
		detections[i] = EnginePlate{Box: p.Box, Score: p.Score}
	}
}
