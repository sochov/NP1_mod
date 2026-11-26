package platesensor

import (
	"math"
	"slices"
	"time"
)

type passingTail struct {
	first, middle, last int
}

// An edge pass can move by several plate widths per capture. When three
// distinct singleton tracks confirm the same outward motion and serial,
// extend the confirmed first read with the other two detections.
func completePassingTails(tracks []*plateTrack, at time.Time) []*plateTrack {
	var tails []passingTail
	var uses [trackLimit]int
	for i, end := range tracks {
		last, ok := passingSample(end, .75)
		if !ok || !last.at.Equal(at) || competingPassingTail(tracks, end, at) {
			continue
		}
		number := end.best.reading.Number
		for j, inter := range tracks {
			middle, ok := passingSample(inter, .40)
			if !ok || inter.best.reading.Number != number || middle.plate.Localized == nil ||
				middle.plate.Localized.Confidence < .72 || !passingTailStep(middle, last) {
				continue
			}
			for k, head := range tracks {
				first, ok := passingSample(head, .78)
				if !ok || head.best.reading.Number != number || first.id == last.id ||
					!passingTailStep(first, middle) || !similarPassingTailMotion(first, middle, last) || !head.numberVerified() {
					continue
				}
				tails = append(tails, passingTail{k, j, i})
				uses[k]++
				uses[j]++
				uses[i]++
			}
		}
	}
	for _, tail := range tails {
		if uses[tail.first] != 1 || uses[tail.middle] != 1 || uses[tail.last] != 1 {
			continue
		}
		head := tracks[tail.first]
		for _, index := range [...]int{tail.middle, tail.last} {
			sample := tracks[index].recentFrames[0]
			p := sample.plate
			head.observe(sample.at, sample.finished, EnginePlate{Box: p.Box, Score: p.Score}, sample.id, sample.minConfidence)
			tracks[index] = nil
		}
	}
	if len(tails) == 0 {
		return tracks
	}
	return slices.DeleteFunc(tracks, func(t *plateTrack) bool { return t == nil })
}

func passingSample(t *plateTrack, primary float64) (trackFrame, bool) {
	if t == nil || t.frames != 1 || t.sent != 0 || t.support != 1 || t.ambiguous || t.conflict || len(t.recentFrames) != 1 {
		return trackFrame{}, false
	}
	sample := t.recentFrames[0]
	p := sample.plate
	number := t.best.reading.Number
	if sample.id == "" || len(number) != 3 || p.Score < .50 || p.Box[2] <= 0 || p.Box[3] <= 0 ||
		p.Box[0] < 0 || p.Box[0]+p.Box[2] > FrameWidth || p.Box[1] < 0 || p.Box[1]+p.Box[3] > FrameHeight ||
		!matchesDepartureRead(p, number, primary, .90, .98) || p.LocalizedLeadConflict != "" ||
		p.Localized != nil && p.Localized.Number != number {
		return trackFrame{}, false
	}
	return sample, true
}

func competingPassingTail(tracks []*plateTrack, end *plateTrack, at time.Time) bool {
	for _, other := range tracks {
		if other != nil && other != end && other.last.Equal(at) &&
			(stationaryDistance(other.box, end.box) <= 1.4 || other.best.reading.Number == end.best.reading.Number) {
			return true
		}
	}
	return false
}

func passingTailStep(a, b trackFrame) bool {
	dt := b.at.Sub(a.at)
	if a.id == b.id || dt < 80*time.Millisecond || dt > 180*time.Millisecond {
		return false
	}
	x, y := center(a.plate.Box)
	nx, ny := center(b.plate.Box)
	dx, dy := nx-x, ny-y
	side := x - FrameWidth/2
	w, h := float64(a.plate.Box[2]), float64(a.plate.Box[3])
	nw, nh := float64(b.plate.Box[2]), float64(b.plate.Box[3])
	return math.Abs(side) >= .22*FrameWidth && side*dx > 0 && math.Abs(dx) >= 2.1*max(w, nw) &&
		math.Abs(dx) <= 3.7*max(w, nw) && dy >= .25*max(h, nh) && dy <= .80*max(h, nh) &&
		nw >= .65*w && nw <= 1.35*w && nh >= h && nh <= 1.5*h
}

func similarPassingTailMotion(a, b, c trackFrame) bool {
	x, y := center(a.plate.Box)
	nx, ny := center(b.plate.Box)
	end, ey := center(c.plate.Box)
	ratio := float64(b.at.Sub(a.at)) / float64(c.at.Sub(b.at))
	return ratio >= .65 && ratio <= 1.55 &&
		math.Abs(end-nx)*ratio >= .85*math.Abs(nx-x) && math.Abs(end-nx)*ratio <= 1.65*math.Abs(nx-x) &&
		(ey-ny)*ratio >= .70*(ny-y) && (ey-ny)*ratio <= 1.65*(ny-y)
}
