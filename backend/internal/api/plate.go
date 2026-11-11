package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/config"
	"github.com/np1-control-center/ncc/internal/plate"
)

// PlateSensorSocket is served by the separate plate sensor process, which owns
// camera-export reading, inference and GNSS joining.
const PlateSensorSocket = "/run/np1-control-plate/sensor.sock"

// PlateSensorSocketPath allows a PC verification run (plate-replay) to use
// another socket; the device always uses PlateSensorSocket.
func PlateSensorSocketPath() string {
	if p := os.Getenv("NCC_PLATE_SENSOR_SOCKET"); p != "" {
		return p
	}
	return PlateSensorSocket
}

const (
	platePollInterval  = time.Second
	plateStatusEvery   = 10 * time.Second
	platePruneInterval = 10 * time.Minute
	plateBatchLimit    = 2 << 20
	plateFrameLimit    = 4 << 20
	plateSettingsLimit = 64 * 1024
)

// PlateSensorStatus is reported by the sensor; the server never infers it.
type PlateSensorStatus struct {
	State       string     `json:"state"` // running | model-missing | engine-missing | error | unavailable
	Reason      string     `json:"reason,omitempty"`
	Instance    string     `json:"instance,omitempty"`
	Engine      string     `json:"engine,omitempty"`
	Backend     string     `json:"backend,omitempty"`
	FPS         float64    `json:"fps,omitempty"`
	LastFrameAt *time.Time `json:"lastFrameAt,omitempty"`
}

type plateBatch struct {
	Instance   string            `json:"instance"`
	Next       uint64            `json:"next"`
	Detections []plate.Detection `json:"detections"`
}

// plateSpeaker abstracts device speech. Until a stock speech path is verified
// on the device, the default speaker reports unsupported and nothing is
// marked as spoken.
type plateSpeaker interface {
	Available() (bool, string)
	Speak(ctx context.Context, text string) error
}

type unsupportedSpeaker struct{}

func (unsupportedSpeaker) Available() (bool, string) {
	return false, "本体スピーカーからの発話経路は未検証のため、まだ発話しません"
}
func (unsupportedSpeaker) Speak(context.Context, string) error {
	return errors.New("speech unsupported")
}

type plateService struct {
	dir     string
	store   *plate.Store
	sensor  *http.Client
	speaker plateSpeaker
	clock   func() time.Time

	mu        sync.Mutex
	settings  plate.Settings
	status    PlateSensorStatus
	statusAt  time.Time
	instance  string
	cursor    uint64
	lastPoll  time.Time
	errorCode string
}

func unixClient(path string) *http.Client {
	return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		},
	}}
}

func newPlateService(stateDir string, sensor *http.Client) *plateService {
	dir := filepath.Join(stateDir, "plate")
	p := &plateService{dir: dir, store: plate.NewStore(dir), sensor: sensor, speaker: unsupportedSpeaker{}, clock: time.Now, settings: plate.DefaultSettings(), status: PlateSensorStatus{State: "unavailable"}}
	if b, e := os.ReadFile(filepath.Join(dir, "settings.json")); e == nil {
		var s plate.Settings
		if json.Unmarshal(b, &s) == nil {
			if valid, e := plate.ValidateSettings(s); e == nil {
				p.settings = valid
			}
		}
	}
	return p
}

func (p *plateService) currentSettings() plate.Settings {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.settings
	s.Rules = append([]plate.Rule(nil), p.settings.Rules...)
	return s
}

func (p *plateService) sensorGet(ctx context.Context, path string, limit int64, into any) error {
	r, e := http.NewRequestWithContext(ctx, http.MethodGet, "http://plate-sensor"+path, nil)
	if e != nil {
		return e
	}
	resp, e := p.sensor.Do(r)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sensor status %d", resp.StatusCode)
	}
	if b, ok := into.(*[]byte); ok {
		data, e := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		if e == nil && int64(len(data)) > limit {
			e = errors.New("sensor response too large")
		}
		*b = data
		return e
	}
	return json.NewDecoder(io.LimitReader(resp.Body, limit)).Decode(into)
}

