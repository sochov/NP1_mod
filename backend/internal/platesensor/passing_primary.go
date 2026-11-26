package platesensor

import (
	"math"
	"time"
)

// With only the primary reader available, the same four digits can appear
// first on a clear crop and again when the car has passed beyond the normal
// displacement gate. An ambiguous assignment must not corroborate a reading.
func usablePassingPrimary(p EnginePlate) bool {
	x, y, w, h := p.Box[0], p.Box[1], p.Box[2], p.Box[3]
	return p.Verifier == nil && p.Reading.Readable() && len(p.Reading.Number) == 4 && p.Reading.Confidence >= .50 &&
		p.Score >= .50 && w >= 40 && w <= 100 && h >= 20 && x >= 0 && x+w <= FrameWidth && y >= 0 && y+h <= FrameHeight*9/10
}

func (t *plateTrack) matchesPassingPrimary(at time.Time, p EnginePlate) bool {
	dt := at.Sub(t.last)
	if dt < 70*time.Millisecond || dt > 240*time.Millisecond || t.hasVerification || t.sent != 0 || t.frames != 1 || t.support != 1 ||
		t.best.reading.Confidence < .60 || t.best.detector < .50 || t.best.reading.Number != p.Reading.Number || !usablePassingPrimary(p) {
		return false
	}
	a, b := t.best.reading, p.Reading
	for _, names := range [3][2]string{{a.Region, b.Region}, {a.Class, b.Class}, {a.Kana, b.Kana}} {
		if names[0] != "" && names[1] != "" && names[0] != names[1] {
			return false
		}
	}
	old, next := t.best.box, p.Box
	x, y := center(old)
	nx, ny := center(next)
	dx, dy := nx-x, ny-y
	w, h := float64(old[2]), float64(old[3])
	side := x - FrameWidth/2
	return w >= 40 && h >= 20 && math.Abs(side) >= .18*FrameWidth && side*dx > 0 && math.Abs(dx) >= .7*w && math.Abs(dx) <= 3*w &&
		dy >= 0 && dy <= 1.25*h && 5*next[2] >= 4*old[2] && 3*next[2] <= 5*old[2] && 5*next[3] >= 4*old[3] && 3*next[3] <= 5*old[3]
}
