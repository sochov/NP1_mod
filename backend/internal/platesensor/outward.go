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
	if len(number) != 4 || number != last.SequenceNumber || dt <= 0 || dt > 175*time.Millisecond {
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
		if min(first.Score, last.Score) < .62 || !confirmedFastOutwardReads(first, last, number) {
			return ""
		}
		maxShift, maxDrop = 2.65, 1.12
	} else if min(first.NumberScores[number], last.NumberScores[number]) < .94 {
		return ""
	}
	if math.Abs(side) < .20*FrameWidth || side*dx <= 0 || first.Score < .40 || last.Score < .40 ||
		w < 35 || h < 18 || nw > 64 || nw < .95*w || nw > 1.3*w || nh < h || nh > 1.35*h ||
		math.Abs(dx) < 1.3*w || math.Abs(dx) > maxShift*max(w, nw) || dy < .65*max(h, nh) || dy > maxDrop*max(h, nh) {
		return ""
	}
	return number
}

func confirmedFastOutwardReads(first, last EnginePlate, number string) bool {
	if last.Reading.Number != number || last.Reading.Confidence < .70 {
		return false
	}
	if first.Reading.Number == number && first.Reading.Confidence >= .40 &&
		min(first.NumberScores[number], last.NumberScores[number]) >= .97 &&
		min(first.Verifier.Confidence, last.Verifier.Confidence) >= .65 {
		return true
	}
	// The independent digit reader can confirm the first crop while the
	// later crop provides the strong primary and numeric reads.
	return oneDigitDiffers(first.Reading.Number, number) && first.Reading.Confidence <= .78 &&
		first.Localized != nil && first.Localized.Number == number && first.Localized.Confidence >= .84 &&
		first.NumberScores[number] >= .90 && first.Verifier.Confidence >= .48 &&
		last.Reading.Confidence >= .80 && last.Verifier.Confidence >= .77 && last.NumberScores[number] >= .985 &&
		first.DigitEdges != nil && last.DigitEdges != nil && min(*first.DigitEdges, *last.DigitEdges) >= .18
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

func acceleratedOutwardPair(history []trackFrame, next EnginePlate, at time.Time) string {
	n := len(history)
	if n < 2 {
		return ""
	}
	prior, previous := history[n-2], history[n-1]
	number := readOutwardPair(previous.plate, next, at.Sub(previous.at), true)
	if number == "" || !acceleratingOutward(prior, previous, next, at) {
		return ""
	}
	if weakOutwardSequence(prior.plate, number) {
		return number
	}
	if n >= 3 {
		earlier := history[n-3]
		if earlier.id != previous.id && weakOutwardSequence(earlier.plate, number) &&
			earlier.plate.NumberScores[number] >= .68 && acceleratingOutward(earlier, prior, previous.plate, previous.at) {
			return number
		}
	}
	return ""
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
	if acceleratedOutwardPair(t.recentFrames, next, at) != "" {
		return true
	}
	// Keep a weak start attached to the first supported reading. This edge
	// contributes geometry only until another frame corroborates the serial.
	return t.support == 0 && t.frames >= 4 && weakOutwardSequence(previous.plate, number) &&
		next.Reading.Number == number && next.Reading.Confidence >= .42 && next.Verifier != nil &&
		next.Verifier.Number == number && next.Verifier.Confidence >= .65 && next.NumberScores[number] >= .97 &&
		next.Localized != nil && next.Localized.Number == number && next.Localized.Confidence >= .74 &&
		next.Box[2] >= 45 && next.Box[2] <= 64 && next.Box[2] >= previous.plate.Box[2] && 4*next.Box[2] <= 5*previous.plate.Box[2] &&
		next.Box[3] >= previous.plate.Box[3] && 4*next.Box[3] <= 6*previous.plate.Box[3] &&
		acceleratingOutward(prior, previous, next, at)
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
		if number == "" && (i < 2 || t.recentFrames[i-2].id != second.id) &&
			(i < 3 || t.recentFrames[i-3].id != second.id) {
			number = acceleratedOutwardPair(t.recentFrames[:i], second.plate, second.at)
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

// A distant side-on plate may leave the image without ever reaching the wide
// verification crop size. Confirm it at the end of the track using two
// localized reads and four detailed, independent frames with whole-plate agreement.
func (t *plateTrack) confirmNarrowDeparture() bool {
	number := t.best.reading.Number
	vote := t.votes[number]
	if len(number) != 4 || t.support < 2 || t.conflict || t.ambiguous || vote.leadingDigitConflict ||
		!vote.hasDigitDetail() || t.frames > 24 || t.last.Sub(t.first) > 3*time.Second {
		return false
	}
	for _, sample := range t.recentFrames {
		p := sample.plate
		if p.LocalizedLeadConflict != "" || p.SequenceNumber != number && p.NumberScores[p.SequenceNumber] >= .75 ||
			p.Reading.Readable() && p.Reading.Number != number && p.Reading.Confidence >= .65 ||
			p.Verifier != nil && p.Verifier.Readable() && p.Verifier.Number != number && p.Verifier.Confidence >= .65 ||
			p.Localized != nil && p.Localized.Number != number && p.Localized.Confidence >= .80 {
			return false
		}
	}
	for i := 2; i < len(t.recentFrames); i++ {
		a, b, c := t.recentFrames[i-2], t.recentFrames[i-1], t.recentFrames[i]
		if a.id == "" || b.id == "" || c.id == "" || a.id == b.id || a.id == c.id || b.id == c.id ||
			!narrowDepartureStep(a, b) || !narrowDepartureStep(b, c) {
			continue
		}
		if !matchesDepartureRead(a.plate, number, .45, .40, .90) || a.plate.Localized == nil ||
			a.plate.Localized.Number != number || a.plate.Localized.Confidence < .90 ||
			!matchesDepartureRead(b.plate, number, .75, .70, .98) || b.plate.Localized == nil ||
			b.plate.Localized.Number != number || b.plate.Localized.Confidence < .82 ||
			!matchesDepartureRead(c.plate, number, .80, .78, .98) ||
			b.plate.DigitEdges == nil || c.plate.DigitEdges == nil || min(*b.plate.DigitEdges, *c.plate.DigitEdges) < .38 {
			continue
		}
		var supporting [4]string
		var previous trackFrame
		count := 0
		for _, sample := range t.recentFrames[i-1:] {
			p := sample.plate
			if sample.at.Sub(b.at) > 600*time.Millisecond {
				break
			}
			if sample.id == "" || slices.Contains(supporting[:count], sample.id) || p.Box[2] < 35 || p.Box[2] > 44 ||
				p.Box[3] <= 0 || p.Score < .55 || p.DigitEdges == nil || *p.DigitEdges < .38 ||
				!matchesDepartureRead(p, number, .75, .65, .96) {
				continue
			}
			if count > 0 && !narrowDepartureMotion(previous, sample, .50) {
				break
			}
			supporting[count] = sample.id
			previous = sample
			count++
			if count == len(supporting) {
				return true
			}
		}
	}
	return false
}

func matchesDepartureRead(p EnginePlate, number string, primary, verify, sequence float64) bool {
	return p.Reading.Number == number && p.Reading.Confidence >= primary &&
		p.Verifier != nil && p.Verifier.Number == number && p.Verifier.Confidence >= verify &&
		p.SequenceNumber == number && p.NumberScores[number] >= sequence
}

func narrowDepartureStep(previous, next trackFrame) bool {
	a, b := previous.plate, next.plate
	if a.Score < .60 || b.Score < .60 ||
		a.Box[2] < 35 || a.Box[2] > 44 || b.Box[2] < 35 || b.Box[2] > 44 ||
		2*a.Box[2] < 3*a.Box[3] || 2*b.Box[2] < 3*b.Box[3] ||
		100*a.Box[1] >= 77*FrameHeight || 100*b.Box[1] >= 77*FrameHeight {
		return false
	}
	return narrowDepartureMotion(previous, next, .70)
}

func narrowDepartureMotion(previous, next trackFrame, minShift float64) bool {
	dt := next.at.Sub(previous.at)
	if dt < 80*time.Millisecond || dt > 200*time.Millisecond {
		return false
	}
	a, b := previous.plate.Box, next.plate.Box
	x, y := center(a)
	nx, ny := center(b)
	dx, dy := nx-x, ny-y
	side := x - FrameWidth/2
	width := float64(max(a[2], b[2]))
	return math.Abs(side) >= .20*FrameWidth && dx*side > 0 &&
		math.Abs(dx) >= minShift*width && math.Abs(dx) <= 1.85*width && dy >= 0 && dy <= .55*float64(b[3])
}