func (p *plateService) refreshStatus(ctx context.Context) {
	var status PlateSensorStatus
	if e := p.sensorGet(ctx, "/status", 64*1024, &status); e != nil || status.State == "" {
		status = PlateSensorStatus{State: "unavailable", Reason: "認識プロセスが起動していません"}
	}
	p.mu.Lock()
	p.status, p.statusAt = status, p.clock()
	p.mu.Unlock()
}

func (p *plateService) frame(ctx context.Context) func(string) ([]byte, error) {
	return func(id string) ([]byte, error) {
		var data []byte
		e := p.sensorGet(ctx, "/frames/"+url.PathEscape(id), plateFrameLimit, &data)
		return data, e
	}
}

func speechText(m plate.Match, r plate.Record) string {
	return fmt.Sprintf("条件「%s」に合うナンバー、%sを検知しました", m.RuleName, r.Display)
}

// poll pulls new detections once. It returns the number ingested.
func (p *plateService) poll(ctx context.Context) int {
	settings := p.currentSettings()
	p.mu.Lock()
	cursor := p.cursor
	p.mu.Unlock()
	var batch plateBatch
	if e := p.sensorGet(ctx, "/detections?after="+strconv.FormatUint(cursor, 10), plateBatchLimit, &batch); e != nil {
		p.recordPoll("sensor-unavailable")
		return 0
	}
	p.mu.Lock()
	if batch.Instance != p.instance {
		// The sensor restarted; its sequence numbers start again.
		p.instance, p.cursor = batch.Instance, 0
		cursor = 0
	}
	p.mu.Unlock()
	if batch.Next < cursor {
		p.recordPoll("invalid-sensor-cursor")
		return 0
	}
	count := 0
	code := ""
	for _, d := range batch.Detections {
		if d.Seq <= cursor {
			continue
		}
		outcome, e := p.store.Ingest(d, settings, p.frame(ctx))
		if e != nil {
			code = "storage-unavailable"
		}
		if outcome.Skipped == "" {
			count++
		}
		p.speak(ctx, outcome)
	}
	p.mu.Lock()
	if p.instance == batch.Instance && batch.Next > p.cursor {
		p.cursor = batch.Next
	}
	p.mu.Unlock()
	p.recordPoll(code)
	return count
}

func (p *plateService) speak(ctx context.Context, outcome plate.Outcome) {
	for _, m := range outcome.NewMatches {
		if !m.Speak {
			continue
		}
		state := plate.SpeechSpoken
		if ok, _ := p.speaker.Available(); !ok {
			state = plate.SpeechUnsupported
		} else if e := p.speaker.Speak(ctx, speechText(m, outcome.Record)); e != nil {
			state = plate.SpeechFailed
		}
		_ = p.store.SetSpeech(outcome.Record.ID, state)
		return // one announcement per vehicle, for the first speaking rule
	}
}

func (p *plateService) recordPoll(code string) {
	p.mu.Lock()
	p.lastPoll, p.errorCode = p.clock().UTC(), code
	p.mu.Unlock()
}

