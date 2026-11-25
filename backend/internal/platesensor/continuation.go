package platesensor

import (
	"math"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

const followingMaxGap = 150 * time.Second

// Following traffic can vanish from plate OCR for many seconds. Allow a gap
// between stable reads in the camera's forward view even when glare prevents
// comparing the vehicle's appearance at the two ends.
func (t *plateTrack) establishedFollowing() bool {
	r := t.best.reading
	return t.sent > 0 && t.last.Sub(t.first) >= 4*time.Second && t.support >= 8 &&
		len(r.Number) >= 3 && r.Confidence >= .82 && t.hasVerification &&
		t.numberVerified() && forwardPlate(t.box)
}

func forwardPlate(box [4]int) bool {
	x, y := center(box)
	return math.Abs(x-FrameWidth/2) <= FrameWidth*.19 && y >= FrameHeight*.34 && y <= FrameHeight*.71
}

func (t *plateTrack) possibleFollowing(next *plateTrack, gap time.Duration) bool {
	if t.best.reading.Number != next.best.reading.Number {
		return false
	}
	return t.followingPositionMatches(next, gap)
}

func (t *plateTrack) followingPositionMatches(next *plateTrack, gap time.Duration) bool {
	if gap <= 0 || gap > followingMaxGap || !t.establishedFollowing() ||
		!forwardPlate(next.firstBox) || !forwardPlate(next.best.box) {
		return false
	}
	// Shorter serials occur more often, so they need corroborated metadata.
	if len(t.best.reading.Number) < 4 && gap > 40*time.Second {
		return false
	}
	ax, ay := center(t.box)
	bx, by := center(next.firstBox)
	return math.Abs(ax-bx) <= .19*FrameWidth && math.Abs(ay-by) <= .16*FrameHeight
}

func (t *plateTrack) rejoinFollowing(next *plateTrack, gap time.Duration) bool {
	if t.best.reading.Number != next.best.reading.Number {
		return false
	}
	forward := t.followingPositionMatches(next, gap)
	if (!forward && !t.rejoinFromLaneExit(next, gap)) || !next.hasVerification || !next.numberVerified() ||
		next.best.reading.Confidence < .82 || next.support < 4 || next.last.Sub(next.first) < 750*time.Millisecond {
		return false
	}
	matches, conflicts := t.compareConfirmedFields(next)
	if !forward {
		return conflicts == 0
	}
	return conflicts < 2 && (len(t.best.reading.Number) == 4 || conflicts == 0 && matches >= 1)
}

// A plate can resume close to its last position but only become readable
// while the vehicle moves to the side. Use the start of that trajectory to
// check continuity, with stronger reads on both fragments.
func (t *plateTrack) rejoinFromLaneExit(next *plateTrack, gap time.Duration) bool {
	number := t.best.reading.Number
	if gap <= trackRecentGap || gap > 45*time.Second || len(number) != 4 || !t.establishedFollowing() ||
		!forwardPlate(next.firstBox) || forwardPlate(next.best.box) || next.support < 6 ||
		min(t.best.reading.Confidence, next.best.reading.Confidence) < .88 ||
		min(t.votes[number].wideVerification, next.votes[number].wideVerification) < .97 || next.localized[number] < .90 {
		return false
	}
	ax, ay := center(t.box)
	bx, by := center(next.firstBox)
	x, _ := center(next.best.box)
	width := float64(t.box[2])
	return math.Abs(ax-bx) <= .10*FrameWidth && math.Abs(ay-by) <= .07*FrameHeight &&
		math.Abs(x-bx) >= .10*FrameWidth && (x-bx)*(bx-FrameWidth/2) > 0 &&
		float64(next.firstBox[2]) >= .60*width && float64(next.firstBox[2]) <= 1.55*width
}

// A verified plate can become too small to read, then briefly come back into
// range. Check both the distant boxes and the larger frames that carry OCR.
func (t *plateTrack) rejoinFromDistantBoxes(next *plateTrack, gap time.Duration) bool {
	number := t.best.reading.Number
	if gap <= trackRejoinGap || gap > 12*time.Second || len(number) != 4 || t.sent == 0 ||
		t.support < 5 || min(t.best.reading.Confidence, next.best.reading.Confidence) < .90 ||
		min(t.votes[number].wideVerification, next.votes[number].wideVerification) < .96 ||
		next.localizedFrames[number][1] == "" || !t.numberVerified() {
		return false
	}
	if max(t.box[2], next.firstBox[2]) > 42 || stationaryDistance(t.box, next.firstBox) > 2 ||
		stationaryDistance(t.best.box, next.best.box) > .85 {
		return false
	}
	if _, conflicts := t.compareConfirmedFields(next); conflicts != 0 {
		return false
	}
	var previous trackFrame
	for _, sample := range next.recentFrames {
		p := sample.plate
		if sample.id == "" || p.Score < .55 || p.SequenceNumber != number || p.NumberScores[number] < .92 ||
			p.Localized == nil || p.Localized.Number != number || p.Localized.Confidence < .84 ||
			p.Verifier == nil || p.Verifier.Number != number || p.Verifier.Confidence < .55 {
			continue
		}
		if elapsed := sample.at.Sub(previous.at); previous.id != "" && sample.id != previous.id &&
			elapsed > 0 && elapsed <= 250*time.Millisecond &&
			(p.Reading.Number == number && p.Reading.Confidence >= .90 ||
				previous.plate.Reading.Number == number && previous.plate.Reading.Confidence >= .90) {
			return true
		}
		previous = sample
	}
	return false
}

// A nearby car can leave the image while waiting alongside and return from
// the same edge. Compare pairs of frames at matching positions and scales.
func (t *plateTrack) rejoinReturningAtEdge(next *plateTrack, gap time.Duration) bool {
	number := t.best.reading.Number
	if gap <= trackRecentGap || gap > 70*time.Second || len(number) != 4 || t.sent == 0 ||
		min(t.support, next.support) < 6 || min(t.votes[number].wideVerification, next.votes[number].wideVerification) < .97 ||
		min(t.localized[number], next.localized[number]) < .94 ||
		t.localizedFrames[number][1] == "" || next.localizedFrames[number][1] == "" {
		return false
	}
	ax, ay := center(t.box)
	bx, by := center(next.firstBox)
	aStart, _ := center(t.firstBox)
	bEnd, _ := center(next.box)
	side := math.Copysign(1, ax-FrameWidth/2)
	if math.Abs(ax-FrameWidth/2) < .42*FrameWidth || side*(bx-FrameWidth/2) < .39*FrameWidth ||
		math.Abs(ax-bx) > .10*FrameWidth || math.Abs(ay-by) > .075*FrameHeight ||
		side*(ax-aStart) < .16*FrameWidth || side*(bx-bEnd) < .055*FrameWidth ||
		!t.numberVerified() || !next.numberVerified() {
		return false
	}
	if _, conflicts := t.compareConfirmedFields(next); conflicts != 0 {
		return false
	}
	const edgeHistory = 16
	outgoing := t.recentFrames[max(0, len(t.recentFrames)-edgeHistory):]
	incoming := next.recentFrames[:min(edgeHistory, len(next.recentFrames))]
	return matchingEdgePoses(number, outgoing, incoming)
}

func matchingEdgePoses(number string, outgoing, incoming []trackFrame) bool {
	var firstA, firstB time.Time
	for _, a := range outgoing {
		p := a.plate
		if p.Localized == nil || p.Localized.Number != number || p.Localized.Confidence < .80 || a.at.Equal(firstA) {
			continue
		}
		for _, b := range incoming {
			q := b.plate
			if q.Localized == nil || q.Localized.Number != number || q.Localized.Confidence < .80 || b.at.Equal(firstB) ||
				stationaryDistance(p.Box, q.Box) > 1 || washedOutNearPlate(p.appearance) && washedOutNearPlate(q.appearance) {
				continue
			}
			if appearanceMatches(p.appearance, q.appearance, 7, 1, 34, 16, 14, 4, .76) &&
				appearanceMatches(p.appearance, q.appearance, 5, 5, 37, 19, 11, 9, .80) {
				if !firstA.IsZero() {
					return true
				}
				firstA, firstB = a.at, b.at
				break
			}
		}
	}
	return false
}

func (t *plateTrack) compareConfirmedFields(next *plateTrack) (matches, conflicts int) {
	for i, votes := range t.votes[t.best.reading.Number].fields {
		a := confirmedField(votes)
		b := confirmedField(next.votes[next.best.reading.Number].fields[i])
		if a != "" && b != "" {
			if a == b {
				matches++
			} else {
				conflicts++
			}
		}
	}
	return matches, conflicts
}

func confirmedField(votes map[string]int) string {
	field := plate.FieldConsensus(votes, .85)
	if votes[field] < 3 {
		return ""
	}
	return field
}

// The appearance view is aligned to the plate. A field whose area is largely
// clipped cannot gain a majority merely because a car held its brakes on.
func fieldVisible(view []byte, field int) bool {
	if len(view) != appearanceWidth*appearanceHeight {
		return true
	}
	// Left and right halves of the upper row, then the lower-left kana.
	areas := [3][4]int{{13, 14, 24, 19}, {24, 14, 34, 19}, {13, 19, 17, 24}}
	r := areas[field]
	saturated := 0
	for row := r[1]; row < r[3]; row++ {
		for _, pixel := range view[row*appearanceWidth+r[0] : row*appearanceWidth+r[2]] {
			if pixel >= 234 {
				saturated++
			}
		}
	}
	return saturated*5 < (r[2]-r[0])*(r[3]-r[1])*4
}
