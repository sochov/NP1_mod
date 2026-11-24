package platesensor

import (
	"math"
	"time"
)

const (
	appearanceWidth       = 48
	appearanceHeight      = 28
	appearanceMaxGap      = 140 * time.Second
	overlapHistoryLimit   = 64
	appearanceSampleLimit = 6
)

// plateAppearance samples the surrounding vehicle from the NV12 luma plane.
func plateAppearance(frame []byte, box [4]int) []byte {
	x, y, w, h := box[0], box[1], box[2], box[3]
	if len(frame) < FrameWidth*FrameHeight || w < 35 || h < 12 || x < 0 || y < 0 || x+w > FrameWidth || y+h > FrameHeight {
		return nil
	}
	left, right := max(0, x-(55*w+50)/100), min(FrameWidth, x+(155*w+50)/100)
	top, bottom := max(0, y-(140*h+50)/100), min(FrameHeight, y+(140*h+50)/100)
	var columns [appearanceWidth * 2]int
	for i := range columns {
		columns[i] = left + (2*i+1)*(right-left)/(4*appearanceWidth)
	}
	pixels := make([]byte, appearanceWidth*appearanceHeight)
	for row := range appearanceHeight {
		a := (top + (4*row+1)*(bottom-top)/(4*appearanceHeight)) * FrameWidth
		b := (top + (4*row+3)*(bottom-top)/(4*appearanceHeight)) * FrameWidth
		for col := range appearanceWidth {
			c, d := columns[col*2], columns[col*2+1]
			pixels[row*appearanceWidth+col] = byte((int(frame[a+c]) + int(frame[a+d]) + int(frame[b+c]) + int(frame[b+d]) + 2) / 4)
		}
	}
	return pixels
}

func trackRetention(t *plateTrack) time.Duration {
	if len(t.best.appearance) == appearanceWidth*appearanceHeight && len(t.best.reading.Number) >= 3 && t.hasVerification && t.numberVerified() {
		return appearanceMaxGap
	}
	return trackRecentGap
}

func (t *plateTrack) stronglyVerifiedContinuation() bool {
	reading := t.best.reading
	return len(reading.Number) == 4 && reading.Confidence >= .70 && t.support >= 10 &&
		t.hasVerification && t.votes[reading.Number].wideVerification >= .95 && t.numberVerified()
}

func (t *plateTrack) canRemainRecent() bool {
	return !t.ambiguous && t.support > 0 &&
		(t.best.reading.Confidence >= .82 || t.support >= 3 && trackRetention(t) > trackRecentGap &&
			(t.best.reading.Confidence >= .77 || t.stronglyVerifiedContinuation()))
}

func (t *plateTrack) rememberAppearance(o observation, p EnginePlate) {
	verification := p.NumberScores[o.reading.Number]
	weakRead := o.reading.Confidence < .73 &&
		(o.reading.Confidence < .69 || p.SequenceNumber != o.reading.Number || verification < .89)
	if len(o.appearance) != appearanceWidth*appearanceHeight || len(o.reading.Number) < 3 || weakRead || verification < .75 || p.Score < .29 {
		return
	}
	samples := t.appearanceSamples
	if n := len(samples); n > 0 && o.at.Sub(samples[n-1].at) < time.Second {
		if o.weight > samples[n-1].weight {
			samples[n-1] = o
		}
		return
	}
	if len(samples) == appearanceSampleLimit {
		copy(samples, samples[1:])
		samples = samples[:appearanceSampleLimit-1]
	}
	t.appearanceSamples = append(samples, o)
}

// Give a possible continuation a few more OCR attempts before publishing a second record.
func waitingForAppearance(next *plateTrack, active, recent []*plateTrack) bool {
	if next.sent != 0 || next.support == 0 || len(next.best.reading.Number) < 3 || len(next.best.appearance) != appearanceWidth*appearanceHeight {
		return false
	}
	if !next.rejoinWaitAt.IsZero() {
		return next.last.Sub(next.rejoinWaitAt) < 16*time.Second
	}
	for _, tracks := range [][]*plateTrack{active, recent} {
		for _, old := range tracks {
			if old == nil || old == next || old.sent == 0 || !old.hasVerification || len(old.best.appearance) != appearanceWidth*appearanceHeight ||
				old.best.reading.Number != next.best.reading.Number || next.first.Sub(old.last) > appearanceMaxGap ||
				old.misses == 0 && next.first.Before(old.last) || next.first.Sub(old.last) < -2*time.Second || !canWaitAtScale(old.best, next.best) {
				continue
			}
			next.rejoinWaitAt = next.last
			return true
		}
	}
	return false
}

