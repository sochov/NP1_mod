package platesensor

import (
	"math"
	"strings"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

// A small crop must earn its own evidence for each optional field. Keep the
// regular-width votes separate: they take precedence and still decide joins.
type fieldEvidence struct {
	values      map[string]int
	strong      map[string]fieldSamples
	first, last time.Time
	lastFrame   string
	overlap     bool
}

type fieldSamples struct {
	count       int
	peak        float64
	first, last time.Time
}

func (f *fieldEvidence) observe(value string, score float64, frame string, at time.Time) {
	if frame == "" || frame == f.lastFrame || at.IsZero() || !f.last.IsZero() && !at.After(f.last) {
		return
	}
	if f.first.IsZero() {
		f.first = at
	}
	f.lastFrame, f.last = frame, at
	addFieldVotes(&f.values, value, 1)
	if math.IsNaN(score) || score < .80 || score > 1 || f.values[value] == 0 {
		return
	}
	f.add(value, fieldSamples{count: 1, peak: score, first: at, last: at})
}

func (f *fieldEvidence) add(value string, other fieldSamples) {
	if f.strong == nil {
		f.strong = make(map[string]fieldSamples)
	}
	v := f.strong[value]
	v.count += other.count
	v.peak = max(v.peak, other.peak)
	if v.first.IsZero() || other.first.Before(v.first) {
		v.first = other.first
	}
	if other.last.After(v.last) {
		v.last = other.last
	}
	f.strong[value] = v
}

func (f *fieldEvidence) merge(other fieldEvidence) {
	if other.first.IsZero() {
		return
	}
	// Overlapping fragments may include two reads from the same exposure.
	f.overlap = f.overlap || other.overlap || !f.first.IsZero() && !other.first.After(f.last) && !f.first.After(other.last)
	if f.first.IsZero() || other.first.Before(f.first) {
		f.first = other.first
	}
	if other.last.After(f.last) {
		f.last, f.lastFrame = other.last, other.lastFrame
	}
	for value, n := range other.values {
		addFieldVotes(&f.values, value, n)
		if v, ok := other.strong[value]; ok && f.values[value] > 0 {
			f.add(value, v)
		}
	}
}

func (f fieldEvidence) consensus(wide map[string]int, minShare float64, classification bool) string {
	if f.overlap || len(f.strong) == 0 {
		return ""
	}
	value := plate.FieldConsensus(f.values, max(.85, minShare))
	v := f.strong[value]
	span := v.last.Sub(v.first)
	brief := v.count >= 3 && v.peak >= .90 && span >= 500*time.Millisecond
	sustained := v.count >= 8 && span >= 2*time.Second && plate.FieldConsensus(f.values, max(.95, minShare)) == value
	if !brief && !sustained {
		return ""
	}
	for alternative := range f.strong {
		if alternative != value && !(classification && sustained && incompleteClassRead(alternative, value)) {
			return ""
		}
	}
	for alternative := range wide {
		if alternative != value {
			return ""
		}
	}
	return value
}

// Every surviving character in a truncated classification must appear in
// order in the complete three-character read; a substituted digit conflicts.
func incompleteClassRead(partial, complete string) bool {
	if len(complete) != 3 || len(partial) == 0 || len(partial) >= 3 {
		return false
	}
	for i := range len(partial) {
		at := strings.IndexByte(complete, partial[i])
		if at < 0 {
			return false
		}
		complete = complete[at+1:]
	}
	return true
}

// One- and two-character classifications are valid. Abstain only when the
// whole-plate reader repeatedly sees a longer prefix match AND a longer
// classification with that prefix has itself been read on multiple frames.
func classLengthDisputed(value string, values, conflicts map[string]int) bool {
	if len(value) == 0 || len(value) >= 3 || conflicts[value] < 2 {
		return false
	}
	for longer, count := range values {
		if count >= 2 && len(longer) > len(value) && strings.HasPrefix(longer, value) {
			return true
		}
	}
	return false
}
