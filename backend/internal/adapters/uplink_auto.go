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
	Switches   int        `json:"switches"`
	LastAction string     `json:"lastAction,omitempty"`
	LastReason string     `json:"lastReason,omitempty"`
}

const (
	uplinkOffAfter     = 45 * time.Second // without a connection before switching to 4G
	uplinkRetryBackoff = 5 * time.Minute  // after an ON that did not connect
	uplinkHomeRadiusM  = 200.0
	uplinkFixMaxAge    = 4 * time.Minute
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
		if in.WPAState == "COMPLETED" {
			s.LastConnectedAt, s.UnconnectedSince, s.RetryAfter = at(), nil, nil
			if in.Position != nil && now.Sub(in.Position.At) <= uplinkFixMaxAge {
				p := *in.Position
				s.LastConnectedPos = &p
			}
			return UplinkNone, "", s
		}
		if s.UnconnectedSince == nil {
			s.UnconnectedSince = at()
		}
		if !in.Enabled || now.Sub(*s.UnconnectedSince) < uplinkOffAfter {
			return UplinkNone, "", s
		}
		s.AutoOffAt, s.UnconnectedSince = at(), nil
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
	}
	if reason == "" {
		return UplinkNone, "", s
	}
	// If this ON finds no network, the OFF rule fires again and this backoff avoids flapping.
	retry := now.Add(uplinkRetryBackoff)
	s.RetryAfter = &retry
	return UplinkOn, reason, s
}
