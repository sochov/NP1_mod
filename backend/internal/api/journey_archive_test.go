package api

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/model"
	"github.com/np1-control-center/ncc/internal/trips"
)

type journeyRoundTrip func(*http.Request) (*http.Response, error)

func (f journeyRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func journeyPoint(at time.Time, speed float64) trips.Sample {
	return trips.Sample{At: at, Lat: 35, Lon: 139, Valid: true, Quality: 1, SpeedMPS: &speed}
}
func journeyClient(t *testing.T, snapshot *trips.Snapshot, now *time.Time, limit int, calls *int) *http.Client {
	t.Helper()
	return &http.Client{Transport: journeyRoundTrip(func(r *http.Request) (*http.Response, error) {
		*calls++
		if r.URL.Path != "/journey-intervals" || r.Method != "GET" {
			t.Fatalf("unexpected internal route %s %s", r.Method, r.URL.Path)
		}
		after := time.Time{}
		if raw := r.URL.Query().Get("after"); raw != "" {
			var e error
			after, e = time.Parse(time.RFC3339Nano, raw)
			if e != nil {
				t.Fatal(e)
			}
		}
		snapshot.IndexedAt = *now
		batch, e := trips.MeasuredBatch(*snapshot, after, limit, *now)
		if e != nil {
			t.Fatal(e)
		}
		raw, e := json.Marshal(model.Value("synthetic-interval-broker", batch))
		if e != nil {
			t.Fatal(e)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}, nil
	})}
}
func retainedHistory(t *testing.T, c *journeyArchiveCollector, path string) trips.HistoryReport {
	t.Helper()
	w := httptest.NewRecorder()
	c.history(w, httptest.NewRequest("GET", path, nil))
	if w.Code != 200 {
		t.Fatalf("history %d %s", w.Code, w.Body.String())
	}
	var result struct {
		Data trips.HistoryReport `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatal("invalid history JSON")
	}
	return result.Data
}

func TestJourneyCollectorPagesBackfillsAndRetainsAfterSourceDeletion(t *testing.T) {
	directory := t.TempDir()
	c := newJourneyArchiveCollector(directory)
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	c.clock = func() time.Time { return now }
	snapshot := trips.Snapshot{Files: 3, NoFix: 42, Samples: []trips.Sample{}}
	for n := 0; n < 6; n++ {
		snapshot.Samples = append(snapshot.Samples, journeyPoint(now.Add(time.Duration(n-10)*time.Second), 1))
	}
	calls := 0
	client := journeyClient(t, &snapshot, &now, 2, &calls)
	// Page views and status inspection do not create the archive.
	before := retainedHistory(t, c, "/trips?period=7")
	if before.Period.HasData || before.Period.DistanceKM != nil {
		t.Fatal("unknown displayed as measured zero")
	}
	status := httptest.NewRecorder()
	c.status(status, httptest.NewRequest("GET", "/trips/archive", nil))
	if _, e := os.Stat(filepath.Join(directory, "journeys")); !os.IsNotExist(e) {
		t.Fatal("GET created persistence")
	}
	c.poll(context.Background(), client)
	report := retainedHistory(t, c, "/trips?period=all")
	if calls != 3 || report.TotalTrips != 1 || math.Abs(*report.Period.DistanceKM-.005) > 1e-8 || report.SourceFiles != 3 || report.NoFixSentences != 42 || report.SourceTruncated {
		t.Fatal(calls, report)
	}
	// Restart includes a full duplicate-safe SD rescan, not an assumption that
	// a saved watermark proves all earlier sidecars had already arrived.
	c = newJourneyArchiveCollector(directory)
	c.clock = func() time.Time { return now }
	c.poll(context.Background(), client)
	report = retainedHistory(t, c, "/trips?period=7")
	if math.Abs(*report.Period.DistanceKM-.005) > 1e-8 {
		t.Fatal("restart doubled retained measurements")
	}
	lateAt := now.Add(-time.Hour)
	snapshot.Samples = append(snapshot.Samples, journeyPoint(lateAt, 2), journeyPoint(lateAt.Add(time.Second), 2))
	now = now.Add(11 * time.Minute)
	c.poll(context.Background(), client)
	report = retainedHistory(t, c, "/trips?period=7")
	if math.Abs(*report.Period.DistanceKM-.007) > 1e-8 || report.TotalTrips != 2 {
		t.Fatal("late sidecar did not fill a disjoint interval", report)
	}
	snapshot.Samples = nil
	snapshot.Files = 0
	now = now.Add(11 * time.Minute)
	c.poll(context.Background(), client)
	report = retainedHistory(t, c, "/trips?period=7")
	if math.Abs(*report.Period.DistanceKM-.007) > 1e-8 || report.SourceFiles != 0 {
		t.Fatal("FIFO source removal erased retention", report)
	}
}

func TestJourneyCollectorBoundsPageBurstAndMarksBackfill(t *testing.T) {
	c := newJourneyArchiveCollector(t.TempDir())
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	c.clock = func() time.Time { return now }
	snapshot := trips.Snapshot{Files: 1}
	for n := 0; n < 10; n++ {
		snapshot.Samples = append(snapshot.Samples, journeyPoint(now.Add(time.Duration(n-20)*time.Second), 1))
	}
	calls := 0
	client := journeyClient(t, &snapshot, &now, 1, &calls)
	c.poll(context.Background(), client)
	if calls != journeyPagesPerPoll || !c.archive.State().Backfilling {
		t.Fatal("page burst was unbounded", calls, c.archive.State())
	}
	if !retainedHistory(t, c, "/trips").SourceTruncated {
		t.Fatal("partial backfill hidden")
	}
	c.poll(context.Background(), client)
	c.poll(context.Background(), client)
	report := retainedHistory(t, c, "/trips")
	if calls != 9 || report.SourceTruncated || math.Abs(*report.Period.DistanceKM-.009) > 1e-8 {
		t.Fatal(calls, report)
	}
}

func TestJourneyCollectorSourceFailureKeepsRetainedDataAndNoFixHealth(t *testing.T) {
	c := newJourneyArchiveCollector(t.TempDir())
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	c.clock = func() time.Time { return now }
	snapshot := trips.Snapshot{Files: 4, NoFix: 900}
	calls := 0
	client := journeyClient(t, &snapshot, &now, 10, &calls)
	c.poll(context.Background(), client)
	report := retainedHistory(t, c, "/trips")
	if report.Period.HasData || report.Period.DistanceKM != nil || report.NoFixSentences != 900 || report.SourceFiles != 4 {
		t.Fatal("empty history hid measured no-fix health", report)
	}
	snapshot.Samples = []trips.Sample{journeyPoint(now.Add(-2*time.Second), 0), journeyPoint(now.Add(-time.Second), 0)}
	c.poll(context.Background(), client)
	broken := &http.Client{Transport: journeyRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	c.poll(context.Background(), broken)
	report = retainedHistory(t, c, "/trips")
	if !report.Period.HasData || report.Period.DistanceKM == nil || *report.Period.DistanceKM != 0 {
		t.Fatal("source loss erased measured stationary zero", report)
	}
	status := httptest.NewRecorder()
	c.status(status, httptest.NewRequest("GET", "/trips/archive", nil))
	if !strings.Contains(status.Body.String(), "source-unavailable") {
		t.Fatal("collection failure hidden", status.Body.String())
	}
}

func TestJourneyHistoryRejectsAmbiguousConditionsWithoutCreatingFiles(t *testing.T) {
	directory := t.TempDir()
	c := newJourneyArchiveCollector(directory)
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	c.clock = func() time.Time { return now }
	for _, path := range []string{"/trips?period=8", "/trips?period=7&days=7", "/trips?period=7&period=30", "/trips?period=custom&from=2026-02-30&to=2026-11-11", "/trips?period=all&from=2026-11-01", "/trips?limit=999999"} {
		w := httptest.NewRecorder()
		c.history(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 400 {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	c.history(w, httptest.NewRequest("POST", "/trips", nil))
	if w.Code != 405 {
		t.Fatal("unexpected method accepted")
	}
	if _, e := os.Stat(filepath.Join(directory, "journeys")); !os.IsNotExist(e) {
		t.Fatal("invalid request wrote state")
	}
}
