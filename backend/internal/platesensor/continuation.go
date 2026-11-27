package platesensor

import (
	"math"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

const followingMaxGap = 150 * time.Second
const longFollowingGap = 4 * time.Minute
const turnReturnMaxGap = 90 * time.Second
const forwardReturnMaxGap = 75 * time.Second

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
	// Shorter serials occur more often, so use a smaller time window.
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
	if len(t.best.reading.Number) == 4 {
		return conflicts < 2
	}
	if conflicts != 0 {
		return false
	}
	return matches != 0 || t.rejoinShortFollowing(next)
}

// Sustained following and repeated digit checks can corroborate a short
// serial across a brief loss of the plate's optional fields.
func (t *plateTrack) rejoinShortFollowing(next *plateTrack) bool {
	number := t.best.reading.Number
	if len(number) != 3 || t.support < 24 || next.support < 8 ||
		t.last.Sub(t.first) < 8*time.Second || next.last.Sub(next.first) < 1500*time.Millisecond {
		return false
	}
	for _, track := range [2]*plateTrack{t, next} {
		if track.best.reading.Confidence < .85 || track.localized[number] < .90 ||
			track.votes[number].wideVerification < .95 || track.localizedFrames[number][1] == "" {
			return false
		}
	}
	ax, ay := center(t.box)
	bx, by := center(next.firstBox)
	return math.Abs(ax-bx) <= .08*FrameWidth && math.Abs(ay-by) <= .05*FrameHeight
}

// Only established four-digit lead tracks carry enough history to wait longer
// for the same rear shape to become visible again.
func (t *plateTrack) hasLongFollowingEvidence() bool {
	number := t.best.reading.Number
	return len(number) == 4 && t.last.Sub(t.first) >= 20*time.Second && t.support >= 20 &&
		len(t.appearanceSamples) >= 3 && t.localized[number] >= .90 && t.votes[number].wideVerification >= .97
}

func (t *plateTrack) longFollowingPosition(next *plateTrack, gap time.Duration) bool {
	if gap <= followingMaxGap || gap > longFollowingGap || !t.hasLongFollowingEvidence() ||
		!t.establishedFollowing() || !forwardPlate(next.firstBox) || !forwardPlate(next.best.box) {
		return false
	}
	ax, ay := center(t.box)
	bx, by := center(next.firstBox)
	return math.Abs(ax-FrameWidth/2) <= .12*FrameWidth && math.Abs(bx-FrameWidth/2) <= .12*FrameWidth &&
		math.Abs(ax-bx) <= .08*FrameWidth && math.Abs(ay-by) <= .075*FrameHeight
}

