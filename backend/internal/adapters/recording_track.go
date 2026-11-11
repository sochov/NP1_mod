package adapters

import (
	"github.com/np1-control-center/ncc/internal/model"
	"github.com/np1-control-center/ncc/internal/trips"
	"net/http"
	"time"
)

func HandleRecordingTrack(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		recordingJSON(w, 405, map[string]string{"reason": "GETのみ使用できます"})
		return
	}
	query := r.URL.Query()
	if len(query) != 1 || len(query["date"]) != 1 {
		recordingJSON(w, 400, map[string]string{"reason": "日付を指定してください"})
		return
	}
	date := query.Get("date")
	if _, err := trips.TrackDate(date); err != nil {
		recordingJSON(w, 400, map[string]string{"reason": "日付が不正です"})
		return
	}
	snapshot, err := tripIndex.Read(r.Context())
	if err != nil {
		recordingJSON(w, 503, map[string]string{"reason": "位置記録を取得できません"})
		return
	}
	track, err := trips.DayTrack(snapshot, date, time.Now())
	if err != nil {
		recordingJSON(w, 400, map[string]string{"reason": "日付が不正です"})
		return
	}
	recordingJSON(w, 200, model.Value(track.Source, track))
}
