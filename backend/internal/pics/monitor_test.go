package pics

import (
	"encoding/binary"
	"fmt"
	"slices"
	"testing"
	"time"
)

func frame(id byte, lamps ...byte) []byte {
	return append([]byte{0, 0, 2, 255, 0, 0, 0, 0, 0, id}, lamps...)
}

func TestCountdownUsesObservedCyclesAndBlueUsesAdvertisement(t *testing.T) {
	var m Monitor
	now := time.Now()
	red := []byte{0x71, 0x61, 0x51, 0x41, 0x31, 0x21, 0x11, 0x01}
	stepsAt := []int{0, 1, 6, 11, 16, 20, 24, 26}
	var heard []string
	for cycle := 0; cycle <= timingSamples; cycle++ {
		for sec := 0; sec <= 28; sec++ {
			var state byte = 0x83
			if sec < 28 {
				for i, change := range stepsAt {
					if sec >= change {
						state = red[i]
					}
				}
			}
			m.Receive("first", frame(1, state, 0x81, 0x84), now)
			if cycle == 0 && sec < 28 && m.Signals(now)[0].Seconds != nil {
				t.Fatal("first observation produced unverified seconds")
			}
			if cue, ok := m.NextCue(now); ok {
				if !m.Valid(cue, now) {
					t.Fatal("fresh cue rejected", cue)
				}
				if cue.Kind == "number" && cue.Seconds == 1 {
					if !m.CanStart(cue, now, 500*time.Millisecond) || m.CanStart(cue, now.Add(900*time.Millisecond), 500*time.Millisecond) {
						t.Fatal("one-second cue did not check the remaining fraction")
					}
				}
				if cycle == timingSamples {
					heard = append(heard, fmt.Sprintf("%s:%d", cue.Kind, cue.Seconds))
				}
				m.Claim(cue, now)
			} else if sec == 28 {
				t.Fatal("blue change was missed")
			}
			now = now.Add(time.Second)
		}
	}
	want := []string{"remaining:27", "remaining:10", "number:5", "number:4", "number:3", "number:2", "number:1", "green:0"}
	if !slices.Equal(heard, want) {
		t.Fatalf("heard %v, want %v", heard, want)
	}
}

func TestFirstVisitUsesEverySlotAndIgnoresStaleChanges(t *testing.T) {
	var m Monitor
	now := time.Now()
	m.Receive("west", frame(1, 0x84, 0x81, 0x81), now)
	m.Receive("east", frame(2, 0x81), now)
	m.Receive("east", frame(2, 0x83), now.Add(changeFor+time.Second))
	if _, ok := m.NextCue(now.Add(changeFor + time.Second)); ok {
		t.Fatal("a transition inferred across an outage")
	}
	at := now.Add(changeFor + time.Second)
	m.Receive("west", frame(1, 0x84, 0x81, 0x81), at)
	m.Receive("west", frame(1, 0x84, 0x81, 0x83), at.Add(500*time.Millisecond))
	cue, ok := m.NextCue(at.Add(500 * time.Millisecond))
	if !ok || cue.Kind != "green" || cue.slot != 2 {
		t.Fatalf("slot 3 green cue missing: %#v", cue)
	}
	m.Receive("second sender", frame(1, 0x84, 0x81, 0x81), at.Add(600*time.Millisecond))
	if m.StillValid(cue, at.Add(600*time.Millisecond)) {
		t.Fatal("conflicting packet did not invalidate cue")
	}
}

// Road reception on 2026-11-26: one crossing among three transmitters was
// heard every 2.5-9 s, and its red-to-green edge arrived 4.7 s after the
// last red. The LED showed both colors; the change must also be spoken.
func TestSparseRoadReceptionStillAnnouncesGreen(t *testing.T) {
	var m Monitor
	now := time.Now()
	for _, step := range []byte{0x31, 0x21, 0x11} {
		m.Receive("sender", frame(1, step, 0x83), now)
		if cue, ok := m.NextCue(now); ok {
			t.Fatalf("unexpected cue while red: %#v", cue)
		}
		now = now.Add(2500 * time.Millisecond)
	}
	now = now.Add(2240 * time.Millisecond)
	m.Receive("sender", frame(1, 0x83, 0x81), now)
	cue, ok := m.NextCue(now)
	if !ok || cue.Kind != "green" || cue.slot != 0 || !m.CanStart(cue, now, 1500*time.Millisecond) {
		t.Fatalf("green after a 4.7 s road gap was not announced: %#v %v", cue, ok)
	}
	// The cue may wait for a plate announcement that holds the speaker.
	if !m.Valid(cue, now.Add(5*time.Second)) || m.Valid(cue, now.Add(greenValidFor+time.Millisecond)) {
		t.Fatal("green start window is not bounded by greenValidFor")
	}
}

