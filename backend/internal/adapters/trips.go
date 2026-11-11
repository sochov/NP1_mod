package adapters

import (
	"context"
	"errors"
	"github.com/np1-control-center/ncc/internal/model"
	"github.com/np1-control-center/ncc/internal/trips"
	"net/http"
	"time"
)

var tripIndex = trips.NewIndex(trips.SDSource{Root: SDRoot, MountToken: func() (string, error) {
	if SDStatus().Status != "available" {
		return "", errors.New("SDカードが挿入されていません")
	}
	return recordingMountToken(SDRoot)
}})

func TripStats(ctx context.Context) model.Observation {
	snapshot, e := tripIndex.Read(ctx)
	stats := trips.Stats(snapshot.Samples, time.Now())
	value := model.Value("stock-recording-nmea", stats)
	value.RefreshSeconds = 30
	if e != nil {
		value.Status = "unknown"
		value.Verified = false
		value.Reason = e.Error()
		return value
	}
	if stats.Status != "available" {
		value.Status = "unknown"
		value.Verified = false
		value.Reason = stats.Reason
	}
	return value
}
func HandleTripStats(w http.ResponseWriter, r *http.Request) {
	recordingJSON(w, http.StatusOK, TripStats(r.Context()))
}

// LatestGNSS returns only a recent valid fix. Archived sidecars never masquerade
// as the current position and cannot be joined to a current radio observation.
func LatestGNSS(ctx context.Context, at time.Time) (*trips.Sample, error) {
	snapshot, e := tripIndex.Read(ctx)
	if e != nil {
		return nil, e
	}
	var latest *trips.Sample
	for i := range snapshot.Samples {
		s := snapshot.Samples[i]
		if !trips.Usable(s) || s.At.After(at.Add(time.Second)) || at.Sub(s.At) > 5*time.Second {
			continue
		}
		if latest == nil || s.At.After(latest.At) {
			copy := s
			latest = &copy
		}
	}
	return latest, nil
}
func GNSSSummary(ctx context.Context) model.Observation {
	snapshot, e := tripIndex.Read(ctx)
	if e != nil {
		return model.Missing("stock-recording-nmea", e.Error())
	}
	fix, e := LatestGNSS(ctx, time.Now())
	if e != nil {
		return model.Missing("stock-recording-nmea", e.Error())
	}
	if fix == nil {
		value := model.Missing("stock-recording-nmea", "直近5秒以内の有効な測位はありません")
		value.Data = map[string]any{"fix": false, "sourceFiles": snapshot.Files, "noFixSentences": snapshot.NoFix, "indexedAt": snapshot.IndexedAt}
		return value
	}
	return model.Value("stock-recording-nmea", map[string]any{"fix": true, "at": fix.At, "lat": fix.Lat, "lon": fix.Lon, "hdop": fix.HDOP, "accuracyM": fix.AccuracyM, "satellites": fix.Satellites})
}
