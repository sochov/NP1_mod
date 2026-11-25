package androidauto

import (
	"context"
	"math"
	"time"

	"github.com/np1-control-center/ncc/internal/androidauto/render"
)

// Advanced PICS (traffic signal) state, read like the Web UI dashboard does
// (frontend/src/services/pics-live.ts): GET /pics every 0.4 s while enabled,
// each signal valid for its validForMs after the request started.

type picsSignal struct {
	Name           string `json:"name"`
	Color          string `json:"color"`
	Seconds        *int   `json:"seconds"`
	DirectionMatch bool   `json:"directionMatch"`
	ValidForMs     *int64 `json:"validForMs"`
	EstimateMs     *int64 `json:"estimateMs"`
	EstimateForMs  *int64 `json:"estimateForMs"`
}

type picsStatus struct {
	Settings struct {
		Enabled     bool `json:"enabled"`
		Directional bool `json:"directional"`
	} `json:"settings"`
	Signals []picsSignal `json:"signals"`
}

type picsSnapshot struct {
	status  picsStatus
	started time.Time
	ok      bool
}

func (s *Source) runPICS(ctx context.Context) {
	for {
		started := time.Now()
		var v picsStatus
		err := s.get(ctx, "/pics", &v)
		s.mu.Lock()
		s.pics = picsSnapshot{status: v, started: started, ok: err == nil}
		s.mu.Unlock()
		wait := 400 * time.Millisecond
		if err != nil || !v.Settings.Enabled {
			wait = 2500 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// signalAt returns the signal to show now, or nil.
func (p picsSnapshot) signalAt(now time.Time) *render.Signal {
	if !p.ok || !p.status.Settings.Enabled {
		return nil
	}
	age := now.Sub(p.started).Milliseconds()
	for _, sig := range p.status.Signals {
		valid := int64(1250)
		if sig.ValidForMs != nil {
			valid = *sig.ValidForMs
		}
		if age >= valid || (p.status.Settings.Directional && !sig.DirectionMatch) {
			continue
		}
		seconds := sig.Seconds
		if sig.EstimateMs != nil {
			var forMs int64
			if sig.EstimateForMs != nil {
				forMs = *sig.EstimateForMs
			}
			seconds = nil
			if age < forMs {
				if est := *sig.EstimateMs - age; est > 0 {
					n := int(math.Ceil(float64(est) / 1000))
					seconds = &n
				}
			}
		}
		out := &render.Signal{Color: sig.Color, Name: sig.Name}
		switch {
		case seconds != nil:
			out.Text, out.Estimate = "青まであと"+itoa(*seconds)+"秒", true
		case sig.Color == "green":
			out.Text = "青"
		case sig.Color == "blink-green":
			out.Text = "青点滅"
		default:
			out.Text = "赤・残り時間不明"
		}
		return out
	}
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
