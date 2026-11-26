package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

func TestPlateDashboardPositionsOnlyUsesFixNearDetection(t *testing.T) {
	now := time.Date(2026, 11, 16, 11, 30, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"timestamp":%d,"gps":{"fix":true,"age":2,"latitude":35.0,"longitude":139.0,"hdop":1.4}}`, now.Unix())
	}))
	defer server.Close()

	lookup := plateDashboardPositions(server.Client(), server.URL)
	positions, err := lookup(context.Background(), []time.Time{now.Add(-3 * time.Second), now.Add(-5 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if positions[0] == nil || positions[0].Lat != 35.0 || positions[0].Lon != 139.0 || positions[0].AccuracyM == nil || *positions[0].AccuracyM != 7.0 {
		t.Fatalf("near position = %+v", positions[0])
	}
	if positions[1] != nil {
		t.Fatalf("old detection received current position: %+v", positions[1])
	}
}

func TestPlateBackfillFillsRecordsWithoutPosition(t *testing.T) {
	dir := t.TempDir()
	p := newPlateService(dir, nil)
	settings := plate.DefaultSettings()
	settings.MinSightings = 1
	seen := time.Date(2026, 11, 14, 14, 23, 10, 0, time.UTC)
	for i := 0; i < 2; i++ {
		d := plate.Detection{Seq: uint64(i + 1), At: seen.Add(time.Duration(i) * time.Second), Reading: plate.Reading{Region: "相模", Class: "581", Kana: "あ", Number: "1589", Confidence: 0.9}, Box: [4]int{100, 100, 80, 40}}
		if _, e := p.store.Ingest(d, settings, nil); e != nil {
			t.Fatal(e)
		}
	}
	if e := p.store.Flush(); e != nil {
		t.Fatal(e)
	}
	calls := 0
	p.positions = func(_ context.Context, at []time.Time) ([]*plate.Location, error) {
		calls++
		out := make([]*plate.Location, len(at))
		if calls > 1 {
			out[1] = &plate.Location{At: at[1], Lat: 35.5, Lon: 139.4}
		}
		return out, nil
	}
	now := seen.Add(5 * time.Minute)
	if n := p.backfillLocations(context.Background(), now); n != 0 {
		t.Fatalf("filled %d without a logged fix", n)
	}
	if n := p.backfillLocations(context.Background(), now.Add(time.Minute)); n != 0 || calls != 1 {
		t.Fatalf("retried before the retry interval: filled %d, calls %d", n, calls)
	}
	if n := p.backfillLocations(context.Background(), now.Add(11*time.Minute)); n != 1 {
		t.Fatalf("filled %d, want 1", n)
	}
	records, _, e := p.store.List(plate.Query{From: plate.Day(seen), To: plate.Day(seen)})
	if e != nil || len(records) != 1 || records[0].Location == nil || records[0].Location.Lat != 35.5 {
		t.Fatalf("records = %+v, %v", records, e)
	}
}

func TestPlateAttachesLivePositionAndRetriesWithoutWaitingForSD(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(-8 * time.Minute)
	p := newPlateService(t.TempDir(), nil)
	p.clock = func() time.Time { return now }
	p.positions = func(context.Context, []time.Time) ([]*plate.Location, error) {
		t.Fatal("SD lookup entered the live path")
		return nil, nil
	}
	var endpoint string
	client := &http.Client{Transport: journeyRoundTrip(func(r *http.Request) (*http.Response, error) {
		endpoint = r.URL.Path
		at := r.URL.Query()["at"]
		if len(at) != 2 || at[0] != start.Format(time.RFC3339Nano) || at[1] != now.Format(time.RFC3339Nano) {
			t.Errorf("requested times: %v", at)
		}
		body := fmt.Sprintf(`{"positions":[{"at":%q,"lat":35.5,"lon":139.4},null]}`, start.Format(time.RFC3339Nano))
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	p.livePositions = plateBrokerPositions(client, "/gps/positions")
	detections := []plate.Detection{{At: start, Track: &plate.TrackSpan{LastSeen: now}}}
	p.attachLocations(context.Background(), detections)
	if loc := detections[0].Location; loc == nil || !loc.At.Equal(start) || endpoint != "/gps/positions" {
		t.Fatalf("live location: %+v via %s", loc, endpoint)
	}
	settings := plate.DefaultSettings()
	settings.MinSightings = 1
	detection := plate.Detection{At: now, Reading: plate.Reading{Number: "1234", Confidence: .96}, Box: [4]int{10, 10, 90, 45}}
	got, err := p.store.Ingest(detection, settings, nil)
	if err != nil || got.Record.ID == "" {
		t.Fatal(got, err)
	}
	p.rememberLiveLocation(got.Record)
	p.livePositions = func(_ context.Context, at []time.Time) ([]*plate.Location, error) {
		return []*plate.Location{{At: at[0].Add(time.Second), Lat: 35.6, Lon: 139.5}, nil}, nil
	}
	p.retryLiveLocations(context.Background(), now.Add(time.Second))
	records, _, err := p.store.List(plate.Query{From: plate.Day(now), To: plate.Day(now)})
	if err != nil || len(records) != 1 || records[0].Location == nil || records[0].Location.Lat != 35.6 || len(p.livePending) != 0 {
		t.Fatalf("live retry failed: %v, %v", records, err)
	}
}
