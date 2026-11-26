package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
	"github.com/np1-control-center/ncc/internal/trips"
)

// Records that got no position at detection time are filled in from the recorder's GNSS log
// once the clip's NMEA sidecar is on the SD card (it is written when the clip closes, about
// every three minutes). Past records are filled in too while their sidecars remain.
const (
	plateBackfillEvery   = 2 * time.Minute
	plateBackfillDelay   = 4 * time.Minute
	plateBackfillDays    = 30
	plateBackfillRecords = 50 // two times per record, within the broker's limit of 100
	plateBackfillRetry   = 10 * time.Minute
	plateBackfillMaxWait = 6 * time.Hour
	plateLiveFixWindow   = 20 * time.Second
)

type plateBackfillState struct {
	next  time.Time
	tries int
}

// plateDashboardPositions reads the DriveRec NMEA fix already exposed by the
// local owner dashboard. A fix is returned only when its timestamp is close to
// the detection, preventing a current position from being attached to an old
// sighting during later backfill passes.
func plateDashboardPositions(client *http.Client, endpoint string) func(context.Context, []time.Time) ([]*plate.Location, error) {
	return func(ctx context.Context, at []time.Time) ([]*plate.Location, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, errors.New("dashboard position lookup failed: " + resp.Status)
		}
		var body struct {
			Timestamp int64 `json:"timestamp"`
			GPS       struct {
				Fix       bool     `json:"fix"`
				Age       *float64 `json:"age"`
				Latitude  *float64 `json:"latitude"`
				Longitude *float64 `json:"longitude"`
				HDOP      *float64 `json:"hdop"`
			} `json:"gps"`
		}
		if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
			return nil, err
		}
		if !body.GPS.Fix || body.Timestamp <= 0 || body.GPS.Age == nil || *body.GPS.Age < 0 ||
			body.GPS.Latitude == nil || body.GPS.Longitude == nil ||
			*body.GPS.Latitude < -90 || *body.GPS.Latitude > 90 ||
			*body.GPS.Longitude < -180 || *body.GPS.Longitude > 180 {
			return make([]*plate.Location, len(at)), nil
		}
		fixAt := time.Unix(body.Timestamp, 0).UTC().Add(-time.Duration(*body.GPS.Age * float64(time.Second)))
		var accuracy *float64
		if body.GPS.HDOP != nil && *body.GPS.HDOP > 0 {
			value := *body.GPS.HDOP * 5
			accuracy = &value
		}
		out := make([]*plate.Location, len(at))
		for i, detectionAt := range at {
			difference := detectionAt.Sub(fixAt)
			if difference < 0 {
				difference = -difference
			}
			if difference <= plateLiveFixWindow {
				out[i] = &plate.Location{At: fixAt, Lat: *body.GPS.Latitude, Lon: *body.GPS.Longitude, AccuracyM: accuracy}
			}
		}
		return out, nil
	}
}

// plateBrokerPositions asks the broker for the logged position nearest to each time.
func plateBrokerPositions(client *http.Client, path string) func(context.Context, []time.Time) ([]*plate.Location, error) {
	return func(ctx context.Context, at []time.Time) ([]*plate.Location, error) {
		q := url.Values{}
		for _, t := range at {
			q.Add("at", t.UTC().Format(time.RFC3339Nano))
		}
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, "http://broker"+path+"?"+q.Encode(), nil)
		if e != nil {
			return nil, e
		}
		resp, e := client.Do(req)
		if e != nil {
			return nil, e
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, errors.New("position lookup failed: " + resp.Status)
		}
		var body struct {
			Positions []*trips.Sample `json:"positions"`
		}
		if e = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); e != nil {
			return nil, e
		}
		if len(body.Positions) != len(at) {
			return nil, errors.New("position lookup returned a different count")
		}
		out := make([]*plate.Location, len(at))
		for i, s := range body.Positions {
			if s != nil {
				out[i] = &plate.Location{At: s.At, Lat: s.Lat, Lon: s.Lon, AccuracyM: s.AccuracyM}
			}
		}
		return out, nil
	}
}

type plateTimeRange struct{ first, last time.Time }

