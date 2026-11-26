package pics

import (
	"cmp"
	"slices"
	"time"
)

type Signal struct {
	ID             string    `json:"id"`
	Name           string    `json:"name,omitempty"`
	Slot           int       `json:"slot"`
	Color          Color     `json:"color"`
	Seconds        *int      `json:"seconds"`
	DirectionMatch bool      `json:"directionMatch,omitempty"`
	Step           int       `json:"step,omitempty"`
	FreshnessMS    int64     `json:"freshnessMs"`
	ValidForMS     int64     `json:"validForMs"`
	EstimateMS     int64     `json:"estimateMs"`
	EstimateForMS  int64     `json:"estimateForMs"`
	FreshUntil     time.Time `json:"-"`
}

type Monitor struct {
	intersections map[string]*intersection
	sequence      uint64
	lastBlue      time.Time
	focus         Cue
	seenSignal    bool
	diagnostics   ReceptionDiagnostics
}

type ReceptionDiagnostics struct {
	FramesByType   [3]uint64 `json:"framesByType"`
	GapByTypeMS    [3]int64  `json:"gapByTypeMs"`
	SignalGapMS    int64     `json:"signalGapMs"`
	MaxSignalGapMS int64     `json:"maxSignalGapMs"`
	GapsOverLimit  uint64    `json:"gapsOverTransitionLimit"`
	SkippedChanges uint64    `json:"skippedChanges"`
}

type intersection struct {
	name, source                            string
	lastSeen, lastType2, conflict, sourceAt time.Time
	position                                *Coordinates
	typeAt                                  [3]time.Time
	lamps                                   [6]timing
	blue                                    [6]time.Time
}

// Interrupt clears observations made before a radio outage. Completed timing
// samples are still usable for the same sender after a new stage edge arrives.
func (m *Monitor) Interrupt() {
	m.focus = Cue{}
	for _, crossing := range m.intersections {
		crossing.lastType2 = time.Time{}
		crossing.sourceAt = time.Time{}
		crossing.typeAt = [3]time.Time{}
		crossing.blue = [6]time.Time{}
		for i, t := range crossing.lamps {
			crossing.lamps[i] = timing{samples: t.samples, calibrated: t.calibrated}
		}
	}
}

func (m *Monitor) Receive(source string, payload []byte, now time.Time) []Reception {
	p, ok := parse(payload)
	if !ok {
		return nil
	}
	m.diagnostics.FramesByType[p.kind]++
	if m.intersections == nil {
		m.intersections = make(map[string]*intersection)
	}
	crossing := m.intersections[p.id]
	if crossing == nil {
		if len(m.intersections) >= 100 {
			var oldest string
			for id, candidate := range m.intersections {
				if oldest == "" || candidate.lastSeen.Before(m.intersections[oldest].lastSeen) {
					oldest = id
				}
			}
			delete(m.intersections, oldest)
		}
		crossing = &intersection{}
		m.intersections[p.id] = crossing
	}
	crossing.lastSeen = now
	if prior := crossing.typeAt[p.kind]; !prior.IsZero() && now.After(prior) {
		m.diagnostics.GapByTypeMS[p.kind] = now.Sub(prior).Milliseconds()
	}
	crossing.typeAt[p.kind] = now
	if p.kind == 0 {
		crossing.name = p.name
		return nil
	}
	if p.kind == 1 {
		crossing.position = p.position
		return nil
	}
	if now.Before(crossing.lastType2) {
		return nil
	}
	if crossing.source != "" && crossing.source != source {
		if now.Sub(crossing.sourceAt) <= freshFor {
			for i, lamp := range p.lamps {
				if lamp != crossing.lamps[i].lamp {
					crossing.conflict = now.Add(freshFor)
					crossing.lamps = [6]timing{}
					crossing.blue = [6]time.Time{}
					return nil
				}
			}
			if now.Before(crossing.conflict) {
				return nil
			}
			// Identical reports can keep the display current without extending
			// ownership or bridging a lamp transition across addresses.
			crossing.lastType2 = now
			return nil
		}
		crossing.lamps = [6]timing{}
		crossing.blue = [6]time.Time{}
	}
	if now.Before(crossing.conflict) {
		return nil
	}
	sameSource := crossing.source == source && !crossing.sourceAt.IsZero()
	gap := now.Sub(crossing.sourceAt)
	var prior [6]Lamp
	if sameSource {
		d := &m.diagnostics
		d.SignalGapMS = gap.Milliseconds()
		d.MaxSignalGapMS = max(d.MaxSignalGapMS, d.SignalGapMS)
		if gap > transitionFor {
			d.GapsOverLimit++
		}
	}
	crossing.source, crossing.lastType2, crossing.sourceAt = source, now, now
	m.sequence++
	var records []Reception
	for i, lamp := range p.lamps {
		if lamp.Color != "" {
			m.seenSignal = true
		}
		t := &crossing.lamps[i]
		previous, gap := t.lamp, now.Sub(t.at)
		prior[i] = previous
		if sameSource && now.Sub(t.at) > changeFor && previous.Color != "" && lamp.Color != "" && previous.Color != lamp.Color {
			m.diagnostics.SkippedChanges++
		}
		blue := t.observe(lamp, now, m.sequence)
		if blue {
			crossing.blue[i] = now
		}
		record, keep := t.reception(previous, gap, blue, now)
		if keep {
			record.ID, record.Name, record.Slot, record.Beacon = p.id, crossing.name, i+1, crossing.position
			records = append(records, record)
		}
	}
	if sameSource && gap <= changeFor {
		crossing.observeOtherDirection(prior, p.lamps, now)
	}
	return records
}

