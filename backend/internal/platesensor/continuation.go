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
	if !t.possibleFollowing(next, gap) || !next.hasVerification || !next.numberVerified() ||
		next.best.reading.Confidence < .82 || next.support < 4 || next.last.Sub(next.first) < 750*time.Millisecond {
		return false
	}
	matches, conflicts := t.compareConfirmedFields(next)
	return conflicts < 2 && (len(t.best.reading.Number) == 4 || conflicts == 0 && matches >= 1)
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