func (t *plateTrack) rejoinLongFollowing(next *plateTrack, gap time.Duration) bool {
	number := t.best.reading.Number
	if !t.longFollowingPosition(next, gap) || next.support < 8 ||
		next.best.reading.Confidence < .88 || next.localized[number] < .90 ||
		next.votes[number].wideVerification < .97 || !next.numberVerified() {
		return false
	}
	_, conflicts := t.compareConfirmedFields(next)
	return conflicts < 2 && matchingTrackPoses(number, t.recentFrames, next.recentFrames, .88, .88)
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

func (t *plateTrack) hasStrongTurnRead() bool {
	number := t.best.reading.Number
	return t.support >= 24 && t.best.reading.Confidence >= .94 &&
		t.localizedFrames[number][1] != "" && t.votes[number].wideVerification >= .98 && t.numberVerified()
}

// After a turn, the same car can leave by one side of the image and reappear
// with a substantially different horizontal position.
func (t *plateTrack) possibleTurnContinuation(next *plateTrack, gap time.Duration) bool {
	number := t.best.reading.Number
	if t == next || t.sent == 0 || t.ambiguous || next.ambiguous || len(number) != 4 ||
		next.best.reading.Number != number || gap < 18*time.Second || gap > turnReturnMaxGap ||
		t.support < 8 || t.last.Sub(t.first) < 4*time.Second ||
		t.best.reading.Confidence < .88 || t.localized[number] < .90 ||
		t.votes[number].wideVerification < .97 || !t.numberVerified() ||
		(!forwardPlate(t.firstBox) && !forwardPlate(t.best.box)) {
		return false
	}
	x, y := center(t.box)
	nx, ny := center(next.firstBox)
	w, nw := float64(t.box[2]), float64(next.firstBox[2])
	if math.Abs(x-FrameWidth/2) < .25*FrameWidth || math.Abs(x-nx) < .24*FrameWidth ||
		math.Abs(y-ny) > .10*FrameHeight || nw < .55*w || nw > 1.70*w {
		return false
	}
	if gap > forwardReturnMaxGap || !forwardPlate(next.firstBox) {
		oppositeSide := (x-FrameWidth/2)*(nx-FrameWidth/2) < 0 && math.Abs(nx-FrameWidth/2) >= .32*FrameWidth
		if !oppositeSide || !t.hasStrongTurnRead() {
			return false
		}
	}
	_, conflicts := t.compareConfirmedFields(next)
	return conflicts == 0
}

func (t *plateTrack) rejoinAfterTurn(next *plateTrack, gap time.Duration) bool {
	if !t.possibleTurnContinuation(next, gap) {
		return false
	}
	number := next.best.reading.Number
	if next.last.Sub(next.first) < 750*time.Millisecond || next.localized[number] < .90 || !next.numberVerified() {
		return false
	}
	if gap > forwardReturnMaxGap || !forwardPlate(next.firstBox) {
		first, _ := center(next.firstBox)
		now, _ := center(next.box)
		inward := (first-FrameWidth/2)*(first-now) > 0 && math.Abs(first-now) >= .06*FrameWidth
		return inward && next.support >= 6 && next.best.reading.Confidence >= .90 &&
			next.localized[number] >= .93 && next.localizedFrames[number][1] != "" &&
			next.votes[number].wideVerification >= .98
	}
	if next.support >= 5 && next.best.reading.Confidence >= .88 &&
		next.localizedFrames[number][1] != "" && next.votes[number].wideVerification >= .97 {
		return true
	}
	// A strongly verified outgoing track can survive a dimmer return when
	// the returning number also has agreement across readers.
	return t.hasStrongTurnRead() && next.last.Sub(next.first) >= 1500*time.Millisecond &&
		next.support >= 4 && next.best.reading.Confidence >= .66 && next.votes[number].wideVerification >= .95
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
	return matchingTrackPoses(number, t.recentFrames, next.recentFrames, .80, .80)
}

// Keep a weak but separately checked record briefly where it left the side of
// the image. Later, clearer frames can corroborate its identity.
func (t *plateTrack) canRetainWeakSideFragment() bool {
	r := t.best.reading
	x, _ := center(t.box)
	return t.sent > 0 && len(r.Number) == 4 && r.Confidence >= .65 && r.Confidence < .70 && t.support >= 4 &&
		math.Abs(x-FrameWidth/2) >= .34*FrameWidth && t.localized[r.Number] >= .90 &&
		t.votes[r.Number].wideVerification >= .94 && t.numberVerified()
}

func (t *plateTrack) rejoinWeakSideFragment(next *plateTrack, gap time.Duration) bool {
	number := t.best.reading.Number
	if gap <= trackRejoinGap || gap > trackRecentGap || len(number) != 4 ||
		stationaryDistance(t.box, next.firstBox) > 1.1 || next.support < 5 || next.localized[number] < .92 ||
		next.localizedFrames[number][1] == "" || next.votes[number].wideVerification < .97 || !next.numberVerified() {
		return false
	}
	x, _ := center(t.box)
	strongSide := gap >= 5*time.Second && t.sent > 0 && math.Abs(x-FrameWidth/2) >= .34*FrameWidth && t.hasStrongTurnRead()
	if !strongSide && !t.canRetainWeakSideFragment() {
		return false
	}
	if _, conflicts := t.compareConfirmedFields(next); conflicts != 0 {
		return false
	}
	return matchingTrackPoses(number, t.recentFrames, next.recentFrames, .72, .80)
}

func matchingTrackPoses(number string, outgoing, incoming []trackFrame, priorMin, nextMin float64) bool {
	const history = 16
	outgoing = outgoing[max(0, len(outgoing)-history):]
	incoming = incoming[:min(history, len(incoming))]
	var firstA, firstB time.Time
	for _, a := range outgoing {
		p := a.plate
		if p.Localized == nil || p.Localized.Number != number || p.Localized.Confidence < priorMin || a.at.Equal(firstA) {
			continue
		}
		for _, b := range incoming {
			q := b.plate
			if q.Localized == nil || q.Localized.Number != number || q.Localized.Confidence < nextMin || b.at.Equal(firstB) ||
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
// Left and right halves of the upper row, then the lower-left kana.
var fieldPatchAreas = [3][4]int{{13, 14, 24, 19}, {24, 14, 34, 19}, {13, 19, 17, 24}}

func fieldVisible(view []byte, field int) bool {
	if len(view) != appearanceWidth*appearanceHeight {
		return true
	}
	r := fieldPatchAreas[field]
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