const (
	otherBlinkCue = "other-blink"
	soonCue       = "soon"
	hintLifetime  = 3 * time.Second
)

type signalHint struct {
	kind                    string
	targetSlots, otherSlots uint8
	at                      time.Time
}

func (crossing *intersection) observeOtherDirection(prior, lamps [6]Lamp, now time.Time) {
	hint := signalHint{at: now}
	for i, next := range lamps {
		previous := prior[i].Color
		slot := uint8(1 << i)
		var change string
		switch {
		case previous == "" && next.Color == "":
			continue
		case previous == Red && next.Color == Red:
			hint.targetSlots |= slot
			continue
		case previous == Green && next.Color == BlinkGreen:
			change = otherBlinkCue
		case (previous == Green || previous == BlinkGreen) && next.Color == Red:
			change = soonCue
		default:
			// An unrelated active lamp makes the observed groups ambiguous.
			return
		}
		if hint.kind != "" && hint.kind != change {
			return
		}
		hint.kind = change
		hint.otherSlots |= slot
	}
	if hint.otherSlots == 0 {
		return
	}
	for i := range crossing.lamps {
		if hint.targetSlots&(1<<i) != 0 {
			crossing.lamps[i].hint = hint
		}
	}
}

func (crossing *intersection) validHint(slot int, now time.Time) bool {
	t := &crossing.lamps[slot]
	if t.hint.kind == "" || t.lamp.Color != Red || t.seconds(now) > 0 {
		return false
	}
	otherColor := BlinkGreen
	if t.hint.kind == soonCue {
		otherColor = Red
	}
	for i, lamp := range crossing.lamps {
		var color Color
		if t.hint.targetSlots&(1<<i) != 0 {
			color = Red
		}
		if t.hint.otherSlots&(1<<i) != 0 {
			color = otherColor
		}
		if lamp.lamp.Color != color {
			return false
		}
	}
	return true
}

// Signals lists all recently seen lamps. A countdown is shown before color-only
// signals so that the compact dashboard can follow the next estimated change.
func (m *Monitor) Signals(now time.Time) []Signal {
	return m.signals(now, showFor)
}

func (m *Monitor) FreshSignals(now time.Time) []Signal {
	return m.signals(now, freshFor)
}

func (m *Monitor) HasSeenSignal() bool { return m.seenSignal }

func (m *Monitor) Diagnostics() ReceptionDiagnostics { return m.diagnostics }

// VisitUntil retains only the LED's dark waiting state between valid signal frames.
func (m *Monitor) VisitUntil(now time.Time, admit func(string, int) bool) time.Time {
	const idleAfter = 10 * time.Second
	var until time.Time
	for id, crossing := range m.intersections {
		deadline := crossing.lastType2.Add(idleAfter)
		if !deadline.After(now) || !deadline.After(until) {
			continue
		}
		for i, lamp := range crossing.lamps {
			if lamp.lamp.Color != "" && admit(id, i+1) {
				until = deadline
				break
			}
		}
	}
	return until
}

func (m *Monitor) signals(now time.Time, maxAge time.Duration) []Signal {
	result := []Signal{}
	for id, crossing := range m.intersections {
		if now.Before(crossing.conflict) || now.Sub(crossing.lastType2) > maxAge {
			continue
		}
		for i, t := range crossing.lamps {
			if t.lamp.Color == "" {
				continue
			}
			signal := Signal{ID: id, Name: crossing.name, Slot: i + 1, Color: t.lamp.Color, Step: t.lamp.Step,
				FreshnessMS: now.Sub(crossing.lastType2).Milliseconds(), ValidForMS: max(0, crossing.lastType2.Add(maxAge).Sub(now).Milliseconds()),
				FreshUntil: crossing.lastType2.Add(freshFor)}
			if n := t.seconds(now); n > 0 {
				signal.Seconds = &n
				signal.EstimateMS = t.deadline.Sub(now).Milliseconds()
				signal.EstimateForMS = max(0, min(t.deadline.Sub(now), t.at.Add(freshFor).Sub(now)).Milliseconds())
			}
			result = append(result, signal)
		}
	}
	slices.SortFunc(result, func(a, b Signal) int {
		if a.Seconds != nil && b.Seconds != nil {
			if order := cmp.Compare(*a.Seconds, *b.Seconds); order != 0 {
				return order
			}
		} else if a.Seconds != nil {
			return -1
		} else if b.Seconds != nil {
			return 1
		}
		if order := cmp.Compare(a.ID, b.ID); order != 0 {
			return order
		}
		return cmp.Compare(a.Slot, b.Slot)
	})
	return result
}

