package adapters

import (
	"context"
	"net/http"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/model"
	"github.com/np1-control-center/ncc/internal/trips"
)

// Real-time position for the dashboard.
//
// The stock image recognition service publishes its latest position every second, and the
// recorder's NMEA sidecars (written when a clip closes) say whether the receiver had a fix,
// with how many satellites and what HDOP. The dashboard shows the per-second position as the
// current position only when it is backed by a recent NMEA fix or the position is visibly
// moving; satellites and accuracy always come from NMEA and carry their own time.

var imageRecogMetaPath = "/tmp/application/imagerecog/metaData.bin"

const (
	metaMaxAge            = 15 * time.Second // the record is rewritten every second
	metaMoveWindow        = 90 * time.Second
	metaMoveMetres        = 25.0
	metaHistorySize       = 30 * 60
	livePositionTolerance = 2 * time.Second
)

type metaTracker struct {
	mu      sync.Mutex
	history []trips.MetaPosition
}

var liveMeta metaTracker
var liveQuality struct {
	sync.RWMutex
	fix *trips.Sample
}

// TrackLivePosition runs once in the broker, independently of camera work and SD reads.
func TrackLivePosition(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for ctx.Err() == nil {
			fix, _ := liveGNSS.LatestWithin(ctx, time.Now(), gnssRecentWindow)
			liveQuality.Lock()
			liveQuality.fix = fix
			liveQuality.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		liveMeta.read(time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func recentQuality(at time.Time) *trips.Sample {
	liveQuality.RLock()
	defer liveQuality.RUnlock()
	fix := liveQuality.fix
	if fix != nil && fix.At.Sub(at).Abs() <= gnssRecentWindow {
		return fix
	}
	return nil
}

func LiveGNSS() model.Observation {
	now := time.Now()
	meta, moving := liveMeta.near(now, 5*time.Second)
	return combineGNSS(now, meta, moving, recentQuality(now), nil)
}

func HandleLivePositions(w http.ResponseWriter, r *http.Request) {
	at, ok := positionQuery(w, r)
	if !ok {
		return
	}
	positions := make([]*trips.Sample, len(at))
	for i, when := range at {
		meta, moving := liveMeta.near(when, livePositionTolerance)
		if meta != nil && metaTrusted(meta.At, *meta, moving, recentQuality(meta.At)) {
			positions[i] = &trips.Sample{At: meta.At, Lat: meta.Lat, Lon: meta.Lon, Source: "stock-image-recognition", Valid: true, Quality: 1}
		}
	}
	recordingJSON(w, http.StatusOK, map[string]any{"positions": positions})
}

func (t *metaTracker) read(now time.Time) (*trips.MetaPosition, bool) {
	info, e := os.Stat(imageRecogMetaPath)
	if e != nil || info.Size() != trips.ImageRecogMetaSize || now.Sub(info.ModTime()) > metaMaxAge {
		return nil, false
	}
	b, e := os.ReadFile(imageRecogMetaPath)
	if e != nil {
		return nil, false
	}
	p, e := trips.ParseImageRecogMeta(b)
	if e != nil {
		return nil, false
	}
	// The record carries its own clock; a record whose time disagrees with ours is not live.
	if d := now.Sub(p.At); d > metaMaxAge || d < -metaMaxAge {
		return nil, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if n := len(t.history); n != 0 && p.At.Before(t.history[n-1].At) {
		t.history = nil
	}
	if n := len(t.history); n == 0 || !t.history[n-1].At.Equal(p.At) {
		t.history = append(t.history, p)
		first, _ := slices.BinarySearchFunc(t.history, now.Add(-metaHistorySize*time.Second), func(p trips.MetaPosition, threshold time.Time) int {
			return p.At.Compare(threshold)
		})
		t.history = t.history[max(first, len(t.history)-metaHistorySize):]
	}
	return &p, metaMoving(t.history, p.At)
}

// metaMoving reports whether the position moved noticeably within the recent window, which
// a stale last-known position cannot do.
func metaMoving(history []trips.MetaPosition, at time.Time) bool {
	if len(history) < 2 {
		return false
	}
	last := history[len(history)-1]
	for i := len(history) - 2; i >= 0; i-- {
		h := history[i]
		if at.Sub(h.At) > metaMoveWindow {
			break
		}
		if distanceM(UplinkPosition{Lat: h.Lat, Lon: h.Lon}, UplinkPosition{Lat: last.Lat, Lon: last.Lon}) >= metaMoveMetres {
			return true
		}
	}
	return false
}

// combineGNSS builds the dashboard position from the per-second record (may be nil), whether
// it is moving, and the newest NMEA fix inside the recent window (may be nil).
func combineGNSS(now time.Time, meta *trips.MetaPosition, moving bool, fix *trips.Sample, extra map[string]any) model.Observation {
	quality := func(data map[string]any) {
		if fix == nil {
			return
		}
		data["satellites"], data["hdop"], data["accuracyM"] = fix.Satellites, fix.HDOP, fix.AccuracyM
		data["qualityAt"], data["qualityAgeSeconds"] = fix.At, int(now.Sub(fix.At).Seconds())
	}
	if meta != nil && fix != nil && !moving && !metaTrusted(now, *meta, moving, fix) {
		// A stationary record far from the newest fix (farther than 50 m/s since then) is
		// not trusted as the current position.
		meta = nil
	}
	if meta != nil && (fix != nil || moving) {
		data := map[string]any{"fix": true, "live": true, "at": meta.At, "lat": meta.Lat, "lon": meta.Lon, "ageSeconds": int(now.Sub(meta.At).Seconds()), "positionSource": "stock-image-recognition"}
		quality(data)
		return model.Value("stock-image-recognition+nmea", data)
	}
	if fix != nil {
		// ageSeconds tells the UI how old the position is; it is never presented as live when old.
		data := map[string]any{"fix": true, "live": false, "at": fix.At, "lat": fix.Lat, "lon": fix.Lon, "ageSeconds": int(now.Sub(fix.At).Seconds()), "positionSource": "stock-recording-nmea"}
		quality(data)
		return model.Value("stock-recording-nmea", data)
	}
	value := model.Missing("stock-recording-nmea", "直近10分以内の有効な測位はありません")
	data := map[string]any{"fix": false}
	for k, v := range extra {
		data[k] = v
	}
	if meta != nil {
		// Shown only as the last known position, never as the current one.
		data["lastKnown"] = map[string]any{"at": meta.At, "lat": meta.Lat, "lon": meta.Lon}
	}
	value.Data = data
	return value
}
