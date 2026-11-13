package adapters

import (
	"testing"
	"time"
)

func TestUplinkSwitchesToCellularOnlyAfterAPersistentOutage(t *testing.T) {
	t0 := time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC)
	home := &UplinkPosition{At: t0, Lat: 35.48, Lon: 139.41}
	var s UplinkState
	a, _, s := DecideUplink(UplinkInputs{Now: t0, STAOn: true, WPAState: "COMPLETED", Enabled: true, Position: home}, s)
	if a != UplinkNone || s.LastConnectedPos == nil {
		t.Fatalf("connected: %v %+v", a, s)
	}
	// Leaving home: scanning for two minutes is not yet an outage.
	for _, d := range []time.Duration{10, 60, 120, 170} {
		if a, _, s = DecideUplink(UplinkInputs{Now: t0.Add(d * time.Second), STAOn: true, WPAState: "SCANNING", Enabled: true}, s); a != UplinkNone {
			t.Fatalf("switched too early at %ds", d)
		}
	}
	a, reason, s := DecideUplink(UplinkInputs{Now: t0.Add(200 * time.Second), STAOn: true, WPAState: "SCANNING", Enabled: true}, s)
	if a != UplinkOff || reason != "wifi-unavailable" || s.AutoOffAt == nil {
		t.Fatalf("expected OFF: %v %s %+v", a, reason, s)
	}
	// Driving away: no scan, far from home: stay on 4G.
	far := &UplinkPosition{At: t0.Add(5 * time.Minute), Lat: 35.60, Lon: 139.70}
	if a, _, s = DecideUplink(UplinkInputs{Now: t0.Add(5 * time.Minute), STAOn: false, Enabled: true, Position: far}, s); a != UplinkNone {
		t.Fatal("must stay on 4G far from home")
	}
	// Back home: switch Wi-Fi on.
	back := &UplinkPosition{At: t0.Add(30 * time.Minute), Lat: 35.4801, Lon: 139.4101}
	a, reason, s = DecideUplink(UplinkInputs{Now: t0.Add(30 * time.Minute), STAOn: false, Enabled: true, Position: back}, s)
	if a != UplinkOn || reason != "near-last-connected-place" {
		t.Fatalf("expected ON near home: %v %s", a, reason)
	}
}
func TestUplinkPrefersAScanAndBacksOffAfterAFailedReturn(t *testing.T) {
	t0 := time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC)
	off := t0
	s := UplinkState{AutoOffAt: &off}
	if a, _, _ := DecideUplink(UplinkInputs{Now: t0, STAOn: false, Enabled: true, ScanKnown: true}, s); a != UplinkNone {
		t.Fatal("no saved network seen: stay on 4G")
	}
	a, reason, s := DecideUplink(UplinkInputs{Now: t0.Add(time.Minute), STAOn: false, Enabled: true, ScanKnown: true, SavedSeen: true}, s)
	if a != UplinkOn || reason != "saved-network-seen" {
		t.Fatalf("expected ON: %v %s", a, reason)
	}
	a = UplinkNone
	// The ON found nothing: after the outage delay it goes OFF again, then waits out the backoff.
	now := t0.Add(time.Minute)
	for i := 0; i < 30 && a != UplinkOff; i++ {
		now = now.Add(10 * time.Second)
		a, _, s = DecideUplink(UplinkInputs{Now: now, STAOn: true, WPAState: "SCANNING", Enabled: true}, s)
	}
	if a != UplinkOff {
		t.Fatalf("expected OFF after a failed return, got %v", a)
	}
	if a, _, _ = DecideUplink(UplinkInputs{Now: now.Add(time.Minute), STAOn: false, Enabled: true, ScanKnown: true, SavedSeen: true}, s); a != UplinkNone {
		t.Fatal("must not retry within the backoff")
	}
}
func TestUplinkKeepsWorkingWiFiWhenTheSupplicantReadsStale(t *testing.T) {
	t0 := time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC)
	// A service restart leaves the supplicant state unreadable for a while. The default
	// route still goes through Wi-Fi, so the supervisor must not switch to 4G.
	var s UplinkState
	var a UplinkAction
	for i := 0; i < 40; i++ {
		a, _, s = DecideUplink(UplinkInputs{Now: t0.Add(time.Duration(i) * 10 * time.Second), STAOn: true, WPAState: "DISCONNECTED", Working: true, Enabled: true}, s)
		if a != UplinkNone {
			t.Fatalf("switched off while Wi-Fi carried the default route at %ds", i*10)
		}
	}
	// The same readings without a working uplink do switch off after the outage delay.
	s = UplinkState{}
	a = UplinkNone
	for i := 0; i <= 20 && a != UplinkOff; i++ {
		a, _, s = DecideUplink(UplinkInputs{Now: t0.Add(time.Duration(i) * 10 * time.Second), STAOn: true, WPAState: "DISCONNECTED", Enabled: true}, s)
	}
	if a != UplinkOff {
		t.Fatalf("expected OFF without a working uplink, got %v", a)
	}
}
func TestUplinkProbesAgainWithoutScanOrFix(t *testing.T) {
	t0 := time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC)
	// Parked in a garage: no scan result (the station is off) and no GPS fix, so neither
	// rule can bring Wi-Fi back. A blind retry must happen eventually.
	off := t0
	s := UplinkState{AutoOffAt: &off}
	next := off.Add(uplinkProbeEvery)
	s.NextProbeAt = &next
	if a, _, _ := DecideUplink(UplinkInputs{Now: next.Add(-10 * time.Second), STAOn: false, Enabled: true}, s); a != UplinkNone {
		t.Fatal("probed before the interval")
	}
	a, reason, _ := DecideUplink(UplinkInputs{Now: next, STAOn: false, Enabled: true}, s)
	if a != UplinkOn || reason != "periodic-recheck" {
		t.Fatalf("expected a blind retry: %v %s", a, reason)
	}
	// Switching the owner's choice back to automatic schedules that retry for now,
	// so the station returns without waiting for the interval.
	now := t0
	if a, _, _ := DecideUplink(UplinkInputs{Now: now, STAOn: false, Enabled: true}, UplinkState{AutoOffAt: &off, NextProbeAt: &now}); a != UplinkOn {
		t.Fatalf("expected an immediate retry after choosing automatic, got %v", a)
	}
}
func TestUplinkNeverOverridesTheOwner(t *testing.T) {
	t0 := time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC)
	// The owner turned STA off in the stock settings: no marker, so never turned on.
	if a, _, _ := DecideUplink(UplinkInputs{Now: t0, STAOn: false, Enabled: true, ScanKnown: true, SavedSeen: true}, UplinkState{}); a != UplinkNone {
		t.Fatal("owner's OFF overridden")
	}
	// Automation disabled: never switches off, and restores a mode it had switched off.
	var s UplinkState
	for i := 0; i < 10; i++ {
		var a UplinkAction
		a, _, s = DecideUplink(UplinkInputs{Now: t0.Add(time.Duration(i) * 30 * time.Second), STAOn: true, WPAState: "SCANNING", Enabled: false}, s)
		if a != UplinkNone {
			t.Fatal("disabled automation switched")
		}
	}
	off := t0
	if a, reason, s := DecideUplink(UplinkInputs{Now: t0, STAOn: false, Enabled: false}, UplinkState{AutoOffAt: &off}); a != UplinkOn || reason != "auto-disabled" || s.AutoOffAt != nil {
		t.Fatalf("disabling must restore Wi-Fi: %v %s", a, reason)
	}
	// The owner switched it back on: the marker is dropped.
	if _, _, s := DecideUplink(UplinkInputs{Now: t0, STAOn: true, WPAState: "COMPLETED", Enabled: true}, UplinkState{AutoOffAt: &off}); s.AutoOffAt != nil {
		t.Fatal("marker kept after ON")
	}
}

