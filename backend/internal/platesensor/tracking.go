package platesensor

import (
	"math"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

const (
	trackGap        = 2 * time.Second
	trackRejoinGap  = 5 * time.Second
	trackReadGrace  = time.Second
	trackUpdate     = 2 * time.Second
	trackMaxMisses  = 3
	trackLimit      = 16
	trackOCRWindow  = 12
	trackEarlyVotes = 3
	trackFieldLimit = 24
)

type observation struct {
	reading plate.Reading
	box     [4]int
	frame   string
	weight  float64
}

type plateTrack struct {
	id                    string
	first, last, finished time.Time
	firstBox, box         [4]int
	velocity              [2]float64
	frames, misses        int
	sent                  int
	sentAt                time.Time
	ocr                   []observation
	fields                map[string][3]fieldObservation
	best                  observation
	support               int
	conflict              bool
	ambiguous             bool
}

type fieldObservation struct {
	value  string
	weight float64
}

func (t *plateTrack) observe(at, finished time.Time, p EnginePlate, frame string, minConf float64) bool {
	if t.frames == 0 {
		t.firstBox = p.Box
	} else if dt := at.Sub(t.last).Seconds(); dt > 0 {
		oldX, oldY := center(t.box)
		x, y := center(p.Box)
		measured := [2]float64{(x - oldX) / dt, (y - oldY) / dt}
		if t.frames > 1 {
			for i := range measured {
				measured[i] = .55*measured[i] + .45*t.velocity[i]
			}
		}
		t.velocity = measured
	}
	t.last, t.finished, t.box = at, finished, p.Box
	t.frames++
	t.misses = 0
	r := plate.Normalize(p.Reading)
	if !r.Readable() || math.IsNaN(r.Confidence) || r.Confidence < minConf {
		return false
	}
	// Agreement outweighs one exceptionally confident read. Large, clear
	// crops carry more weight and can also supply the representative image.
	weight := (.5 + .5*r.Confidence) * (.85 + .3*min(1, float64(p.Box[2])/100)) * (.9 + .1*p.Score)
	if len(t.ocr) == trackOCRWindow {
		copy(t.ocr, t.ocr[1:])
		t.ocr = t.ocr[:trackOCRWindow-1]
	}
	t.ocr = append(t.ocr, observation{reading: r, box: p.Box, frame: frame, weight: weight})
	var fields [3]fieldObservation
	for i, value := range [...]string{r.Region, r.Class, r.Kana} {
		if value != "" {
			fields[i] = fieldObservation{value: value, weight: weight}
		}
	}
	t.rememberFields(r.Number, fields)
	t.updateConsensus()
	return true
}

func (t *plateTrack) rememberFields(number string, fields [3]fieldObservation) {
	if fields == ([3]fieldObservation{}) {
		return
	}
	old, exists := t.fields[number]
	if !exists && len(t.fields) >= trackFieldLimit {
		return
	}
	if t.fields == nil {
		t.fields = make(map[string][3]fieldObservation)
	}
	for i, field := range fields {
		if field.weight > old[i].weight {
			old[i] = field
		}
	}
	t.fields[number] = old
}

func (t *plateTrack) updateConsensus() {
	t.best, t.support, t.conflict, t.ambiguous = consensus(t.ocr)
	fields := t.fields[t.best.reading.Number]
	for i, value := range [...]*string{&t.best.reading.Region, &t.best.reading.Class, &t.best.reading.Kana} {
		if *value == "" {
			*value = fields[i].value
		}
	}
}

func consensus(samples []observation) (observation, int, bool, bool) {
	serials := map[string]float64{}
	for _, s := range samples {
		serials[s.reading.Number] += s.weight
	}
	serial := strongest(serials)
	ambiguous := false
	for number, score := range serials {
		if number != serial && score*1.20 >= serials[serial] {
			ambiguous = true
		}
	}
	var fields [3]map[string]float64
	for i := range fields {
		fields[i] = map[string]float64{}
	}
	var best observation
	support := 0
	for _, s := range samples {
		if s.reading.Number != serial {
			continue
		}
		support++
		if s.weight >= best.weight {
			best = s
		}
		for i, value := range [...]string{s.reading.Region, s.reading.Class, s.reading.Kana} {
			if value != "" {
				fields[i][value] += s.weight
			}
		}
	}
	best.reading.Region = strongest(fields[0])
	best.reading.Class = strongest(fields[1])
	best.reading.Kana = strongest(fields[2])
	return best, support, len(serials) > 1, ambiguous
}

func strongest(votes map[string]float64) string {
	var best string
	for value, weight := range votes {
		if weight > votes[best] || (weight == votes[best] && value < best) {
			best = value
		}
	}
	return best
}

func (t *plateTrack) detection(final bool) (plate.Detection, bool) {
	if t.support == 0 || t.sent == t.frames || (t.ambiguous && t.sent == 0) {
		return plate.Detection{}, false
	}
	if !final {
		if t.sent == 0 && (t.support < trackEarlyVotes || t.support <= len(t.ocr)/2) {
			return plate.Detection{}, false
		}
		if t.sent != 0 && t.last.Sub(t.sentAt) < trackUpdate {
			return plate.Detection{}, false
		}
	}
	d := plate.Detection{At: t.first, Reading: t.best.reading, Box: t.best.box, Frame: t.best.frame,
		Track: &plate.TrackSpan{ID: t.id, LastSeen: t.last, Frames: t.frames, Support: t.support,
			Conflict: t.conflict, Ambiguous: t.ambiguous, Update: t.sent != 0}}
	t.sent, t.sentAt = t.frames, t.last
	return d, true
}

func center(box [4]int) (float64, float64) {
	return float64(box[0]) + float64(box[2])/2, float64(box[1]) + float64(box[3])/2
}

func proximity(previous, next [4]int, dx, dy float64) float64 {
	w, h := float64(previous[2]), float64(previous[3])
	nw, nh := float64(next[2]), float64(next[3])
	if ratio := nw * nh / (w * h); ratio < .45 || ratio > 2.2 {
		return 0
	}
	x, y := float64(previous[0])+dx, float64(previous[1])+dy
	nx, ny := float64(next[0]), float64(next[1])
	intersection := max(0, min(x+w, nx+nw)-max(x, nx)) * max(0, min(y+h, ny+nh)-max(y, ny))
	iou := intersection / (w*h + nw*nh - intersection)
	distance := math.Hypot((x+w/2-nx-nw/2)/max(w, nw), (y+h/2-ny-nh/2)/max(h, nh))
	if iou < .30 && distance > .48 {
		return 0
	}
	return iou + .35*max(0, 1-distance)
}

func (t *plateTrack) match(at time.Time, p EnginePlate) float64 {
	dt := at.Sub(t.last).Seconds()
	var score float64
	if t.misses < trackMaxMisses && at.Before(t.finished.Add(trackGap)) {
		score = max(proximity(t.box, p.Box, 0, 0), proximity(t.box, p.Box, t.velocity[0]*dt, t.velocity[1]*dt))
	}
	r := p.Reading
	if score > 0 && t.support >= 2 && r.Number == t.best.reading.Number && r.Confidence >= .7 {
		score += .25 + .08*float64(matchingFields(r, t.best.reading))
	}
	return score
}

func matchingFields(a, b plate.Reading) int {
	count := 0
	for _, pair := range [3][2]string{{a.Region, b.Region}, {a.Class, b.Class}, {a.Kana, b.Kana}} {
		if pair[0] != "" && pair[0] == pair[1] {
			count++
		}
	}
	return count
}

// canRejoin combines consecutive, non-overlapping fragments with an exact
// serial reading and plausible image displacement. Geometry alone remains
// sufficient for nearby frames whose OCR is unreadable or temporarily wrong.
func (t *plateTrack) canRejoin(next *plateTrack) bool {
	gap := next.first.Sub(t.last)
	if gap <= 0 || gap > trackRejoinGap || next.sent != 0 || t.ambiguous || next.ambiguous ||
		t.support == 0 || next.support == 0 || t.best.reading.Number != next.best.reading.Number {
		return false
	}
	conflicts := 0
	for _, pair := range [3][2]string{{t.best.reading.Region, next.best.reading.Region},
		{t.best.reading.Class, next.best.reading.Class}, {t.best.reading.Kana, next.best.reading.Kana}} {
		if pair[0] != "" && pair[1] != "" && pair[0] != pair[1] {
			conflicts++
		}
	}
	// Optional OCR fields can disagree on a real plate. Two disagreements
	// are stronger evidence of distinct plates with the same serial number.
	if conflicts >= 2 {
		return false
	}
	w, h := float64(t.box[2]), float64(t.box[3])
	nw, nh := float64(next.firstBox[2]), float64(next.firstBox[3])
	x, y := center(t.box)
	nx, ny := center(next.firstBox)
	dt := min(gap.Seconds(), 1)
	xAllowance := max(2*max(w, nw), 60+420*dt)
	yAllowance := max(max(h, nh), 20+120*dt)
	dx, dy := nx-x, ny-y
	return nw/w >= .35 && nw/w <= 2.85 && nh/h >= .35 && nh/h <= 2.85 &&
		math.Hypot(dx/xAllowance, dy/yAllowance) <= 1 &&
		!(math.Abs(t.velocity[0]) >= 80 && math.Abs(dx) > max(w, nw)+50 && t.velocity[0]*dx < 0)
}

func (t *plateTrack) join(next *plateTrack) {
	if next.frames == 1 {
		x, y := center(t.box)
		nx, ny := center(next.box)
		dt := next.last.Sub(t.last).Seconds()
		t.velocity = [2]float64{(nx - x) / dt, (ny - y) / dt}
	} else {
		t.velocity = next.velocity
	}
	t.last, t.finished, t.box, t.misses = next.last, next.finished, next.box, next.misses
	t.frames += next.frames
	if excess := len(t.ocr) + len(next.ocr) - trackOCRWindow; excess > 0 {
		copy(t.ocr, t.ocr[excess:])
		t.ocr = t.ocr[:len(t.ocr)-excess]
	}
	t.ocr = append(t.ocr, next.ocr...)
	for number, fields := range next.fields {
		t.rememberFields(number, fields)
	}
	t.updateConsensus()
}

func rejoinTracks(tracks []*plateTrack) []*plateTrack {
	for i, next := range tracks {
		if next == nil || next.misses != 0 || next.sent != 0 || next.support == 0 {
			continue
		}
		var prior *plateTrack
		for _, track := range tracks {
			if track != nil && track.canRejoin(next) {
				if prior != nil {
					prior = nil // two possible predecessors: wait for more evidence
					break
				}
				prior = track
			}
		}
		if prior != nil {
			prior.join(next)
			tracks[i] = nil
		}
	}
	remaining := tracks[:0]
	for _, track := range tracks {
		if track != nil {
			remaining = append(remaining, track)
		}
	}
	clear(tracks[len(remaining):])
	return remaining
}

// assignTracks maximizes the total weight of the frame's one-to-one assignments.
// A zero-weight edge, including a padded column, leaves a detection unmatched.
func assignTracks(tracks []*plateTrack, at time.Time, detections []EnginePlate) []int {
	assigned := make([]int, len(detections))
	for i := range assigned {
		assigned[i] = -1
	}
	if len(tracks) == 0 || len(detections) == 0 {
		return assigned
	}
	columns := max(len(tracks), len(detections))
	weights := make([][]float64, len(detections))
	for i, detection := range detections {
		detection.Reading = plate.Normalize(detection.Reading)
		weights[i] = make([]float64, columns)
		for j, track := range tracks {
			weights[i][j] = track.match(at, detection)
		}
	}
	// Hungarian algorithm. Each pass augments the assignment by one detection,
	// reversing previous assignments along the cheapest path when necessary.
	rowPotential := make([]float64, len(detections)+1)
	columnPotential := make([]float64, columns+1)
	matchedRow := make([]int, columns+1)
	previous := make([]int, columns+1)
	distance := make([]float64, columns+1)
	used := make([]bool, columns+1)
	for row := 1; row <= len(detections); row++ {
		matchedRow[0] = row
		clear(used)
		for col := 1; col <= columns; col++ {
			distance[col] = math.Inf(1)
		}
		col := 0
		for {
			used[col] = true
			candidateRow := matchedRow[col]
			delta, next := math.Inf(1), 0
			for j := 1; j <= columns; j++ {
				if used[j] {
					continue
				}
				cost := -weights[candidateRow-1][j-1] - rowPotential[candidateRow] - columnPotential[j]
				if cost < distance[j] {
					distance[j], previous[j] = cost, col
				}
				if distance[j] < delta {
					delta, next = distance[j], j
				}
			}
			for j := 0; j <= columns; j++ {
				if used[j] {
					rowPotential[matchedRow[j]] += delta
					columnPotential[j] -= delta
				} else {
					distance[j] -= delta
				}
			}
			col = next
			if matchedRow[col] == 0 {
				break
			}
		}
		for col != 0 {
			prior := previous[col]
			matchedRow[col] = matchedRow[prior]
			col = prior
		}
	}
	for j := range tracks {
		if row := matchedRow[j+1]; row > 0 && weights[row-1][j] > 0 {
			assigned[row-1] = j
		}
	}
	return assigned
}
