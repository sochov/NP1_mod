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
	if box[2] < 35 {
		return nil
	}
	return sampleVehicleView(frame, box)
}

func sampleVehicleView(frame []byte, box [4]int) []byte {
	x, y, w, h := box[0], box[1], box[2], box[3]
	if len(frame) < FrameWidth*FrameHeight || w < 24 || h < 12 || x < 0 || y < 0 || x+w > FrameWidth || y+h > FrameHeight {
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

func canCompareSmallViews(a, b [4]int) bool {
	w, h := max(a[2], b[2]), max(a[3], b[3])
	if min(a[2], b[2]) < 24 || w > 42 || min(a[3], b[3]) < 12 || 3*min(a[2], b[2]) < 2*w {
		return false
	}
	x, y := center(a)
	bx, by := center(b)
	return math.Abs(x-bx) <= .48*float64(w) && math.Abs(y-by) <= .90*float64(h)
}

// Keep a small view only where a published plate could switch to a new track.
func needsContinuityView(tracks []*plateTrack, assigned int, at time.Time, box [4]int) bool {
	if box[2] < 24 || box[2] > 42 || box[3] < 12 {
		return false
	}
	for i, track := range tracks {
		if track.sent == 0 || track.support < 3 || track.ambiguous || !track.hasVerification || len(track.best.reading.Number) < 3 {
			continue
		}
		age := at.Sub(track.last)
		if i == assigned || age >= 0 && age <= 250*time.Millisecond && canCompareSmallViews(track.box, box) {
			return true
		}
	}
	return false
}

func sameSmallVehicleView(a, b EnginePlate) bool {
	first, second := a.continuityView, b.continuityView
	if len(first) != appearanceWidth*appearanceHeight || len(second) != len(first) || !canCompareSmallViews(a.Box, b.Box) {
		return false
	}
	upper := appearanceMatches(first, second, 7, 1, 34, 12, 14, 7, .74) ||
		appearanceMatches(second, first, 7, 1, 34, 12, 14, 7, .74)
	return upper && (appearanceMatches(first, second, 5, 6, 37, 17, 11, 11, .84) ||
		appearanceMatches(second, first, 5, 6, 37, 17, 11, 11, .84))
}

// During a short overlap a reflected or blurred plate can alternate between
// two trajectories. Verify separate vehicle views at both transitions.
func (t *plateTrack) interleavedOverlap(next *plateTrack) []trackFrame {
	gap := next.first.Sub(t.last)
	number := t.best.reading.Number
	if t == next || gap >= 0 || gap < -750*time.Millisecond || len(number) < 3 || number != next.best.reading.Number ||
		t.sent == 0 || next.sent != 0 || t.misses == 0 || t.ambiguous || next.ambiguous || !next.last.After(t.last) ||
		min(t.support, next.support) < 3 || min(t.best.reading.Confidence, next.best.reading.Confidence) < .80 ||
		len(next.openingFrames) < 2 || len(next.openingFrames) != next.frames ||
		len(t.recentFrames) == 0 || t.recentFrames[0].at.After(next.first) ||
		!t.hasVerification || !next.hasVerification || !t.numberVerified() || !next.numberVerified() {
		return nil
	}
	for _, pair := range [3][2]string{
		{t.best.reading.Region, next.best.reading.Region}, {t.best.reading.Class, next.best.reading.Class},
		{t.best.reading.Kana, next.best.reading.Kana},
	} {
		if pair[0] != "" && pair[1] != "" && pair[0] != pair[1] {
			return nil
		}
	}
	for i, first := range next.openingFrames {
		if !first.at.Before(t.last) {
			break
		}
		if !t.matchesNearTransition(first, false) {
			continue
		}
		for _, second := range next.openingFrames[i+1:] {
			if second.at.Sub(first.at) > 350*time.Millisecond || !second.at.Before(t.last) {
				break
			}
			if t.matchesNearTransition(second, true) {
				return next.openingFrames
			}
		}
	}
	return nil
}

func (t *plateTrack) matchesNearTransition(sample trackFrame, after bool) bool {
	matched := false
	for i := len(t.recentFrames) - 1; i >= 0; i-- {
		prior := t.recentFrames[i]
		delta := prior.at.Sub(sample.at)
		if delta < -250*time.Millisecond {
			break
		}
		if delta == 0 {
			// Simultaneous detections use the existing exact-frame test.
			return false
		}
		if !matched && delta <= 250*time.Millisecond && (delta > 0) == after {
			matched = sameSmallVehicleView(prior.plate, sample.plate)
		}
	}
	return matched
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
	kana := t.best.reading.Kana
	serial := t.best.reading.Number
	// For a medium gap, repeated kana and strong serial reads can corroborate
	// two vehicle views whose lighting has changed.
	useKana := gap > 16*time.Second && gap <= 45*time.Second && kana != "" && next.best.reading.Kana == kana &&
		min(t.best.reading.Confidence, next.best.reading.Confidence) >= .89 &&
		min(t.localized[serial], next.localized[serial]) >= .90 &&
		t.stronglyVerifiedContinuation() && next.stronglyVerifiedContinuation() &&
		consistentAppearanceKana(t.appearanceSamples, serial, kana) && consistentAppearanceKana(next.appearanceSamples, serial, kana)
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
			if gap > 16*time.Second && washedOutNearPlate(a.appearance) && washedOutNearPlate(b.appearance) {
				continue
			}
			matches := appearanceMatches(a.appearance, b.appearance, 7, 1, 34, 16, 14, 4, body) &&
				appearanceMatches(a.appearance, b.appearance, 5, 5, 37, 19, 11, 9, whole)
			if !matches && !(useKana && distance <= .60 && a.reading.Kana == kana && b.reading.Kana == kana &&
				appearanceMatches(a.appearance, b.appearance, 7, 1, 34, 16, 14, 4, .53) &&
				appearanceMatches(a.appearance, b.appearance, 5, 5, 37, 19, 11, 9, .81)) {
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

func consistentAppearanceKana(views []observation, number, kana string) bool {
	var last time.Time
	matches := 0
	for _, view := range views {
		if view.reading.Number != number || view.reading.Kana == "" || view.box[2] < 45 {
			continue
		}
		if view.reading.Kana != kana {
			return false
		}
		if view.at.After(last) {
			matches++
			last = view.at
		}
	}
	return matches >= 3
}

func matchingAppearance(a, b observation, gap time.Duration) bool {
	// Similar glare in both crops can hide differences between vehicles.
	if gap > 16*time.Second && washedOutNearPlate(a.appearance) && washedOutNearPlate(b.appearance) {
		return false
	}
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

func washedOutNearPlate(pixels []byte) bool {
	if len(pixels) != appearanceWidth*appearanceHeight {
		return false
	}
	aroundPlate := pixels[13*appearanceWidth:]
	clipped := 0
	for _, luma := range aroundPlate {
		if luma >= 240 {
			clipped++
		}
	}
	return clipped*5 >= len(aroundPlate)
}

// Compare luminance patterns while allowing a small shift caused by jitter in the crop.
// matchFastSingleFrame compares the vehicle around two independently read plates.
// canRejoin checks the direction, displacement and size before calling this method.
func (t *plateTrack) matchFastSingleFrame(next *plateTrack, gap time.Duration, conflicts int) bool {
	if gap > 175*time.Millisecond || conflicts != 0 || t.frames != 1 || next.frames != 1 {
		return false
	}
	number := t.best.reading.Number
	a, b := t.votes[number], next.votes[number]
	if !a.localizedProof || !b.localizedProof || min(a.wideVerification, b.wideVerification) < .98 {
		return false
	}
	body, whole := .80, .80
	switch len(number) {
	case 3:
		body, whole = .90, .86
	case 4:
	default:
		return false
	}
	x, y := t.best.appearance, next.best.appearance
	if len(x) != appearanceWidth*appearanceHeight || len(y) != appearanceWidth*appearanceHeight {
		return false
	}
	if appearanceMatches(x, y, 7, 1, 34, 16, 14, 4, body) &&
		appearanceMatches(x, y, 5, 5, 37, 19, 11, 9, whole) {
		return true
	}
	// For adjacent three-digit reads, require both regions to agree without
	// shifting either crop, while preserving all motion and OCR checks above.
	return len(number) == 3 && gap <= 125*time.Millisecond &&
		alignedAppearanceMatches(x, y, 7, 1, 34, 16, .885) &&
		alignedAppearanceMatches(x, y, 5, 5, 37, 19, .875)
}

// A fast outward pass can split into two single-frame reads. Extend the
// displacement allowance only when an earlier view supports their motion.
func (t *plateTrack) canRejoinFastOutward(next *plateTrack, active []*plateTrack) bool {
	if t == next || next.sent != 0 || t.ambiguous || next.ambiguous || t.frames != 1 || next.frames != 1 {
		return false
	}
	older, newer := t.best.reading, next.best.reading
	number := older.Number
	gap := next.first.Sub(t.last)
	if gap <= 0 || gap > 150*time.Millisecond || len(number) != 3 || number != newer.Number ||
		min(older.Confidence, newer.Confidence) < .92 {
		return false
	}
	for _, pair := range [3][2]string{{older.Region, newer.Region}, {older.Class, newer.Class}, {older.Kana, newer.Kana}} {
		if pair[0] != "" && pair[1] != "" && pair[0] != pair[1] {
			return false
		}
	}
	w, h := float64(t.box[2]), float64(t.box[3])
	nw, nh := float64(next.firstBox[2]), float64(next.firstBox[3])
	x, y := center(t.box)
	nx, ny := center(next.firstBox)
	dx, dy := nx-x, ny-y
	if math.Abs(x-FrameWidth/2) < .25*FrameWidth || (x-FrameWidth/2)*dx <= 0 ||
		math.Abs(dx) < 2.35*max(w, nw) || math.Abs(dx) > 2.9*max(w, nw) ||
		dy < .6*max(h, nh) || dy > 1.15*max(h, nh) || nw/w < .95 || nw/w > 1.25 || nh/h < .95 || nh/h > 1.35 {
		return false
	}
	a, b := t.votes[number], next.votes[number]
	imgA, imgB := t.best.appearance, next.best.appearance
	if !a.independentProof || !b.independentProof || !b.localizedProof ||
		min(a.wideVerification, b.wideVerification) < .99 || min(a.jointConfidence, b.jointConfidence) < .86 ||
		!t.numberVerified() || !next.numberVerified() || len(imgA) != appearanceWidth*appearanceHeight || len(imgB) != len(imgA) ||
		!alignedAppearanceMatches(imgA, imgB, 7, 1, 34, 16, .70) ||
		!alignedAppearanceMatches(imgA, imgB, 5, 5, 37, 19, .79) ||
		!alignedAppearanceMatches(imgA, imgB, 7, 1, 34, 9, .48) {
		return false
	}
	predecessors := 0
	for _, track := range active {
		if track != nil && track != t && track != next && t.followsOutwardPredecessor(track, gap, dx, dy) {
			predecessors++
		}
	}
	return predecessors == 1
}

func (t *plateTrack) followsOutwardPredecessor(track *plateTrack, gap time.Duration, dx, dy float64) bool {
	if track.sent != 0 || track.frames < 3 || len(track.recentFrames) == 0 {
		return false
	}
	frame := track.recentFrames[len(track.recentFrames)-1]
	previousGap := t.first.Sub(frame.at)
	if !frame.at.Equal(track.last) || previousGap < 100*time.Millisecond || previousGap > 175*time.Millisecond {
		return false
	}
	number := t.best.reading.Number
	p := frame.plate
	if p.SequenceNumber != number || p.NumberScores[number] < .96 || p.Verifier == nil ||
		p.Verifier.Number != number || p.Verifier.Confidence < .80 || p.Score < .60 || len(p.appearance) != len(t.best.appearance) {
		return false
	}
	x, y := center(t.box)
	px, py := center(p.Box)
	pace := float64(previousGap) / float64(gap)
	alongX, alongY := dx*pace/(x-px), dy*pace/(y-py)
	scale := float64(t.box[2]) / float64(p.Box[2])
	return alongX >= .9 && alongX <= 1.35 && alongY >= .8 && alongY <= 1.35 && scale >= .95 && scale <= 1.22 &&
		appearanceMatches(p.appearance, t.best.appearance, 7, 1, 34, 11, 14, 4, .79) &&
		appearanceMatches(p.appearance, t.best.appearance, 5, 5, 37, 19, 11, 9, .86)
}

func alignedAppearanceMatches(a, b []byte, col, row, width, height int, minimum float64) bool {
	return appearanceMatches(a, b[row*appearanceWidth+col:], col, row, width, height, 0, 0, minimum)
}

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
