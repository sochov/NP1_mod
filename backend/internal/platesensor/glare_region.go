package platesensor

import "time"

// A pale plate can clip most of the region patch while leaving strokes on
// several rows. Retain an independent tally of consistent, scored OCR reads.
type glareRegionEvidence struct {
	region      string
	lastFrame   string
	first, last time.Time
	count       int
	peak        float64
	conflict    bool
}

func regionPatchHasContrast(view []byte) bool {
	if len(view) != appearanceWidth*appearanceHeight {
		return false
	}
	rows := 0
	patch := fieldPatchAreas[0]
	for y := patch[1]; y < patch[3]; y++ {
		for _, value := range view[y*appearanceWidth+patch[0] : y*appearanceWidth+patch[2]] {
			if value <= 208 {
				rows++
				break
			}
		}
	}
	return rows >= 2
}

func (e *glareRegionEvidence) observe(region string, score float64, frame string, at time.Time, view []byte) {
	if frame == "" || at.IsZero() || frame == e.lastFrame || !at.After(e.last) {
		return
	}
	if e.region == "" {
		e.region = region
	}
	if e.region != region {
		e.conflict = true
	}
	if e.conflict || !(score >= .45 && score <= 1) || !regionPatchHasContrast(view) {
		return
	}
	if e.count == 0 {
		e.first = at
	}
	e.count++
	e.peak = max(e.peak, score)
	e.last, e.lastFrame = at, frame
}

func (e *glareRegionEvidence) merge(other glareRegionEvidence) {
	if e.region == "" {
		*e = other
		return
	}
	e.conflict = e.conflict || other.conflict || (other.region != "" && e.region != other.region)
	if other.count == 0 {
		return
	}
	if e.count == 0 {
		prior := e.conflict
		*e = other
		e.conflict = e.conflict || prior
		return
	}
	e.conflict = e.conflict || (!e.first.After(other.last) && !other.first.After(e.last))
	e.count += other.count
	e.peak = max(e.peak, other.peak)
	if other.first.Before(e.first) {
		e.first = other.first
	}
	if other.last.After(e.last) {
		e.last, e.lastFrame = other.last, other.lastFrame
	}
}

func (e *glareRegionEvidence) consensus(regular map[string]int) string {
	if e.conflict || e.count < 3 || e.peak < .75 || e.last.Sub(e.first) < time.Second {
		return ""
	}
	for alternative := range regular {
		if alternative != e.region {
			return ""
		}
	}
	return e.region
}