type Cue struct {
	Kind    string
	Seconds int
	id      string
	slot    int
	phase   uint64
	eventAt time.Time
}

func (m *Monitor) NextCue(now time.Time) (Cue, bool) {
	return m.NextCueFor(now, nil)
}

func (c Cue) SignalKey() (string, int) { return c.id, c.slot + 1 }

func (m *Monitor) NextCueFor(now time.Time, admit func(string, int) bool) (Cue, bool) {
	signals := m.Signals(now)
	if admit != nil {
		signals = slices.DeleteFunc(signals, func(signal Signal) bool { return !admit(signal.ID, signal.Slot) })
	}
	if now.Sub(m.lastBlue) >= time.Second {
		for _, signal := range signals {
			if signal.Color != Green {
				continue
			}
			at := m.intersections[signal.ID].blue[signal.Slot-1]
			if at.After(m.lastBlue) && now.Sub(at) <= freshFor {
				return Cue{Kind: "green", id: signal.ID, slot: signal.Slot - 1, eventAt: at}, true
			}
		}
	}
	if len(signals) == 0 || signals[0].Seconds == nil {
		for _, signal := range signals {
			crossing, slot := m.intersections[signal.ID], signal.Slot-1
			t := &crossing.lamps[slot]
			if t.hint.kind != t.hintAnnounced && now.Sub(t.hint.at) < hintLifetime &&
				now.Sub(crossing.sourceAt) <= freshFor && crossing.validHint(slot, now) {
				return Cue{Kind: t.hint.kind, id: signal.ID, slot: slot, phase: t.phase, eventAt: t.hint.at}, true
			}
		}
		return Cue{}, false
	}
	id, slot := signals[0].ID, signals[0].Slot-1
	// Continue a spoken countdown for the same lamp when estimates overlap.
	// All lamps still participate in the green-change detection above.
	if m.focus.id != "" && m.StillValid(m.focus, now) && (admit == nil || admit(m.focus.id, m.focus.slot+1)) {
		id, slot = m.focus.id, m.focus.slot
	}
	t := &m.intersections[id].lamps[slot]
	n := t.seconds(now)
	cue := Cue{Kind: "remaining", Seconds: n, id: id, slot: slot, phase: t.phase}
	if n <= 5 && n < t.lastNumber {
		cue.Kind = "number"
		return cue, true
	}
	if n > 5 && (!t.announced || n == 10 && !t.ten) {
		return cue, true
	}
	return Cue{}, false
}

func (m *Monitor) Valid(cue Cue, now time.Time) bool {
	if !m.StillValid(cue, now) {
		return false
	}
	switch cue.Kind {
	case "green":
		return now.Sub(cue.eventAt) <= greenValidFor
	case otherBlinkCue, soonCue:
		return now.Sub(cue.eventAt) < hintLifetime && now.Sub(m.intersections[cue.id].sourceAt) <= freshFor
	default:
		return m.intersections[cue.id].lamps[cue.slot].seconds(now) == cue.Seconds
	}
}

func (m *Monitor) CanStart(cue Cue, now time.Time, clipLength time.Duration) bool {
	return m.Valid(cue, now) &&
		(cue.Seconds == 0 || now.Add(clipLength).Before(m.intersections[cue.id].lamps[cue.slot].deadline))
}

func (m *Monitor) StillValid(cue Cue, now time.Time) bool {
	crossing := m.intersections[cue.id]
	if crossing == nil || now.Before(crossing.conflict) {
		return false
	}
	t := &crossing.lamps[cue.slot]
	switch cue.Kind {
	case "green":
		return cue.eventAt == crossing.blue[cue.slot] && t.lamp.Color == Green
	case otherBlinkCue, soonCue:
		return t.phase == cue.phase && t.hint.kind == cue.Kind &&
			t.hint.at == cue.eventAt && crossing.validHint(cue.slot, now)
	}
	return t.phase == cue.phase && t.lamp.Color == Red && t.deadline.After(now)
}

func (m *Monitor) Claim(cue Cue, now time.Time) {
	if cue.Kind == "green" {
		m.lastBlue = now
		return
	}
	crossing := m.intersections[cue.id]
	t := &crossing.lamps[cue.slot]
	if cue.Kind == otherBlinkCue || cue.Kind == soonCue {
		for i := range crossing.lamps {
			if t.hint.targetSlots&(1<<i) != 0 {
				crossing.lamps[i].hintAnnounced = cue.Kind
			}
		}
		return
	}
	m.focus = cue
	if cue.Kind == "number" {
		t.lastNumber = cue.Seconds
	} else {
		t.announced = true
		t.ten = cue.Seconds <= 10
	}
}
