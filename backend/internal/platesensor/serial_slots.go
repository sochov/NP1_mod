package platesensor

import (
	"math"
	"time"
)

// On a passing car each image can lose a different stroke. Consider the last
// sharp crop with a full number read by all three readers, and count an equal
// fraction of a vote per reader in each image to avoid overweighting one view.
func (t *plateTrack) recoverCorroboratedDigits() (observation, bool) {
	if !t.conflict || t.frames < 4 || t.frames > 12 || t.last.Sub(t.first) > 1500*time.Millisecond {
		return observation{}, false
	}
	var end trackFrame
	for _, f := range t.recentFrames {
		if f.plate.Score >= .35 && f.plate.Verifier != nil {
			end = f
		}
	}
	p := end.plate
	n := p.Reading.Number
	vote := t.votes[n]
	if len(n) != 4 || !movingSerialCrop(end) || p.Box[2] < 40 || p.Reading.Confidence < .55 || p.Verifier.Number != n ||
		p.Verifier.Confidence < .60 || p.SequenceNumber != n || p.NumberScores[n] < .93 || vote.best.frame != end.id ||
		vote.best.reading.Confidence < end.minConfidence || vote.leadingDigitConflict || t.localizedConflicts(vote.best) {
		return observation{}, false
	}
	x0, _ := center(t.firstBox)
	x1, _ := center(p.Box)
	if (x0-FrameWidth/2)*(x1-x0) <= 0 || math.Abs(x1-x0) < 2*float64(p.Box[2]) || p.Box[2] <= t.firstBox[2] || p.Box[1]+p.Box[3] >= FrameHeight*9/10 {
		return observation{}, false
	}
	var shares [4]float64
	var disagreed [4]bool
	var first, last trackFrame
	var primary, views int
	for _, f := range t.recentFrames {
		q := f.plate
		if q.Localized != nil && q.Localized.Confidence >= .80 && q.Localized.Number != n {
			return observation{}, false
		}
		if q.Verifier == nil || q.Score < .35 || q.Box[2] < 32 || f.id == "" || f.id == last.id || !f.at.After(last.at) {
			continue
		}
		if q.Reading.Number != n && q.Reading.Number == q.Verifier.Number && q.Reading.Confidence >= .75 && q.Verifier.Confidence >= .75 {
			return observation{}, false
		}
		var matches [4]int
		readers := 0
		for _, r := range [...]struct {
			num   string
			score float64
		}{{q.Reading.Number, q.Reading.Confidence}, {q.Verifier.Number, q.Verifier.Confidence}, {q.SequenceNumber, q.NumberScores[q.SequenceNumber]}} {
			if r.score < .42 || r.num == "" {
				continue
			}
			if r.num != n && !oneDigitDiffers(r.num, n) {
				return observation{}, false
			}
			readers++
			for i := range shares {
				if r.num[i] == n[i] {
					matches[i]++
				} else {
					disagreed[i] = true
				}
			}
		}
		if readers == 0 {
			continue
		}
		if views == 0 {
			first = f
		} else if f.at.Sub(last.at) > 350*time.Millisecond {
			return observation{}, false
		}
		last = f
		views++
		if q.Reading.Number == n && q.Reading.Confidence >= .42 {
			primary++
		}
		for i, m := range matches {
			shares[i] += float64(m) / float64(readers)
		}
	}
	if views < 4 || primary < 2 || last.at.Sub(first.at) < 300*time.Millisecond {
		return observation{}, false
	}
	disputed := 0
	for i, s := range shares {
		if s < float64(views)*.60 {
			return observation{}, false
		}
		if disagreed[i] {
			disputed++
		}
	}
	return vote.best, disputed >= 2
}
