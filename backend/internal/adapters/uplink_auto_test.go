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
	// Leaving home: scanning for 40 s is not yet an outage.
	for _, d := range []time.Duration{10, 20, 40} {
		if a, _, s = DecideUplink(UplinkInputs{Now: t0.Add(d * time.Second), STAOn: true, WPAState: "SCANNING", Enabled: true}, s); a != UplinkNone {
			t.Fatalf("switched too early at %ds", d)
		}
	}
	a, reason, s := DecideUplink(UplinkInputs{Now: t0.Add(60 * time.Second), STAOn: true, WPAState: "SCANNING", Enabled: true}, s)
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
	for i := 0; i < 8 && a != UplinkOff; i++ {
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
