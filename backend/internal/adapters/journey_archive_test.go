package adapters

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/np1-control-center/ncc/internal/trips"
)

func TestJourneyIntervalBrokerIsBoundedReadOnlyAndKeepsNoFixHealth(t *testing.T) {
	previous := tripIndex
	tripIndex = trips.NewIndex(historyFixtureSource{})
	t.Cleanup(func() { tripIndex = previous })
	w := httptest.NewRecorder()
	HandleJourneyIntervals(w, httptest.NewRequest("GET", "/journey-intervals", nil))
	if w.Code != 200 || w.Body.Len() > 1024*1024 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Status string                      `json:"status"`
		Data   trips.MeasuredIntervalBatch `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Status != "available" || response.Data.Health.NoFix != 1 || response.Data.Health.Files != 1 || len(response.Data.Intervals) != 0 || response.Data.More {
		t.Fatal("no-fix became a fake interval", w.Body.String())
	}
	for _, path := range []string{"/journey-intervals?after=bad", "/journey-intervals?after=2026-11-01T00:00:00Z&after=2026-11-02T00:00:00Z", "/journey-intervals?limit=999999", "/journey-intervals?after=2099-01-01T00:00:00Z"} {
		w = httptest.NewRecorder()
		HandleJourneyIntervals(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 400 {
			t.Fatal(path, w.Code)
		}
	}
	w = httptest.NewRecorder()
	HandleJourneyIntervals(w, httptest.NewRequest("PUT", "/journey-intervals", nil))
	if w.Code != 405 {
		t.Fatal("mutating broker request accepted")
	}
}
