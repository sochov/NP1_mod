package adapters

import (
	"math"
	"time"
)

// Automatic Wi-Fi/4G uplink switching.
//
// The stock Communication service never starts the LTE data call while the stock Wi-Fi
// STA mode is ON, even when no saved network is in range (static analysis and field
// trials, 2026-11-12/13). In a car that leaves the home Wi-Fi this means no internet at
// all. The supervisor therefore turns the stock STA mode OFF (the verified owner request,
// which makes Communication start LTE by itself) after the station has been without a
// connection for a while, and turns it back ON when a saved network is likely in range.
// It only ever turns ON a mode that it turned OFF itself; a mode the owner switched off
// in the stock settings is left alone.

type UplinkInputs struct {
	Now       time.Time
	STAOn     bool   // stock STA mode (setting 40 == 0)
	WPAState  string // supplicant state while the mode is on
	Working   bool   // Wi-Fi actually carries the default route (supplicant reads can lag a restart)
	Enabled   bool   // owner setting
	SavedSeen bool   // a saved SSID was seen by a scan (only meaningful while OFF)
	ScanKnown bool   // a scan result is available while OFF
	Position  *UplinkPosition
}
type UplinkPosition struct {
	At       time.Time
	Lat, Lon float64
}

// UplinkState is persisted so that a mode switched off before a reboot is switched back on.
type UplinkState struct {
	// AutoOffAt is set while the STA mode is off because of this supervisor.
	AutoOffAt *time.Time `json:"autoOffAt,omitempty"`
	// LastConnected is where and when the station last had a connection.
	LastConnectedAt  *time.Time      `json:"lastConnectedAt,omitempty"`
	LastConnectedPos *UplinkPosition `json:"lastConnectedPosition,omitempty"`
	// UnconnectedSince marks the start of the current period without a connection while on.
	UnconnectedSince *time.Time `json:"unconnectedSince,omitempty"`
	// RetryAfter suppresses another ON after an ON that found no network.
	RetryAfter *time.Time `json:"retryAfter,omitempty"`
	// NextProbeAt is when the supervisor may turn Wi-Fi on once more even without a scan
	// result or a position fix, so a switch caused by a bad reading heals by itself.
	NextProbeAt *time.Time `json:"nextProbeAt,omitempty"`
	// ProbeUntil bounds a blind probe: if the station does not associate by then it is
	// switched off again instead of waiting out the full outage delay.
	ProbeUntil *time.Time `json:"probeUntil,omitempty"`
	Switches   int        `json:"switches"`
	LastAction string     `json:"lastAction,omitempty"`
	LastReason string     `json:"lastReason,omitempty"`
}

const (
	// Restarts of the stock Wi-Fi service and stale supplicant reads can leave the state
	// "not completed" while Wi-Fi is fine, so an outage must last long enough to be real.
	uplinkOffAfter     = 3 * time.Minute // without a connection before switching to 4G
	uplinkRetryBackoff = 5 * time.Minute // after an ON that did not connect
	uplinkProbeEvery   = 3 * time.Minute // blind ON attempts while the mode is off
	// A blind probe that does not associate must give the radio back quickly: the
	// stock service stops LTE while the station is on.
	uplinkProbeWindow = 40 * time.Second
	uplinkHomeRadiusM = 200.0
	uplinkFixMaxAge   = 4 * time.Minute
)

type UplinkAction string

const (
	UplinkNone UplinkAction = ""
	UplinkOff  UplinkAction = "off"
	UplinkOn   UplinkAction = "on"
)

func distanceM(a, b UplinkPosition) float64 {
	const r = 6371000.0
	la1, la2 := a.Lat*math.Pi/180, b.Lat*math.Pi/180
	dla, dlo := la2-la1, (b.Lon-a.Lon)*math.Pi/180
	h := math.Sin(dla/2)*math.Sin(dla/2) + math.Cos(la1)*math.Cos(la2)*math.Sin(dlo/2)*math.Sin(dlo/2)
	return 2 * r * math.Asin(math.Min(1, math.Sqrt(h)))
}

// DecideUplink returns the action for one tick and the updated state. It never acts when
// the owner turned the mode off, and never toggles faster than the delays above.
func DecideUplink(in UplinkInputs, s UplinkState) (UplinkAction, string, UplinkState) {
	now := in.Now
	at := func() *time.Time { t := now; return &t }
	if in.STAOn {
		if s.AutoOffAt != nil {
			// The mode is on again (by us, or by the owner): our marker no longer applies.
			s.AutoOffAt = nil
		}
		if in.WPAState == "COMPLETED" || in.Working {
			s.LastConnectedAt, s.UnconnectedSince, s.RetryAfter, s.NextProbeAt = at(), nil, nil, nil
			s.ProbeUntil = nil
			if in.Position != nil && now.Sub(in.Position.At) <= uplinkFixMaxAge {
				p := *in.Position
				s.LastConnectedPos = &p
			}
			return UplinkNone, "", s
		}
		if s.ProbeUntil != nil {
			if now.Before(*s.ProbeUntil) {
				return UplinkNone, "", s // still inside the probe window
			}
			s.ProbeUntil, s.UnconnectedSince = nil, nil
			s.AutoOffAt = at()
			probe := now.Add(uplinkProbeEvery)
			s.NextProbeAt = &probe
			return UplinkOff, "probe-timeout", s
		}
		if s.UnconnectedSince == nil {
			s.UnconnectedSince = at()
		}
		if !in.Enabled || now.Sub(*s.UnconnectedSince) < uplinkOffAfter {
			return UplinkNone, "", s
		}
		s.AutoOffAt, s.UnconnectedSince = at(), nil
		probe := now.Add(uplinkProbeEvery)
		s.NextProbeAt = &probe
		return UplinkOff, "wifi-unavailable", s
	}
	// The mode is off.
	s.UnconnectedSince = nil
	if s.AutoOffAt == nil {
		return UplinkNone, "", s // the owner's choice
	}
	if !in.Enabled {
		s.AutoOffAt = nil
		return UplinkOn, "auto-disabled", s
	}
	if s.RetryAfter != nil && now.Before(*s.RetryAfter) {
		return UplinkNone, "", s
	}
	reason := ""
	switch {
	case in.ScanKnown && in.SavedSeen:
		reason = "saved-network-seen"
	case !in.ScanKnown && in.Position != nil && s.LastConnectedPos != nil && now.Sub(in.Position.At) <= uplinkFixMaxAge && distanceM(*in.Position, *s.LastConnectedPos) <= uplinkHomeRadiusM:
		reason = "near-last-connected-place"
	case s.NextProbeAt == nil:
		// Schedule the blind retry for a state saved before it was tracked.
		probe := now.Add(uplinkProbeEvery)
		s.NextProbeAt = &probe
		return UplinkNone, "", s
	case !now.Before(*s.NextProbeAt):
		// Neither a scan nor a fix is available (the stock station cannot scan while it
		// is off, and there is no GPS fix in a garage), so try the station anyway now
		// and then instead of staying on 4G. The probe window keeps LTE outages short.
		reason = "periodic-recheck"
	}
	if reason == "" {
		return UplinkNone, "", s
	}
	// If this ON finds no network, the OFF rule fires again and this backoff avoids flapping.
	retry := now.Add(uplinkRetryBackoff)
	s.RetryAfter = &retry
	s.NextProbeAt = nil
	if reason == "periodic-recheck" {
		until := now.Add(uplinkProbeWindow)
		s.ProbeUntil = &until
	}
	return UplinkOn, reason, s
}