func TestMetadataHasNoSignalFreshness(t *testing.T) {
	var m Monitor
	now := time.Now()
	m.Receive("device", frame(1, 0x83), now)
	name := make([]byte, 24)
	name[9] = 1
	copy(name[10:], "Test crossing")
	m.Receive("device", name, now.Add(time.Second))
	m.Receive("device", frame(1), now.Add(4*time.Second))
	if got := m.Signals(now.Add(6 * time.Second)); len(got) != 0 {
		t.Fatalf("metadata refreshed a stale signal: %#v", got)
	}
}

func TestOtherDirectionChangesWhenSecondsAreUnknown(t *testing.T) {
	var m Monitor
	now := time.Now()
	advance := func(want string, lamps ...byte) {
		t.Helper()
		now = now.Add(500 * time.Millisecond)
		m.Receive("device", frame(1, lamps...), now)
		cue, ok := m.NextCue(now)
		if ok != (want != "") || cue.Kind != want {
			t.Fatalf("got %#v, want %q", cue, want)
		}
		if ok {
			if cue.slot != 0 || !m.Valid(cue, now) {
				t.Fatalf("invalid target cue %#v", cue)
			}
			m.Claim(cue, now)
		}
	}
	advance("", 0x81, 0x81)
	now = now.Add(changeFor)
	advance("", 0x81, 0x83, 0x83)
	advance("", 0x81, 0x82, 0x83) // A third lamp makes the pairing ambiguous.
	advance("", 0x81, 0x83)
	advance(otherBlinkCue, 0x81, 0x82)
	advance("", 0x81, 0x82)
	advance(soonCue, 0x81, 0x81)
	advance("", 0x81, 0x81)
	advance("green", 0x83, 0x81)
	now = now.Add(changeFor)
	advance("", 0x81, 0x83)
	now = now.Add(changeFor)
	advance("", 0x81, 0x81) // An outage is not an observed transition.
}

func TestRoadGapAllowsObservedGreenWithoutExtendingTheDisplay(t *testing.T) {
	var m Monitor
	now := time.Now()
	m.Receive("sender", frame(1, 0x81), now)
	after := now.Add(2467 * time.Millisecond)
	if len(m.Signals(after)) != 0 || !m.VisitUntil(after, func(string, int) bool { return true }).After(after) {
		t.Fatal("signal should expire while the LED visit remains active")
	}
	records := m.Receive("sender", frame(1, 0x83), after)
	cue, ok := m.NextCue(after)
	if !ok || cue.Kind != "green" || !m.CanStart(cue, after, 2200*time.Millisecond) {
		t.Fatal("same-source red to green across a road gap did not qualify")
	}
	if records[0].PhaseSeconds != 0 || !m.StillValid(cue, after.Add(2200*time.Millisecond)) ||
		m.Valid(cue, after.Add(greenValidFor+time.Millisecond)) || len(m.Signals(after.Add(2200*time.Millisecond))) != 0 {
		t.Fatal("the observed cue, timing history and stale display are not independent")
	}
	m.Receive("sender", frame(1, 0x81), after.Add(2200*time.Millisecond))
	if m.StillValid(cue, after.Add(2200*time.Millisecond)) {
		t.Fatal("green cue survived receiving red")
	}
}

func TestInProgressBlinkHintContinuesWhileItsSignalIsFresh(t *testing.T) {
	var m Monitor
	now := time.Now()
	m.Receive("device", frame(1, 0x81, 0x83), now.Add(-500*time.Millisecond))
	m.Receive("device", frame(1, 0x81, 0x82), now)
	cue, ok := m.NextCue(now)
	if !ok || cue.Kind != otherBlinkCue || !m.CanStart(cue, now, 3700*time.Millisecond) {
		t.Fatalf("unexpected cue: %#v", cue)
	}
	for _, gap := range []time.Duration{2300 * time.Millisecond, 4 * time.Second} {
		m.Receive("device", frame(1, 0x81, 0x82), now.Add(gap))
	}
	late := now.Add(4 * time.Second)
	if _, ok := m.NextCue(late); ok || m.Valid(cue, late) || !m.StillValid(cue, late) {
		t.Fatal("a late start and an ongoing announcement did not use separate deadlines")
	}
	m.Receive("device", frame(1, 0x81, 0x81), late)
	if m.StillValid(cue, late) {
		t.Fatal("the blink announcement survived the other lamp changing to red")
	}
}

