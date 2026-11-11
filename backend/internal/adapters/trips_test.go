package adapters

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/model"
	"github.com/np1-control-center/ncc/internal/trips"
)

type historyFixtureSource struct{}

func (historyFixtureSource) List(context.Context) ([]trips.SourceFile, string, error) {
	return []trips.SourceFile{{Path: "DRIVERECDATA/Video/FILEM_261111_120000_0000.NMEA", Size: 80, Modified: time.Unix(0, 0)}}, "synthetic-card", nil
}
func (historyFixtureSource) Open(string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(trips.Sentence("GPRMC,,V,,,,,,,,,,N"))), nil
}
func TestHandleTripsUnknownNoFixAndBackwardCompatibleDays(t *testing.T) {
	previous := tripIndex
	tripIndex = trips.NewIndex(historyFixtureSource{})
	t.Cleanup(func() { tripIndex = previous })
	for _, query := range []string{"?days=7", "?period=30", "?period=all", "?period=custom&from=2026-11-01&to=2026-11-10"} {
		w := httptest.NewRecorder()
		HandleTrips(w, httptest.NewRequest("GET", "/trips"+query, nil))
		var value model.Observation
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &value) != nil || value.Status != "unknown" {
			t.Fatal(w.Code, w.Body.String())
		}
		data := value.Data.(map[string]any)
		period := data["period"].(map[string]any)
		if period["distanceKm"] != nil || period["drivingSeconds"] != nil || data["noFixSentences"] != float64(1) || len(data["trips"].([]any)) != 0 {
			t.Fatal("missing GPS was fabricated", data)
		}
	}
}
func TestHandleTripsRejectsMethodAndAmbiguousRanges(t *testing.T) {
	for _, path := range []string{"?period=8", "?days=7&period=7", "?period=all&period=7", "?period=custom&from=2026-02-30&to=2026-11-01", "?unknown=1", "?days=7&from=2026-11-01"} {
		w := httptest.NewRecorder()
		HandleTrips(w, httptest.NewRequest("GET", "/trips"+path, nil))
		if w.Code != 400 {
			t.Fatal(path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	HandleTrips(w, httptest.NewRequest("POST", "/trips", nil))
	if w.Code != 405 || w.Header().Get("Allow") != "GET" {
		t.Fatal(w.Code, w.Header())
	}
}
