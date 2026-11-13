package adapters

import (
	"context"
	"errors"
	"github.com/np1-control-center/ncc/internal/model"
	"github.com/np1-control-center/ncc/internal/trips"
	"net/http"
	"time"
)

var tripSource = trips.SDSource{Root: SDRoot, MountToken: func() (string, error) {
	if SDStatus().Status != "available" {
		return "", errors.New("SDカードが挿入されていません")
	}
	return recordingMountToken(SDRoot)
}}
var tripIndex = trips.NewIndex(tripSource)
var liveGNSS = trips.NewLiveIndex(tripSource)

func TripStats(ctx context.Context) model.Observation {
	snapshot, e := tripIndex.Read(ctx)
	stats := trips.Stats(snapshot.Samples, time.Now())
	stats.SourceTruncated = snapshot.Truncated
	stats.SourceFiles = snapshot.Files
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

func HandleTrips(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		recordingJSON(w, http.StatusMethodNotAllowed, map[string]string{"reason": "GETのみ使用できます"})
		return
	}
	query := r.URL.Query()
	for key, values := range query {
		if len(values) != 1 || (key != "period" && key != "days" && key != "from" && key != "to") {
			recordingJSON(w, http.StatusBadRequest, map[string]string{"reason": "走行履歴の条件が不正です"})
			return
		}
	}
	period := query.Get("period")
	if query.Has("days") {
		if query.Has("period") {
			recordingJSON(w, http.StatusBadRequest, map[string]string{"reason": "periodとdaysは同時指定できません"})
			return
		}
		period = query.Get("days")
	}
	options := trips.HistoryOptions{Period: period, From: query.Get("from"), To: query.Get("to")}
	// Reject invalid ranges before reading the SD card.
	if _, e := trips.History(trips.Snapshot{}, options, time.Now()); e != nil {
		recordingJSON(w, http.StatusBadRequest, map[string]string{"reason": "期間は7/30/90/365/all、または有効な開始日・終了日を指定してください"})
		return
	}
	snapshot, readError := tripIndex.Read(r.Context())
	report, e := trips.History(snapshot, options, time.Now())
	if e != nil {
		recordingJSON(w, http.StatusBadRequest, map[string]string{"reason": "測位データの日時範囲が大きすぎるため表示できません"})
		return
	}
	value := model.Value(report.Source, report)
	value.RefreshSeconds = 30
	if readError != nil {
		report.Status, report.Reason = "unknown", readError.Error()
		value.Data = report
	}
	if report.Status != "available" {
		value.Status, value.Reason, value.Verified = "unknown", report.Reason, false
	}
	recordingJSON(w, http.StatusOK, value)
}

// LatestGNSS returns only a recent valid fix. Archived sidecars never masquerade
// as the current position and cannot be joined to a current radio observation.
func LatestGNSS(ctx context.Context, at time.Time) (*trips.Sample, error) {
	return liveGNSS.Latest(ctx, at)
}

// gnssRecentWindow covers one closed recording clip (about three minutes) with margin.
const gnssRecentWindow = 10 * time.Minute

func GNSSSummary(ctx context.Context) model.Observation {
	snapshot, e := tripIndex.Read(ctx)
	if e != nil {
		return model.Missing("stock-recording-nmea", e.Error())
	}
	now := time.Now()
	fix, e := liveGNSS.LatestWithin(ctx, now, gnssRecentWindow)
	if e != nil {
		return model.Missing("stock-recording-nmea", e.Error())
	}
	meta, moving := liveMeta.read(now)
	return combineGNSS(now, meta, moving, fix, map[string]any{"sourceFiles": snapshot.Files, "noFixSentences": snapshot.NoFix, "indexedAt": snapshot.IndexedAt})
}
