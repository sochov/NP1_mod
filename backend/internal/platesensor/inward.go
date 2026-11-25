package platesensor

import (
	"math"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

// Use both independently positioned digits and the verifier before asking
// the tracker to bridge a wider step. A weak primary reading is inconclusive.
func inwardDigitRead(p EnginePlate) string {
	if p.Localized == nil || p.Localized.Confidence < .86 || p.Verifier == nil ||
		len(p.Localized.Number) != 4 || p.Verifier.Number != p.Localized.Number ||
		p.Verifier.Confidence < .35 || p.NumberScores[p.Localized.Number] < .55 || p.Score < .55 {
		return ""
	}
	w, h := p.Box[2], p.Box[3]
	if w < 36 || w > 60 || h < 18 || 4*w < 5*h || w > 2*h {
		return ""
	}
	return p.Localized.Number
}

func confirmsInwardDigitStep(a, b EnginePlate, dt time.Duration, number string) bool {
	if dt <= 0 || dt > 250*time.Millisecond ||
		max(inwardSequenceProof(a, number), inwardSequenceProof(b, number)) < .84 {
		return false
	}
	x, y := center(a.Box)
	nx, ny := center(b.Box)
	dx := nx - x
	side := x - FrameWidth/2
	w, h := float64(a.Box[2]), float64(a.Box[3])
	nw, nh := float64(b.Box[2]), float64(b.Box[3])
	return math.Abs(side) >= .18*FrameWidth && side*dx < 0 && side*(nx-FrameWidth/2) > 0 &&
		math.Abs(dx) >= .72*max(w, nw) && math.Abs(dx) <= 2*max(w, nw) &&
		y-ny >= .10*min(h, nh) && y-ny <= .65*max(h, nh) &&
		nw >= .74*w && nw <= 1.10*w && nh >= .66*h && nh <= 1.10*h
}

func inwardSequenceProof(p EnginePlate, number string) float64 {
	if p.SequenceNumber != number {
		return 0
	}
	return p.NumberScores[number]
}

func (t *plateTrack) inwardDigitMatch(at time.Time, p EnginePlate) float64 {
	if t.sent != 0 || len(t.recentFrames) == 0 {
		return 0
	}
	number := inwardDigitRead(p)
	prior := t.recentFrames[len(t.recentFrames)-1]
	if number == "" || !prior.at.Equal(t.last) || inwardDigitRead(prior.plate) != number ||
		!confirmsInwardDigitStep(prior.plate, p, at.Sub(prior.at), number) {
		return 0
	}
	return .10
}

func (t *plateTrack) recoverInwardDigits() (observation, bool) {
	if t.frames > 14 || t.last.Sub(t.first) > 2*time.Second {
		return observation{}, false
	}
	var previous trackFrame
	var best observation
	for _, sample := range t.recentFrames {
		number := inwardDigitRead(sample.plate)
		if number == "" || sample.id == "" {
			continue
		}
		if best.reading.Number != "" && best.reading.Number != number {
			return observation{}, false
		}
		if previous.id != "" && previous.id != sample.id && inwardDigitRead(previous.plate) == number &&
			confirmsInwardDigitStep(previous.plate, sample.plate, sample.at.Sub(previous.at), number) {
			representative := sample
			if inwardSequenceProof(previous.plate, number) > inwardSequenceProof(sample.plate, number) {
				representative = previous
			}
			best = observation{reading: plate.Reading{Number: number, Confidence: .80}, box: representative.plate.Box,
				frame: representative.id, at: representative.at, detector: representative.plate.Score, appearance: representative.plate.appearance}
		}
		previous = sample
	}
	if !best.reading.Readable() {
		return observation{}, false
	}
	number := best.reading.Number
	for _, sample := range t.recentFrames {
		view := sample.plate
		if supported := inwardDigitRead(view); supported != "" && supported != number {
			return observation{}, false
		}
		if view.Localized != nil && view.Localized.Number != number && view.Localized.Confidence >= .86 &&
			view.NumberScores[view.Localized.Number] >= .80 {
			return observation{}, false
		}
		if view.SequenceNumber != "" && view.SequenceNumber != number && view.Verifier != nil &&
			view.Verifier.Number == view.SequenceNumber && view.NumberScores[view.SequenceNumber] >= .91 {
			return observation{}, false
		}
	}
	return best, true
}
