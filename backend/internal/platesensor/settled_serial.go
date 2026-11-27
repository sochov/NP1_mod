package platesensor

import (
	"bytes"
	"time"
)

// A distant followed plate can lose a different stroke on each primary read.
// Use a sharply localized sequence as the anchor, then look for the same four
// digits from the number readers across separate exposures in this track.
func (t *plateTrack) confirmsSettledSerial(best observation) bool {
	number := best.reading.Number
	if len(number) != 4 || !t.hasVerification || len(t.recentFrames) < 4 || len(t.recentFrames) > overlapHistoryLimit ||
		t.votes[number].leadingDigitConflict || t.localizedConflicts(best) || t.hasWiderReadConflict(best) {
		return false
	}
	var first, last trackFrame
	var views, primaryReads, strongChecks int
	var localizedAnchor bool
	var seen [overlapHistoryLimit]struct {
		id     string
		pixels []byte
	}
	for _, f := range t.recentFrames {
		p := f.plate
		if p.Localized != nil && p.Localized.Number != number && p.Localized.Confidence >= .80 ||
			p.Verifier != nil && p.Verifier.Number != number && p.SequenceNumber == p.Verifier.Number && p.Verifier.Confidence >= .65 && p.NumberScores[p.SequenceNumber] >= .90 ||
			p.Reading.Number != number && p.Reading.Confidence >= .75 {
			return false
		}
		x, y, w, h := p.Box[0], p.Box[1], p.Box[2], p.Box[3]
		if f.id == "" || f.id == last.id || !f.at.After(last.at) || w < 40 || w > 75 || h <= 0 || w < h || 5*w > 12*h ||
			x < 0 || y < 0 || x+w > FrameWidth || y+h >= FrameHeight*9/10 || p.Score < .35 ||
			p.Verifier == nil || p.Verifier.Number != number || p.Verifier.Confidence < .40 || p.SequenceNumber != number || p.NumberScores[number] < .75 {
			continue
		}
		if len(p.appearance) != appearanceWidth*appearanceHeight {
			continue
		}
		duplicate := false
		for _, previous := range seen[:views] {
			if previous.id == f.id || bytes.Equal(previous.pixels, p.appearance) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		seen[views].id, seen[views].pixels = f.id, p.appearance
		if first.id == "" {
			first = f
		} else if f.at.Sub(last.at) > time.Second || stationaryDistance(first.plate.Box, p.Box) > .65 {
			return false
		}
		last = f
		views++
		if p.Reading.Number == number && p.Reading.Confidence >= .35 {
			primaryReads++
		}
		if p.Verifier.Confidence >= max(f.minConfidence, .70) && p.NumberScores[number] >= .94 {
			strongChecks++
		}
		if p.Reading.Number == number && p.Reading.Confidence >= .50 && p.Localized != nil && p.Localized.Number == number &&
			p.Localized.Confidence >= .90 && p.Verifier.Confidence >= max(f.minConfidence, .70) && p.NumberScores[number] >= .95 &&
			p.DigitEdges != nil && *p.DigitEdges >= .16 && !digitBandObscured(p.appearance) {
			localizedAnchor = true
		}
	}
	return views >= 4 && primaryReads >= 2 && strongChecks >= 2 && localizedAnchor && last.at.Sub(first.at) >= time.Second
}