func (t *plateTrack) rejoinByAppearance(next *plateTrack, gap time.Duration) bool {
	older, newer := t.best, next.best
	if gap > appearanceMaxGap || gap < -2*time.Second || len(older.reading.Number) < 3 ||
		len(older.appearance) != appearanceWidth*appearanceHeight || len(newer.appearance) != appearanceWidth*appearanceHeight ||
		!t.hasVerification || !next.hasVerification || !t.numberVerified() || !next.numberVerified() ||
		max(t.support, next.support) < 2 {
		return false
	}
	if gap <= 0 && (t.misses == 0 || !next.last.After(t.last) || len(next.overlapFrames(t.last)) == 0 ||
		min(t.support, next.support) < 3 || stationaryDistance(older.box, newer.box) > .30) {
		return false
	}
	if (gap > 16*time.Second || len(older.reading.Number) == 3) && min(t.support, next.support) < 3 {
		return false
	}
	// A modest representative confidence can hide much stronger readings of
	// the same car. Require the repeated checks and two independent image pairs.
	if min(older.reading.Confidence, newer.reading.Confidence) < .74 {
		return gap > 0 && t.stronglyVerifiedContinuation() && next.stronglyVerifiedContinuation() &&
			max(older.reading.Confidence, newer.reading.Confidence) >= .89 && t.rejoinWithTwoViews(next, gap)
	}
	if matchingAppearance(older, newer, gap) {
		return true
	}
	for _, a := range t.appearanceSamples {
		if a.reading.Number != older.reading.Number {
			continue
		}
		for _, b := range next.appearanceSamples {
			if b.reading.Number == newer.reading.Number && matchingAppearance(a, b, gap) {
				return true
			}
		}
	}
	return t.rejoinWithTwoViews(next, gap)
}

func (t *plateTrack) rejoinWithTwoViews(next *plateTrack, gap time.Duration) bool {
	if gap <= 0 || len(t.best.reading.Number) != 4 || min(t.support, next.support) < 4 ||
		stationaryDistance(t.best.box, next.best.box) > 1.5 {
		return false
	}
	// A single comparison can be influenced by illumination and crop position.
	// Require support from two independently captured images on both sides.
	var firstA, firstB time.Time
	var sharedA, sharedB bool
	for _, a := range t.appearanceSamples {
		if a.reading.Number != t.best.reading.Number || len(a.appearance) != appearanceWidth*appearanceHeight {
			continue
		}
		for _, b := range next.appearanceSamples {
			if b.reading.Number != t.best.reading.Number || len(b.appearance) != appearanceWidth*appearanceHeight {
				continue
			}
			if firstA.Equal(a.at) && firstB.Equal(b.at) {
				continue
			}
			distance := stationaryDistance(a.box, b.box)
			if distance > 1.25 || gap > 60*time.Second && (distance > .85 || min(t.support, next.support) < 6) {
				continue
			}
			body, whole := .72, .75
			if gap > 60*time.Second {
				body, whole = .80, .81
			}
			if !appearanceMatches(a.appearance, b.appearance, 7, 1, 34, 16, 14, 4, body) ||
				!appearanceMatches(a.appearance, b.appearance, 5, 5, 37, 19, 11, 9, whole) {
				continue
			}
			if firstA.IsZero() {
				firstA, firstB = a.at, b.at
				continue
			}
			sameA, sameB := firstA.Equal(a.at), firstB.Equal(b.at)
			if !sameA && !sameB {
				return true
			}
			// Two matches sharing opposite ends of the first match also use
			// distinct images. The first match need not be part of the pair.
			sharedA, sharedB = sharedA || sameA, sharedB || sameB
			if sharedA && sharedB {
				return true
			}
		}
	}
	return false
}

