package adapters

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/np1-control-center/ncc/internal/model"
	"github.com/np1-control-center/ncc/internal/trips"
)

// HandleJourneyIntervals is an internal broker route. It only reads measured
// sidecars; the non-root collector owns persistence and archive retention.
func HandleJourneyIntervals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		nativeError(w, 405, "METHOD_NOT_ALLOWED", "GETのみ使用できます")
		return
	}
	query := r.URL.Query()
	for key, values := range query {
		if key != "after" || len(values) != 1 {
			nativeError(w, 400, "INVALID_JOURNEY_CURSOR", "走行記録の収集条件が不正です")
			return
		}
	}
	after := time.Time{}
	var e error
	if query.Has("after") {
		after, e = time.Parse(time.RFC3339Nano, query.Get("after"))
		if e != nil {
			nativeError(w, 400, "INVALID_JOURNEY_CURSOR", "走行記録の収集時刻が不正です")
			return
		}
	}
	now := time.Now().UTC()
	if after.After(now) {
		nativeError(w, 400, "INVALID_JOURNEY_CURSOR", "未来の収集時刻は指定できません")
		return
	}
	snapshot, e := tripIndex.Read(r.Context())
	if e != nil {
		nativeError(w, 503, "JOURNEY_SOURCE_UNAVAILABLE", "測位ファイルを取得できません")
		return
	}
	batch, e := trips.MeasuredBatch(snapshot, after, trips.MaximumMeasuredBatch, now)
	if e != nil {
		nativeError(w, 400, "INVALID_JOURNEY_CURSOR", "走行記録の収集条件が不正です")
		return
	}
	value := model.Value("measured sidecar interval page", batch)
	value.RefreshSeconds = 30
	raw, e := json.Marshal(value)
	if e != nil || len(raw) > 1024*1024 {
		nativeError(w, 503, "JOURNEY_PAGE_UNAVAILABLE", "走行記録の収集ページを取得できません")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(raw)
}
