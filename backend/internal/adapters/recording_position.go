package adapters

import (
	"net/http"
	"time"

	"github.com/np1-control-center/ncc/internal/trips"
)

// recordingPositionTolerance is how far the nearest logged sample may be from a requested
// time (the recorder logs one per second while it has a fix).
const recordingPositionTolerance = 3 * time.Second

// maxPositionQueries bounds one request.
const maxPositionQueries = 100

// HandleRecordingPositions answers "where was the car at these times" from the recorder's
// NMEA sidecars on the SD card. The plate store uses it to fill in records that got no
// position when they were detected; it never reports a position without a logged fix.
func HandleRecordingPositions(w http.ResponseWriter, r *http.Request) {
	at, ok := positionQuery(w, r)
	if !ok {
		return
	}
	snapshot, e := tripIndex.Read(r.Context())
	if e != nil {
		recordingJSON(w, 503, map[string]string{"reason": "位置記録を取得できません"})
		return
	}
	recordingJSON(w, 200, map[string]any{"positions": trips.NearestSamples(snapshot.Samples, at, recordingPositionTolerance)})
}

func positionQuery(w http.ResponseWriter, r *http.Request) ([]time.Time, bool) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		recordingJSON(w, 405, map[string]string{"reason": "GETのみ使用できます"})
		return nil, false
	}
	query := r.URL.Query()
	values := query["at"]
	if len(query) != 1 || len(values) == 0 || len(values) > maxPositionQueries {
		recordingJSON(w, 400, map[string]string{"reason": "時刻を1〜100件指定してください"})
		return nil, false
	}
	at := make([]time.Time, len(values))
	for i, v := range values {
		t, e := time.Parse(time.RFC3339Nano, v)
		if e != nil {
			recordingJSON(w, 400, map[string]string{"reason": "時刻が不正です"})
			return nil, false
		}
		at[i] = t
	}
	return at, true
}