func matchingAppearance(a, b observation, gap time.Duration) bool {
	distance := stationaryDistance(a.box, b.box)
	if len(a.reading.Number) == 3 {
		return gap >= 0 && gap <= 30*time.Second && distance <= .5 && min(a.reading.Confidence, b.reading.Confidence) >= .87 &&
			appearanceMatches(a.appearance, b.appearance, 7, 1, 34, 11, 14, 4, .78) &&
			appearanceMatches(a.appearance, b.appearance, 5, 5, 37, 19, 11, 9, .84)
	}
	if distance > 2.65 && (gap > 300*time.Millisecond || distance > 4.5) {
		return false
	}
	if gap <= 0 {
		return distance <= .35 && appearanceMatches(a.appearance, b.appearance, 7, 1, 34, 16, 14, 4, .77) &&
			appearanceMatches(a.appearance, b.appearance, 5, 5, 37, 19, 11, 9, .88)
	}
	body, whole := .70, .75
	if gap > 16*time.Second {
		body, whole = .78, .82
	}
	if gap > 45*time.Second {
		body, whole = .84, .84
		if distance > 1.65 {
			return false
		}
	}
	ap, bp := a.appearance, b.appearance
	if min(a.reading.Confidence, b.reading.Confidence) < .81 {
		return gap > 2*time.Second && distance <= 1.65 && appearanceMatches(ap, bp, 7, 1, 34, 11, 14, 4, .85) &&
			appearanceMatches(ap, bp, 5, 5, 37, 19, 11, 9, .71)
	}
	return appearanceMatches(ap, bp, 7, 1, 34, 16, 14, 4, body) &&
		appearanceMatches(ap, bp, 5, 5, 37, 19, 11, 9, whole) ||
		gap > 2*time.Second && distance <= 1.75 && appearanceMatches(ap, bp, 7, 1, 34, 11, 14, 4, body+.08) &&
			appearanceMatches(ap, bp, 5, 5, 37, 19, 11, 9, .61)
}

// Compare luminance patterns while allowing a small shift caused by jitter in the crop.
func appearanceMatches(a, b []byte, col, row, w, h, xShift, yShift int, minimum float64) bool {
	area := w * h
	var sumA, squareA int
	for y := range h {
		start := (row+y)*appearanceWidth + col
		for _, px := range a[start : start+w] {
			x := int(px)
			sumA += x
			squareA += x * x
		}
	}
	varianceA := area*squareA - sumA*sumA
	if varianceA < area*area*9 {
		return false
	}
	for dy := 0; dy <= yShift; dy++ {
		for dx := 0; dx <= xShift; dx++ {
			var sumB, squareB, dot int
			for y := range h {
				i, j := (row+y)*appearanceWidth+col, (dy+y)*appearanceWidth+dx
				ar, br := a[i:i+w], b[j:j+w]
				for k, px := range ar {
					p, q := int(px), int(br[k])
					sumB += q
					squareB += q * q
					dot += p * q
				}
			}
			varianceB := area*squareB - sumB*sumB
			covariance := area*dot - sumA*sumB
			if varianceB >= area*area*9 && covariance > 0 && float64(covariance) >= minimum*math.Sqrt(float64(varianceA)*float64(varianceB)) {
				return true
			}
		}
	}
	return false
}

func (t *plateTrack) overlapFrames(last time.Time) []trackFrame {
	if len(t.startFrames) == t.frames {
		return t.startFrames
	}
	// A complete suffix is necessary to count an overlap only once.
	if tail := t.recentFrames; len(tail) > 0 && !tail[0].at.After(last) {
		return tail
	}
	return nil
}

// Two tracks can receive different crops of one plate in the same camera frame.
func sameFrameBoxes(older, newer *plateTrack) bool {
	first, second := older.recentFrames, newer.overlapFrames(older.last)
	count := 0
	i, j := 0, 0
	for i < len(first) && j < len(second) {
		a, b := first[i], second[j]
		switch {
		case a.at.Before(b.at):
			i++
		case a.at.After(b.at):
			j++
		default:
			if stationaryDistance(a.plate.Box, b.plate.Box) <= .34 && intersectionOverUnion(a.plate.Box, b.plate.Box) >= .31 {
				count++
			}
			i++
			j++
		}
	}
	return count >= 3
}

func intersectionOverUnion(a, b [4]int) float64 {
	intersection := max(0, min(a[0]+a[2], b[0]+b[2])-max(a[0], b[0])) * max(0, min(a[1]+a[3], b[1]+b[3])-max(a[1], b[1]))
	union := a[2]*a[3] + b[2]*b[3] - intersection
	return float64(intersection) / float64(max(1, union))
}

func canWaitAtScale(older, newer observation) bool {
	if stationaryDistance(older.box, newer.box) <= 3.4 {
		return true
	}
	if len(older.reading.Number) != 4 || older.reading.Confidence < .87 {
		return false
	}
	a, b := older.box, newer.box
	width := float64(max(a[2], b[2]))
	if min(a[2], b[2]) < 35 || float64(min(a[2], b[2])) < .30*width {
		return false
	}
	ax, ay := center(a)
	bx, by := center(b)
	return math.Hypot(ax-bx, ay-by) < 2.5*width
}