func (p *plateService) collect(ctx context.Context) {
	ticker := time.NewTicker(platePollInterval)
	defer ticker.Stop()
	defer p.store.Flush()
	var statusAt, pruneAt time.Time
	for {
		now := p.clock()
		if now.Sub(statusAt) >= plateStatusEvery {
			p.refreshStatus(ctx)
			statusAt = now
		}
		if now.Sub(pruneAt) >= platePruneInterval {
			_, _ = p.store.Prune(p.currentSettings())
			pruneAt = now
		}
		if p.currentSettings().Enabled {
			p.poll(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) registerPlate() {
	p := s.plates
	s.mux.HandleFunc("GET /api/v1/plate/status", func(w http.ResponseWriter, r *http.Request) {
		settings := p.currentSettings()
		speech, reason := p.speaker.Available()
		p.mu.Lock()
		status, lastPoll, code := p.status, p.lastPoll, p.errorCode
		p.mu.Unlock()
		var poll *time.Time
		if !lastPoll.IsZero() {
			poll = &lastPoll
		}
		jsonOut(w, 200, map[string]any{
			"enabled":         settings.Enabled,
			"revision":        settings.Revision,
			"sensor":          status,
			"collector":       map[string]any{"lastPollAt": poll, "error": code},
			"speech":          map[string]any{"available": speech, "reason": reason},
			"usage":           p.store.Usage(),
			"imageQuotaBytes": int64(settings.ImageQuotaMB) << 20,
		})
	})
	s.mux.HandleFunc("GET /api/v1/plate/settings", func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 200, p.currentSettings())
	})
	s.mux.HandleFunc("PUT /api/v1/plate/settings", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ExpectedRevision int            `json:"expectedRevision"`
			Settings         plate.Settings `json:"settings"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, plateSettingsLimit)
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if d.Decode(&body) != nil || d.Decode(&struct{}{}) != io.EOF {
			fail(w, 400, "INVALID_INPUT", "入力を確認してください")
			return
		}
		next, e := plate.ValidateSettings(body.Settings)
		if e != nil {
			fail(w, 400, "INVALID_PLATE_SETTINGS", e.Error())
			return
		}
		p.mu.Lock()
		if body.ExpectedRevision != p.settings.Revision {
			p.mu.Unlock()
			fail(w, 409, "REVISION_CONFLICT", "別の画面で設定が変更されました。再読み込みしてください")
			return
		}
		next.Revision = p.settings.Revision + 1
		if e = config.Atomic(filepath.Join(p.dir, "settings.json"), next); e != nil {
			p.mu.Unlock()
			s.audit("plate.settings.update", "storage-failed")
			fail(w, 503, "STORAGE_UNAVAILABLE", "設定を保存できませんでした")
			return
		}
		p.settings = next
		p.mu.Unlock()
		if !next.Enabled {
			_ = p.store.Flush()
		}
		s.audit("plate.settings.update", "accepted")
		jsonOut(w, 200, next)
	})
	s.mux.HandleFunc("GET /api/v1/plate/records", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		for key, values := range q {
			if len(values) != 1 || (key != "from" && key != "to" && key != "matched" && key != "q" && key != "limit") {
				fail(w, 400, "INVALID_QUERY", "検索条件を確認してください")
				return
			}
		}
		limit := 0
		if v := q.Get("limit"); v != "" {
			n, e := strconv.Atoi(v)
			if e != nil || n < 1 {
				fail(w, 400, "INVALID_QUERY", "件数を確認してください")
				return
			}
			limit = n
		}
		records, truncated, e := p.store.List(plate.Query{From: q.Get("from"), To: q.Get("to"), MatchedOnly: q.Get("matched") == "true", Text: q.Get("q"), Limit: limit})
		if records == nil && e != nil {
			fail(w, 400, "INVALID_QUERY", e.Error())
			return
		}
		out := make([]map[string]any, 0, len(records))
		for _, record := range records {
			_, imageErr := p.store.ImagePath(record.ID)
			out = append(out, map[string]any{"record": record, "imageAvailable": record.ImageID != "" && imageErr == nil})
		}
		reason := ""
		if e != nil {
			reason = "一部の記録を読み取れませんでした"
		}
		jsonOut(w, 200, map[string]any{"records": out, "truncated": truncated, "reason": reason})
	})
	s.mux.HandleFunc("GET /api/v1/plate/records/{id}/image", func(w http.ResponseWriter, r *http.Request) {
		path, e := p.store.ImagePath(r.PathValue("id"))
		if e != nil {
			fail(w, 404, "IMAGE_NOT_FOUND", "画像は保存されていないか、保存期間・容量上限により削除されました")
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "private, max-age=3600")
		http.ServeFile(w, r, path)
	})
}
