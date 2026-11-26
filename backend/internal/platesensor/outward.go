package platesensor

import (
	"math"
	"slices"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

func outwardDigitPair(first, last EnginePlate, dt time.Duration) string {
	return readOutwardPair(first, last, dt, false)
}

func readOutwardPair(first, last EnginePlate, dt time.Duration, fast bool) string {
	number := first.SequenceNumber
	if len(number) != 4 || number != last.SequenceNumber || dt <= 0 || dt > 175*time.Millisecond ||
		first.NumberScores[number] < .94 || last.NumberScores[number] < .94 {
		return ""
	}
	if first.Verifier == nil || last.Verifier == nil || first.Verifier.Number != number || last.Verifier.Number != number ||
		max(first.Verifier.Confidence, last.Verifier.Confidence) < .70 {
		return ""
	}
	localized := func(p EnginePlate) bool {
		return p.Localized != nil && p.Localized.Number == number && p.Localized.Confidence >= .80
	}
	primary := func(p EnginePlate) bool {
		return p.Reading.Number == number && p.Reading.Confidence >= .60
	}
	if (!localized(first) && !localized(last)) || (!primary(first) && !primary(last)) {
		return ""
	}
	x, y := center(first.Box)
	nx, ny := center(last.Box)
	dx, dy := nx-x, ny-y
	w, h := float64(first.Box[2]), float64(first.Box[3])
	nw, nh := float64(last.Box[2]), float64(last.Box[3])
	side := x - FrameWidth/2
	maxShift, maxDrop := 2.2, 1.0
	if fast {
		if first.Reading.Number != number || last.Reading.Number != number || first.Reading.Confidence < .40 ||
			last.Reading.Confidence < .70 || min(first.Score, last.Score) < .62 ||
			min(first.NumberScores[number], last.NumberScores[number]) < .97 ||
			min(first.Verifier.Confidence, last.Verifier.Confidence) < .65 {
			return ""
		}
		maxShift, maxDrop = 2.65, 1.12
	}
	if math.Abs(side) < .20*FrameWidth || side*dx <= 0 || first.Score < .40 || last.Score < .40 ||
		w < 35 || h < 18 || nw > 64 || nw < .95*w || nw > 1.3*w || nh < h || nh > 1.35*h ||
		math.Abs(dx) < 1.3*w || math.Abs(dx) > maxShift*max(w, nw) || dy < .65*max(h, nh) || dy > maxDrop*max(h, nh) {
		return ""
	}
	return number
}

// An earlier box with the same sequence and an outbound trajectory can
// corroborate a sharp increase in displacement over the next two frames.
func acceleratingOutward(first, middle trackFrame, last EnginePlate, at time.Time) bool {
	previousGap, gap := middle.at.Sub(first.at), at.Sub(middle.at)
	if first.id == "" || middle.id == "" || first.id == middle.id ||
		previousGap < 100*time.Millisecond || previousGap > 175*time.Millisecond ||
		gap < 100*time.Millisecond || gap > 175*time.Millisecond || min(first.plate.Score, middle.plate.Score, last.Score) < .60 {
		return false
	}
	ax, ay := center(first.plate.Box)
	bx, by := center(middle.plate.Box)
	cx, cy := center(last.Box)
	rate := float64(gap) / float64(previousGap)
	firstDX, firstDY := (bx-ax)*rate, (by-ay)*rate
	dx, dy := cx-bx, cy-by
	return math.Abs(bx-FrameWidth/2) >= .20*FrameWidth && (bx-FrameWidth/2)*firstDX > 0 &&
		math.Abs(firstDX) >= float64(middle.plate.Box[2]) && firstDY >= 4 &&
		dx/firstDX >= .95 && dx/firstDX <= 2.05 && dy/firstDY >= .75 && dy/firstDY <= 2.65
}

func weakOutwardSequence(p EnginePlate, number string) bool {
	return p.SequenceNumber == number && p.NumberScores[number] >= .62 && p.Verifier != nil && p.Verifier.Number == number
}

func (t *plateTrack) canMatchOutwardDigits(at time.Time, next EnginePlate) bool {
	if t.sent != 0 || len(t.recentFrames) == 0 || t.missed(at) || !at.Before(t.finished.Add(trackGap)) {
		return false
	}
	previous := t.recentFrames[len(t.recentFrames)-1]
	if !previous.at.Equal(t.last) {
		return false
	}
	gap := at.Sub(previous.at)
	if outwardDigitPair(previous.plate, next, gap) != "" {
		return true
	}
	if len(t.recentFrames) < 2 {
		return false
	}
	prior := t.recentFrames[len(t.recentFrames)-2]
	number := next.SequenceNumber
	if !acceleratingOutward(prior, previous, next, at) {
		return false
	}
	if weakOutwardSequence(prior.plate, number) && readOutwardPair(previous.plate, next, gap, true) != "" {
		return true
	}
	// Keep a weak start attached to the first supported reading. This edge
	// contributes geometry only until another frame corroborates the serial.
	return t.support == 0 && t.frames >= 4 && weakOutwardSequence(previous.plate, number) &&
		next.Reading.Number == number && next.Reading.Confidence >= .42 && next.Verifier != nil &&
		next.Verifier.Number == number && next.Verifier.Confidence >= .65 && next.NumberScores[number] >= .97 &&
		next.Localized != nil && next.Localized.Number == number && next.Localized.Confidence >= .74 &&
		next.Box[2] >= 45 && next.Box[2] <= 64 && next.Box[2] >= previous.plate.Box[2] && 4*next.Box[2] <= 5*previous.plate.Box[2] &&
		next.Box[3] >= previous.plate.Box[3] && 4*next.Box[3] <= 6*previous.plate.Box[3]
}

// The wide displacement is eligible only when both the previous track and
// the new detection have one compatible candidate and no ordinary match.
func allowUniqueOutwardMatches(weights [][]float64, tracks []*plateTrack, at time.Time, detections []EnginePlate) {
	for i, detection := range detections {
		number := detection.SequenceNumber
		if len(number) != 4 || detection.NumberScores[number] < .94 ||
			slices.ContainsFunc(weights[i], func(weight float64) bool { return weight > 0 }) {
			continue
		}
		candidate := -1
		for j, track := range tracks {
			if !track.canMatchOutwardDigits(at, detection) {
				continue
			}
			if candidate != -1 {
				candidate = -1
				break
			}
			candidate = j
		}
		if candidate < 0 {
			continue
		}
		for other, alternative := range detections {
			if other != i && (weights[other][candidate] > 0 || tracks[candidate].canMatchOutwardDigits(at, alternative)) {
				candidate = -1
				break
			}
		}
		if candidate >= 0 {
			weights[i][candidate] = .10
		}
	}
}

func (t *plateTrack) recoverOutwardDigitPair() (observation, bool) {
	if t.frames < 2 || t.frames > 12 || t.last.Sub(t.first) > 1500*time.Millisecond {
		return observation{}, false
	}
	var best observation
	for i := 1; i < len(t.recentFrames); i++ {
		first, second := t.recentFrames[i-1], t.recentFrames[i]
		if first.id == "" || second.id == "" || first.id == second.id {
			continue
		}
		number := outwardDigitPair(first.plate, second.plate, second.at.Sub(first.at))
		if number == "" && i >= 2 {
			prior := t.recentFrames[i-2]
			if prior.id != second.id && weakOutwardSequence(prior.plate, second.plate.SequenceNumber) &&
				acceleratingOutward(prior, first, second.plate, second.at) {
				number = readOutwardPair(first.plate, second.plate, second.at.Sub(first.at), true)
			}
		}
		if number == "" {
			continue
		}
		if best.reading.Number != "" && best.reading.Number != number {
			return observation{}, false
		}
		best = observation{reading: plate.Reading{Number: number, Confidence: .80}, box: second.plate.Box,
			frame: second.id, at: second.at, detector: second.plate.Score, appearance: second.plate.appearance}
	}
	if !best.reading.Readable() {
		return observation{}, false
	}
	for _, frame := range t.recentFrames {
		view := frame.plate
		if view.Localized != nil && view.Localized.Number != best.reading.Number && view.Localized.Confidence >= .84 &&
			view.NumberScores[view.Localized.Number] >= .82 {
			return observation{}, false
		}
		if view.Reading.Number != best.reading.Number && view.Reading.Confidence >= .84 &&
			view.NumberScores[view.Reading.Number] >= .85 {
			return observation{}, false
		}
	}
	return best, true
}