func TestParallelSignalHeadsShareAnAnnouncement(t *testing.T) {
	var m Monitor
	now := time.Now()
	m.Receive("device", frame(1, 0x81, 0x83, 0x81, 0x83), now)
	for _, next := range []struct {
		color byte
		cue   string
	}{{0x82, otherBlinkCue}, {0x81, soonCue}} {
		now = now.Add(time.Second)
		m.Receive("device", frame(1, 0x81, next.color, 0x81, next.color), now)
		cue, ok := m.NextCueFor(now, func(_ string, slot int) bool { return slot == 3 })
		if !ok || cue.Kind != next.cue || !m.Valid(cue, now) {
			t.Fatalf("directional target cue = %#v, expected %s", cue, next.cue)
		}
		m.Claim(cue, now)
		if cue, ok := m.NextCue(now); ok {
			t.Fatalf("another head announced the same event: %#v", cue)
		}
	}
}

func TestReceptionRecordsMeasureOnlyCompleteObservedCycle(t *testing.T) {
	var m Monitor
	now := time.Now()
	position := make([]byte, 18)
	position[2], position[9] = 1, 1
	binary.BigEndian.PutUint32(position[10:14], 35000000)
	binary.BigEndian.PutUint32(position[14:18], 140000000)
	m.Receive("device", position, now)
	var records []Reception
	for _, lamp := range []byte{0x81, 0x83, 0x83, 0x82, 0x81, 0x81, 0x83, 0x81} {
		records = append(records, m.Receive("device", frame(1, lamp), now)...)
		now = now.Add(time.Second)
	}
	if len(records) != 6 || records[0].Beacon == nil || records[0].Beacon.Lon != 140 || records[1].CycleSeconds != 0 || records[1].PhaseSeconds != 0 || records[4].CycleSeconds != 5 || records[4].PreviousColor != Red || records[4].PhaseSeconds != 2 {
		t.Fatalf("unexpected received cycle evidence: %v", records)
	}
	m.Receive("device", frame(1, 0x81), now.Add(3*time.Second))
	afterGap := m.Receive("device", frame(1, 0x83), now.Add(4*time.Second))
	if len(afterGap) != 1 || afterGap[0].CycleSeconds != 0 || afterGap[0].PhaseSeconds != 0 {
		t.Fatalf("receive gap joined into a cycle: %v", afterGap)
	}
}

func TestInterruptedReceptionCannotBridgeOldLightButKeepsLearnedTiming(t *testing.T) {
	var monitor Monitor
	now := time.Now()
	monitor.Receive("same", frame(1, 0x11), now)
	lamp := &monitor.intersections["00000001"].lamps[0]
	lamp.samples[1] = []time.Duration{9 * time.Second, 9 * time.Second, 9 * time.Second, 9 * time.Second, 9 * time.Second}
	lamp.calibrated = now
	monitor.Interrupt()
	if len(monitor.Signals(now)) != 0 {
		t.Fatal("interruption left a stale light visible")
	}
	monitor.Receive("same", frame(1, 0x83), now.Add(100*time.Millisecond))
	if _, ok := monitor.NextCue(now.Add(100 * time.Millisecond)); ok {
		t.Fatal("red-to-green transition inferred across reconnect")
	}
	monitor.Receive("same", frame(1, 0x11), now.Add(300*time.Millisecond))
	monitor.Receive("same", frame(1, 0x01), now.Add(600*time.Millisecond))
	signals := monitor.Signals(now.Add(600 * time.Millisecond))
	if len(signals) != 1 || signals[0].Seconds == nil || *signals[0].Seconds != 9 {
		t.Fatalf("learned timing not restored on a new stage: %v", signals)
	}
}

func TestIdenticalAlternateUpdatesDisplayWithoutBridgingColorChange(t *testing.T) {
	var m Monitor
	now := time.Now()
	m.Receive("first", frame(1, 0x81), now)
	m.Receive("second", frame(1, 0x81), now.Add(time.Second))
	if len(m.FreshSignals(now.Add(freshFor+50*time.Millisecond))) != 1 {
		t.Fatal("identical alternate report did not keep the display fresh")
	}
	m.Receive("second", frame(1, 0x83), now.Add(freshFor+100*time.Millisecond))
	if _, ok := m.NextCue(now.Add(freshFor + 100*time.Millisecond)); ok {
		t.Fatal("green announcement crossed sender identities")
	}
	m.Receive("second", frame(1, 0x81), now.Add(freshFor+75*time.Millisecond))
	signals := m.Signals(now.Add(freshFor + 150*time.Millisecond))
	if len(signals) != 1 || signals[0].Color != Green {
		t.Fatal("out-of-order report rolled back the current state")
	}
}
