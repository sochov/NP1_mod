package platesensor

import (
	"math"
	"sort"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

const (
	trackGap        = 2 * time.Second
	trackUpdate     = 2 * time.Second
	trackMaxMisses  = 3
	trackLimit      = 16
	trackOCRWindow  = 12
	trackEarlyVotes = 3
)

type observation struct {
	reading plate.Reading
	box     [4]int
	frame   string
	weight  float64
}

type plateTrack struct {
	id                   string
	first, last, expires time.Time
	box                  [4]int
	velocity             [2]float64
	frames, misses       int
	sent                 int
	sentAt               time.Time
	ocr                  []observation
	best                 observation
	support              int
	conflict             bool
}

func (t *plateTrack) observe(at, finished time.Time, p EnginePlate, frame string, minConf float64) bool {
	if dt := at.Sub(t.last).Seconds(); !t.last.IsZero() && dt > 0 {
		oldX, oldY := center(t.box)
		x, y := center(p.Box)
		t.velocity = [2]float64{(x - oldX) / dt, (y - oldY) / dt}
	}
	t.last, t.expires, t.box = at, finished.Add(trackGap), p.Box
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
	t.best, t.support, t.conflict = consensus(t.ocr)
	return true
}

func consensus(samples []observation) (observation, int, bool) {
	serials := map[string]float64{}
	for _, s := range samples {
		serials[s.reading.Number] += s.weight
	}
	serial := strongest(serials)
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
	return best, support, len(serials) > 1
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
	if t.support == 0 || t.sent == t.frames {
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
			Conflict: t.conflict, Update: t.sent != 0}}
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
	dx := min(max(t.velocity[0]*dt, -float64(t.box[2])), float64(t.box[2]))
	dy := min(max(t.velocity[1]*dt, -float64(t.box[3])), float64(t.box[3]))
	score := max(proximity(t.box, p.Box, 0, 0), proximity(t.box, p.Box, dx, dy))
	if score == 0 || t.support < 2 || p.Reading.Confidence < .7 {
		return score
	}
	// OCR only disambiguates plausible geometric matches. A wrong number
	// in one frame must still be able to join its track and be outvoted.
	r := plate.Normalize(p.Reading)
	if r.Number != "" && r.Number == t.best.reading.Number {
		score += .25
		for _, field := range [][2]string{{r.Region, t.best.reading.Region}, {r.Class, t.best.reading.Class}, {r.Kana, t.best.reading.Kana}} {
			if field[0] != "" && field[0] == field[1] {
				score += .08
			}
		}
	}
	return score
}

// assignTracks pairs each detection and track at most once. The strongest
// matches are assigned first, independently of the detector's confidence order.
func assignTracks(tracks []*plateTrack, at time.Time, detections []EnginePlate) []int {
	type pair struct {
		track, detection int
		score            float64
	}
	var pairs []pair
	for i, track := range tracks {
		for j, detection := range detections {
			if score := track.match(at, detection); score > 0 {
				pairs = append(pairs, pair{i, j, score})
			}
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].score > pairs[j].score })
	assigned := make([]int, len(detections))
	for i := range assigned {
		assigned[i] = -1
	}
	used := make([]bool, len(tracks))
	for _, p := range pairs {
		if assigned[p.detection] == -1 && !used[p.track] {
			assigned[p.detection], used[p.track] = p.track, true
		}
	}
	return assigned
}
