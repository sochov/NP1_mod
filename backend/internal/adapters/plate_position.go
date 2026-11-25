package adapters

import (
	"time"

	"github.com/np1-control-center/ncc/internal/trips"
)

// near returns the history record closest to at (within tol) and whether the position was
// moving up to that record.
func (t *metaTracker) near(at time.Time, tol time.Duration) (*trips.MetaPosition, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	best := -1
	for i, h := range t.history {
		d := absDuration(h.At.Sub(at))
		if d <= tol && (best < 0 || d < absDuration(t.history[best].At.Sub(at))) {
			best = i
		}
	}
	if best < 0 {
		return nil, false
	}
	p := t.history[best]
	return &p, metaMoving(t.history[:best+1], p.At)
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// metaTrusted reports whether a per-second record is a real position rather than a stale
// last-known one: it is moving, or it is near the newest NMEA fix (within 500 m plus 50 m/s
// since that fix).
func metaTrusted(at time.Time, meta trips.MetaPosition, moving bool, fix *trips.Sample) bool {
	if moving {
		return true
	}
	if fix == nil {
		return false
	}
	limit := 500 + 50*at.Sub(fix.At).Abs().Seconds()
	return distanceM(UplinkPosition{Lat: meta.Lat, Lon: meta.Lon}, UplinkPosition{Lat: fix.Lat, Lon: fix.Lon}) <= limit
}
