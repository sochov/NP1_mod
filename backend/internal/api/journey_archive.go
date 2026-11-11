package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/model"
	"github.com/np1-control-center/ncc/internal/trips"
)

const journeyRescanInterval = 10 * time.Minute
const journeyPollInterval = 30 * time.Second
const journeyPagesPerPoll = 4

type journeyArchiveCollector struct {
	archive   *trips.Archive
	clock     func() time.Time
	mu        sync.Mutex
	active    bool
	lastPoll  time.Time
	errorCode string
	scanAfter time.Time
	scanning  bool
	rescanAt  time.Time
}

func newJourneyArchiveCollector(directory string) *journeyArchiveCollector {
	return &journeyArchiveCollector{archive: trips.NewArchive(directory), clock: time.Now, scanning: true}
}

func (c *journeyArchiveCollector) recordPoll(code string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastPoll = c.clock().UTC()
	c.errorCode = code
}

// collect runs independently of page views. Reads, exports and graph requests
// never initialize a retention file or trigger a modem/GNSS permission flow.
func (c *journeyArchiveCollector) collect(ctx context.Context, client *http.Client) {
	c.mu.Lock()
	c.active = true
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.active = false; c.mu.Unlock() }()
	c.poll(ctx, client)
	ticker := time.NewTicker(journeyPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.poll(ctx, client)
		}
	}
}
func (c *journeyArchiveCollector) poll(ctx context.Context, client *http.Client) {
	if ctx.Err() != nil {
		return
	}
	now := c.clock().UTC()
	if !c.scanning && !now.Before(c.rescanAt) {
		c.scanAfter = time.Time{}
		c.scanning = true
	}
	if c.scanAfter.After(now) {
		c.scanAfter = time.Time{}
		c.scanning = true
	}
	for page := 0; page < journeyPagesPerPoll; page++ {
		batch, e := fetchJourneyBatch(ctx, client, c.scanAfter)
		if e != nil {
			if ctx.Err() == nil {
				c.recordPoll("source-unavailable")
			}
			return
		}
		if !batch.After.Equal(c.scanAfter) {
			c.recordPoll("invalid-source-cursor")
			return
		}
		if _, e = c.archive.Ingest(batch, c.clock().UTC()); e != nil {
			c.recordPoll("storage-or-data-unavailable")
			return
		}
		c.recordPoll("")
		if batch.More {
			if !batch.Next.After(c.scanAfter) {
				c.recordPoll("invalid-source-cursor")
				return
			}
			c.scanAfter = batch.Next
			continue
		}
		c.scanAfter = c.archive.State().Watermark
		if c.scanning {
			c.rescanAt = c.clock().UTC().Add(journeyRescanInterval)
		}
		c.scanning = false
		return
	}
}
func fetchJourneyBatch(ctx context.Context, client *http.Client, after time.Time) (trips.MeasuredIntervalBatch, error) {
	target := "http://broker/journey-intervals"
	if !after.IsZero() {
		target += "?after=" + url.QueryEscape(after.UTC().Format(time.RFC3339Nano))
	}
	r, e := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if e != nil {
		return trips.MeasuredIntervalBatch{}, e
	}
	response, e := client.Do(r)
	if e != nil {
		return trips.MeasuredIntervalBatch{}, e
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return trips.MeasuredIntervalBatch{}, errors.New("journey source unavailable")
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	if e != nil || len(raw) > 1024*1024 {
		return trips.MeasuredIntervalBatch{}, errors.New("journey page exceeds limit")
	}
	var value struct {
		Status string                      `json:"status"`
		Data   trips.MeasuredIntervalBatch `json:"data"`
	}
	if json.Unmarshal(raw, &value) != nil || value.Status != "available" || value.Data.Health.IndexedAt.IsZero() {
		return trips.MeasuredIntervalBatch{}, errors.New("invalid journey observation")
	}
	return value.Data, nil
}
func historyOptions(query url.Values) (trips.HistoryOptions, error) {
	for key, values := range query {
		if len(values) != 1 || (key != "period" && key != "days" && key != "from" && key != "to") {
			return trips.HistoryOptions{}, errors.New("invalid history query")
		}
	}
	period := query.Get("period")
	if query.Has("days") {
		if query.Has("period") {
			return trips.HistoryOptions{}, errors.New("ambiguous history period")
		}
		period = query.Get("days")
	}
	return trips.HistoryOptions{Period: period, From: query.Get("from"), To: query.Get("to")}, nil
}
func (c *journeyArchiveCollector) history(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		fail(w, 405, "METHOD_NOT_ALLOWED", "GETのみ使用できます")
		return
	}
	options, e := historyOptions(r.URL.Query())
	if e != nil {
		fail(w, 400, "INVALID_JOURNEY_PERIOD", "走行履歴の期間を確認してください")
		return
	}
	now := c.clock().UTC()
	// Validate query conditions separately from retention availability.
	if _, e = trips.History(trips.Snapshot{}, options, now); e != nil {
		fail(w, 400, "INVALID_JOURNEY_PERIOD", "走行履歴の期間を確認してください")
		return
	}
	report, e := c.archive.History(options, now)
	if e != nil {
		fail(w, 503, "JOURNEY_ARCHIVE_UNAVAILABLE", "保存済みの走行履歴を読み取れません")
		return
	}
	value := model.Value(report.Source, report)
	value.RefreshSeconds = 30
	if report.Status != "available" {
		value.Status = "unknown"
		value.Verified = false
		value.Reason = report.Reason
	}
	jsonOut(w, 200, value)
}
func (c *journeyArchiveCollector) status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		fail(w, 405, "METHOD_NOT_ALLOWED", "GETのみ使用できます")
		return
	}
	c.mu.Lock()
	active, lastPoll, errorCode := c.active, c.lastPoll, c.errorCode
	c.mu.Unlock()
	storage := c.archive.State()
	value := model.Value("continuous measured journey retention", map[string]any{"active": active, "lastPoll": lastPoll, "error": errorCode, "storage": storage})
	if !storage.Available {
		value.Status = "unknown"
		value.Verified = false
		value.Reason = "走行記録の保存先を確認できません"
	}
	jsonOut(w, 200, value)
}
