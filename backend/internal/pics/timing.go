package pics

import (
	"slices"
	"time"
)

const (
	freshFor       = 1650 * time.Millisecond
	showFor        = 1850 * time.Millisecond
	transitionFor  = 3 * time.Second
	// changeFor is the longest reception gap across which a lamp change is
	// still announced. On the road the stock receiver reports one
	// advertisement per scan, so with several transmitters in range one
	// crossing is heard only every 2.5-9 s (2026-11-26 drive): a 3 s limit
	// missed observed red-to-green edges while the LED showed both colors.
	changeFor = 10 * time.Second
	// greenValidFor is how long after the observed edge "green" may start,
	// so it can wait for a plate announcement that holds the speaker.
	greenValidFor = 6 * time.Second
	timingLifetime = 20 * time.Minute
	timingSamples  = 5
	maxSeconds     = 99
)

type timing struct {
	lamp                     Lamp
	at, calibrated, deadline time.Time
	edges                    [9]time.Time
	samples                  [9][]time.Duration
	phase                    uint64
	announced, ten           bool
	lastNumber               int
	hint                     signalHint
	hintAnnounced            string
	colorSince, cycleStart   time.Time
	lastRecorded             time.Time
}

func (t *timing) observe(lamp Lamp, at time.Time, phase uint64) (blue bool) {
	previous := t.lamp
	gap := at.Sub(t.at)
	continuous := !t.at.IsZero() && gap <= freshFor
	blue = !t.at.IsZero() && gap <= changeFor && previous.Color == Red && lamp.Color == Green
	t.lamp, t.at = lamp, at
	if !t.calibrated.IsZero() && at.Sub(t.calibrated) > timingLifetime {
		t.samples = [9][]time.Duration{}
	}
	if !continuous || previous.Color != Red || lamp.Color != Red {
		if continuous && blue {
			t.learn(at)
		}
		t.edges = [9]time.Time{}
		t.deadline = time.Time{}
		if lamp.Color == Red && (previous.Color != Red || gap > transitionFor) {
			t.phase, t.announced, t.ten, t.lastNumber = phase, false, false, 6
			t.hint, t.hintAnnounced = signalHint{}, ""
		}
		return blue
	}
	if lamp.Step == previous.Step && lamp.Step > 0 {
		return false
	}
	if lamp.Step == 0 || previous.Step != lamp.Step+1 {
		t.edges = [9]time.Time{}
		t.deadline = time.Time{}
		return false
	}
	t.edges[lamp.Step] = at
	t.deadline = time.Time{}
	if samples := t.samples[lamp.Step]; len(samples) == timingSamples {
		sorted := slices.Clone(samples)
		slices.Sort(sorted)
		if sorted[len(sorted)-1]-sorted[0] <= 2*time.Second {
			t.deadline = at.Add(sorted[len(sorted)/2])
		}
	}
	return false
}

func (t *timing) learn(at time.Time) {
	for step, edge := range t.edges {
		if edge.IsZero() {
			continue
		}
		d := at.Sub(edge)
		if d <= 0 || d > maxSeconds*time.Second {
			continue
		}
		samples := t.samples[step]
		if len(samples) == timingSamples {
			copy(samples, samples[1:])
			samples = samples[:timingSamples-1]
		}
		t.samples[step] = append(samples, d)
		t.calibrated = at
	}
}

func (t *timing) seconds(now time.Time) int {
	if t.lamp.Color != Red || now.Sub(t.at) > freshFor || !t.deadline.After(now) {
		return 0
	}
	return int((t.deadline.Sub(now) + time.Second - 1) / time.Second)
}
