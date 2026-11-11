package adapters

import (
	"encoding/json"
	"github.com/np1-control-center/ncc/internal/model"
	"github.com/np1-control-center/ncc/internal/trips"
	"net/http/httptest"
	"testing"
)

func TestRecordingTrackNoFixAndStrictDate(t *testing.T) {
	previous := tripIndex
	tripIndex = trips.NewIndex(historyFixtureSource{})
	t.Cleanup(func() { tripIndex = previous })
	for _, query := range []string{"", "?date=2026-02-30", "?date=2026-11-11&date=2026-11-12", "?date=2026-11-11&path=anything"} {
		w := httptest.NewRecorder()
		HandleRecordingTrack(w, httptest.NewRequest("GET", "/recordings/track"+query, nil))
		if w.Code != 400 {
			t.Fatal(query, w.Code)
		}
	}
	w := httptest.NewRecorder()
	HandleRecordingTrack(w, httptest.NewRequest("GET", "/recordings/track?date=2026-11-11", nil))
	var value model.Observation
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &value) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	data := value.Data.(map[string]any)
	if len(data["points"].([]any)) != 0 || len(data["segments"].([]any)) != 0 || data["reason"] == "" {
		t.Fatal("no fix became coordinates", data)
	}
	w = httptest.NewRecorder()
	HandleRecordingTrack(w, httptest.NewRequest("POST", "/recordings/track?date=2026-11-11", nil))
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
}