func TestStationLinkUpIgnoresAStaleDefaultRoute(t *testing.T) {
	if stationLinkUp("down", "0", true) {
		t.Fatal("a disconnected station must not count as working")
	}
	if stationLinkUp("up", "0", true) {
		t.Fatal("carrier 0 must not count as working")
	}
	if !stationLinkUp("up", "1", true) || !stationLinkUp("up", "", false) {
		t.Fatal("a connected station must count as working")
	}
}

func TestUplinkBlindProbeGivesTheRadioBackWhenNothingConnects(t *testing.T) {
	t0 := time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC)
	off := t0
	s := UplinkState{AutoOffAt: &off}
	probe := t0
	s.NextProbeAt = &probe
	a, reason, s := DecideUplink(UplinkInputs{Now: t0, STAOn: false, Enabled: true}, s)
	if a != UplinkOn || reason != "periodic-recheck" || s.ProbeUntil == nil {
		t.Fatalf("expected a bounded probe: %v %s %+v", a, reason, s)
	}
	// Inside the window nothing is decided, even though the station looks unconnected.
	for _, d := range []time.Duration{5, 20, 35} {
		if a, _, s = DecideUplink(UplinkInputs{Now: t0.Add(d * time.Second), STAOn: true, WPAState: "SCANNING", Enabled: true}, s); a != UplinkNone {
			t.Fatalf("gave up inside the probe window at %ds: %v", d, a)
		}
	}
	// After the window the station goes back off at once, not after the outage delay.
	a, reason, s = DecideUplink(UplinkInputs{Now: t0.Add(45 * time.Second), STAOn: true, WPAState: "SCANNING", Enabled: true}, s)
	if a != UplinkOff || reason != "probe-timeout" || s.AutoOffAt == nil || s.ProbeUntil != nil {
		t.Fatalf("expected probe-timeout OFF: %v %s %+v", a, reason, s)
	}
	if s.NextProbeAt == nil || !s.NextProbeAt.Equal(t0.Add(45*time.Second+uplinkProbeEvery)) {
		t.Fatalf("next probe not scheduled: %+v", s.NextProbeAt)
	}
	// A probe that does associate keeps the station on.
	probe2 := t0.Add(4 * time.Minute)
	keep := UplinkState{AutoOffAt: &off, NextProbeAt: &probe2, ProbeUntil: &probe2}
	if a, _, keep = DecideUplink(UplinkInputs{Now: probe2, STAOn: false, Enabled: true}, keep); a != UplinkOn {
		t.Fatalf("expected probe: %v", a)
	}
	if a, _, keep = DecideUplink(UplinkInputs{Now: probe2.Add(10 * time.Second), STAOn: true, Working: true, Enabled: true}, keep); a != UplinkNone || keep.ProbeUntil != nil || keep.AutoOffAt != nil {
		t.Fatalf("connected probe kept the probe markers: %v %+v", a, keep)
	}
}