// Find the position at the start of each detection interval, or use a live
// position at its end if the receiver had not acquired a fix at the start.
func (p *plateService) matchLiveLocations(ctx context.Context, ranges []plateTimeRange) []*plate.Location {
	out := make([]*plate.Location, len(ranges))
	if p.livePositions == nil || len(ranges) == 0 {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	for start := 0; start < len(ranges) && ctx.Err() == nil; start += plateBackfillRecords {
		end := min(start+plateBackfillRecords, len(ranges))
		at := make([]time.Time, 0, 2*(end-start))
		for _, interval := range ranges[start:end] {
			at = append(at, interval.first, interval.last)
		}
		found, err := p.livePositions(ctx, at)
		if err != nil || len(found) != len(at) {
			break
		}
		for i := start; i < end; i++ {
			first, last := found[2*(i-start)], found[2*(i-start)+1]
			if first == nil {
				first = last
			}
			out[i] = first
		}
	}
	return out
}

const plateLiveRetryWindow = 20 * time.Second

func (p *plateService) rememberLiveLocation(record plate.Record) {
	if p.livePositions == nil || record.ID == "" {
		return
	}
	if record.Location != nil {
		delete(p.livePending, record.ID)
		return
	}
	if p.clock().Sub(record.LastSeen) > plateLiveRetryWindow {
		return
	}
	if p.livePending == nil {
		p.livePending = make(map[string]plateTimeRange)
	}
	p.livePending[record.ID] = plateTimeRange{record.FirstSeen, record.LastSeen}
}

func (p *plateService) retryLiveLocations(ctx context.Context, now time.Time) {
	var ids []string
	var ranges []plateTimeRange
	for id, interval := range p.livePending {
		if now.Sub(interval.last) > plateLiveRetryWindow {
			delete(p.livePending, id)
			continue
		}
		ids, ranges = append(ids, id), append(ranges, interval)
	}
	for i, loc := range p.matchLiveLocations(ctx, ranges) {
		if loc != nil && p.store.SetLocation(ids[i], ranges[i].first, *loc) == nil {
			delete(p.livePending, ids[i])
		}
	}
}

func (p *plateService) collectLocationBackfill(ctx context.Context) {
	ticker := time.NewTicker(plateBackfillEvery)
	defer ticker.Stop()
	for ctx.Err() == nil {
		p.backfillLocations(ctx, p.clock())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// backfillLocations fills in positions for records without one and returns how many it
// filled. Each record is looked up at its first and last sighting; a record whose time has no
// logged fix is retried with a growing interval (the SD card may have been away).
func (p *plateService) backfillLocations(ctx context.Context, now time.Time) int {
	if p.positions == nil {
		return 0
	}
	records, _, _ := p.store.List(plate.Query{From: plate.Day(now.AddDate(0, 0, -(plateBackfillDays - 1))), To: plate.Day(now), Limit: plate.MaxListLimit})
	if p.backfill == nil {
		p.backfill = map[string]plateBackfillState{}
	}
	listed := map[string]bool{}
	due := []plate.Record{}
	for _, r := range records {
		if r.Location != nil {
			continue
		}
		listed[r.ID] = true
		if now.Sub(r.FirstSeen) < plateBackfillDelay || len(due) == plateBackfillRecords {
			continue
		}
		if st, ok := p.backfill[r.ID]; ok && now.Before(st.next) {
			continue
		}
		due = append(due, r)
	}
	for id := range p.backfill {
		if !listed[id] {
			delete(p.backfill, id)
		}
	}
	if len(due) == 0 {
		return 0
	}
	at := make([]time.Time, 0, 2*len(due))
	for _, r := range due {
		at = append(at, r.FirstSeen, r.LastSeen)
	}
	found, e := p.positions(ctx, at)
	if e != nil {
		return 0 // broker or SD card unavailable: the next round tries again
	}
	filled := 0
	for i, r := range due {
		loc := found[2*i]
		if loc == nil {
			loc = found[2*i+1]
		}
		if loc == nil {
			st := p.backfill[r.ID]
			st.tries++
			wait := plateBackfillRetry << min(st.tries-1, 6)
			st.next = now.Add(min(wait, plateBackfillMaxWait))
			p.backfill[r.ID] = st
			continue
		}
		if p.store.SetLocation(r.ID, r.FirstSeen, *loc) == nil {
			filled++
			delete(p.backfill, r.ID)
		}
	}
	return filled
}
