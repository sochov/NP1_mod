package platesensor

import "time"

// The primary OCR can clear the registration threshold on only the sharpest
// image of a moving plate. Weaker reads may corroborate that number without
// casting new serial votes or contributing uncertain optional fields.
func (t *plateTrack) confirmsRepeatedSerial(best observation) bool {
	number := best.reading.Number
	if len(number) != 4 || t.reads != 1 || !t.votes[number].hasDigitDetail() ||
		t.votes[number].leadingDigitConflict || t.localizedConflicts(best) || t.hasWiderReadConflict(best) {
		return false
	}
	var first, last trackFrame
	var primaryMatches, sequenceMatches, checked int
	var anchor bool
	for _, frame := range t.recentFrames {
		p := frame.plate
		if p.Localized != nil && p.Localized.Number != number && p.Localized.Confidence >= .80 ||
			p.SequenceNumber != "" && p.SequenceNumber != number && p.Verifier != nil &&
				p.Verifier.Number == p.SequenceNumber && p.NumberScores[p.SequenceNumber] >= .92 {
			return false
		}
		x, y, w, h := p.Box[0], p.Box[1], p.Box[2], p.Box[3]
		if frame.id == "" || w < 40 || w < h || x < 0 || y < 0 || x+w > FrameWidth || y+h >= FrameHeight*9/10 ||
			p.Score < .35 || p.Verifier == nil || p.Verifier.Number != number || p.Verifier.Confidence < .35 {
			continue
		}
		if last.id != "" && (frame.id == last.id || !frame.at.After(last.at)) {
			continue
		}
		if first.id == "" || frame.at.Sub(last.at) > 350*time.Millisecond {
			first, primaryMatches, sequenceMatches, checked, anchor = frame, 0, 0, 0, false
		}
		last = frame
		checked++
		primary := p.Reading.Number == number && p.Reading.Confidence >= .40 && p.Verifier.Confidence >= .40
		if primary {
			primaryMatches++
		}
		sequence := p.SequenceNumber == number && p.NumberScores[number] >= .75
		if sequence {
			sequenceMatches++
		}
		if primary && sequence && frame.id == best.frame && p.DigitEdges != nil && *p.DigitEdges >= .08 &&
			!digitBandObscured(p.appearance) && p.Reading.Confidence >= max(.70, frame.minConfidence) &&
			p.Verifier.Confidence >= .50 && p.NumberScores[number] >= .90 {
			anchor = true
		}
	}
	return anchor && checked >= 3 && primaryMatches >= 2 && sequenceMatches >= 2 &&
		last.at.Sub(first.at) >= 200*time.Millisecond && stationaryDistance(first.plate.Box, last.plate.Box) >= .35
}
