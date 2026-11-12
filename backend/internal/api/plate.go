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
	"github.com/np1-control-center/ncc/internal/platespeech"
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
	plateUploadLimit   = platespeech.MaxClipBytes + 64*1024
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

// plateSpeaker announces a rule match on the device speaker. Announce
// queues the announcement and reports the real playback outcome to done; a
// record is marked spoken only after playback succeeded.
type plateSpeaker interface {
	Available() (bool, string)
	Announce(ctx context.Context, rule plate.Rule, reading plate.Reading, done func(error)) bool
}

type plateService struct {
	dir     string
	store   *plate.Store
	sensor  *http.Client
	speaker plateSpeaker
	voice   *platespeech.Manager
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
	voice := platespeech.NewManager(filepath.Join(dir, "speech"), platespeech.NewClient(), platespeech.Pacat{Path: "/usr/bin/pacat"})
	p := &plateService{dir: dir, store: plate.NewStore(dir), sensor: sensor, speaker: voice, voice: voice, clock: time.Now, settings: plate.DefaultSettings(), status: PlateSensorStatus{State: "unavailable"}}
	// Resume the sensor cursor so a server restart does not re-ingest
	// detections the sensor still holds (that duplicated records).
	if b, e := os.ReadFile(filepath.Join(dir, "cursor.json")); e == nil {
		var c struct {
			Instance string `json:"instance"`
			Cursor   uint64 `json:"cursor"`
		}
		if json.Unmarshal(b, &c) == nil {
			p.instance, p.cursor = c.Instance, c.Cursor
		}
	}
	if b, e := os.ReadFile(filepath.Join(dir, "settings.json")); e == nil {
		var s plate.Settings
		if json.Unmarshal(b, &s) == nil {
			if valid, e := plate.ValidateSettings(s); e == nil {
				p.settings = valid
			}
		}
	}
	voice.Configure(p.settings)
	return p
}

func (p *plateService) currentSettings() plate.Settings {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.settings
	s.Rules = append([]plate.Rule{}, p.settings.Rules...)
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
	advanced := p.instance == batch.Instance && batch.Next > p.cursor
	if advanced {
		p.cursor = batch.Next
	}
	instance, next := p.instance, p.cursor
	p.mu.Unlock()
	if advanced {
		if e := config.Atomic(filepath.Join(p.dir, "cursor.json"), map[string]any{"instance": instance, "cursor": next}); e != nil {
			code = "storage-unavailable"
		}
	}
	p.recordPoll(code)
	return count
}

func (p *plateService) rule(id string) (plate.Rule, bool) {
	for _, r := range p.currentSettings().Rules {
		if r.ID == id {
			return r, true
		}
	}
	return plate.Rule{}, false
}

func (p *plateService) speak(ctx context.Context, outcome plate.Outcome) {
	var speechRule, lightRule *plate.Rule
	for _, m := range outcome.NewMatches {
		rule, found := p.rule(m.RuleID)
		if !found {
			continue
		}
		if m.Speak && speechRule == nil {
			copy := rule
			speechRule = &copy
		}
		if rule.LED.Enabled() && lightRule == nil {
			copy := rule
			lightRule = &copy
		}
	}
	if speechRule == nil {
		if lightRule != nil {
			lightRule.Speak = false
			p.speaker.Announce(context.WithoutCancel(ctx), *lightRule, outcome.Record.Reading, func(error) {})
		}
		return
	}
	rule := *speechRule
	if !rule.LED.Enabled() && lightRule != nil {
		rule.LED = lightRule.LED
	}
	id := outcome.Record.ID
	if ok, _ := p.speaker.Available(); !ok {
		_ = p.store.SetSpeech(id, plate.SpeechUnsupported)
		if rule.LED.Enabled() {
			rule.Speak = false
			p.speaker.Announce(context.WithoutCancel(ctx), rule, outcome.Record.Reading, func(error) {})
		}
		return
	}
	_ = p.store.SetSpeech(id, plate.SpeechRequested)
	queued := p.speaker.Announce(context.WithoutCancel(ctx), rule, outcome.Record.Reading, func(e error) {
		state := plate.SpeechSpoken
		if e != nil {
			state = plate.SpeechFailed
		}
		_ = p.store.SetSpeech(id, state)
	})
	if !queued {
		_ = p.store.SetSpeech(id, plate.SpeechFailed)
	}
}

func (p *plateService) recordPoll(code string) {
	p.mu.Lock()
	p.lastPoll, p.errorCode = p.clock().UTC(), code
	p.mu.Unlock()
}

func (p *plateService) collect(ctx context.Context) {
	go p.voice.Run(ctx)
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
		for _, rule := range next.Rules {
			if rule.MessageAudio != "" && !p.voice.HasUpload(rule.MessageAudio) {
				fail(w, 400, "INVALID_PLATE_SETTINGS", fmt.Sprintf("「%s」の発話音声が見つかりません。もう一度アップロードしてください", rule.Name))
				return
			}
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
		// Synthesize the rules' announcements now, in the background.
		p.voice.Configure(next)
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
	s.registerPlateSpeech()
}

// registerPlateSpeech serves announcement preparation (rule clips, uploads,
// preview) and the owner's VOICEVOX API settings shown on the system page.
func (s *Server) registerPlateSpeech() {
	p := s.plates
	s.mux.HandleFunc("GET /api/v1/plate/speech", func(w http.ResponseWriter, r *http.Request) {
		available, reason := p.speaker.Available()
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		jsonOut(w, 200, map[string]any{"available": available, "reason": reason, "status": p.voice.Status(), "styles": p.voice.Styles(ctx)})
	})
	s.mux.HandleFunc("POST /api/v1/plate/speech/uploads", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, plateUploadLimit)
		data, e := io.ReadAll(r.Body)
		if e != nil {
			fail(w, 413, "UPLOAD_TOO_LARGE", "音声は30秒以内にしてください")
			return
		}
		pcm, e := platespeech.ParseWAV(data)
		if e != nil {
			fail(w, 400, "INVALID_AUDIO", e.Error())
			return
		}
		id, e := p.voice.SaveUpload(pcm)
		if e != nil {
			fail(w, 503, "STORAGE_UNAVAILABLE", "音声を保存できませんでした")
			return
		}
		s.audit("plate.speech.upload", "accepted")
		jsonOut(w, 200, map[string]any{"id": id, "durationMs": len(pcm) * 1000 / (platespeech.SampleRate * 2)})
	})
	s.mux.HandleFunc("GET /api/v1/plate/speech/uploads/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !p.voice.HasUpload(id) {
			fail(w, 404, "AUDIO_NOT_FOUND", "音声が見つかりません")
			return
		}
		pcm, e := os.ReadFile(filepath.Join(p.dir, "speech", "uploads", id+".pcm"))
		if e != nil {
			fail(w, 404, "AUDIO_NOT_FOUND", "音声が見つかりません")
			return
		}
		w.Header().Set("Content-Type", "audio/wav")
		w.Header().Set("Cache-Control", "private, max-age=86400")
		w.Write(platespeech.EncodeWAV(pcm))
	})
	s.mux.HandleFunc("POST /api/v1/plate/speech/preview", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RuleID string `json:"ruleId"`
		}
		if !decode(w, r, &body) {
			return
		}
		rule, ok := p.rule(body.RuleID)
		if !ok {
			fail(w, 404, "RULE_NOT_FOUND", "保存済みの条件が見つかりません。先に設定を保存してください")
			return
		}
		if !rule.Speak && !rule.LED.Enabled() {
			fail(w, 409, "NOTIFICATION_DISABLED", "発話またはLED通知を有効にしてください")
			return
		}
		if available, reason := p.speaker.Available(); rule.Speak && !available {
			fail(w, 409, "SPEECH_UNAVAILABLE", reason)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		if e := p.voice.Preview(ctx, rule); e != nil {
			fail(w, 503, "SPEECH_FAILED", e.Error())
			return
		}
		s.audit("plate.speech.preview", "accepted")
		jsonOut(w, 200, map[string]any{"spoken": rule.Speak, "led": rule.LED.Enabled()})
	})
	s.mux.HandleFunc("GET /api/v1/plate/speech/rules/{id}/preview", func(w http.ResponseWriter, r *http.Request) {
		rule, ok := p.rule(r.PathValue("id"))
		if !ok {
			fail(w, 404, "RULE_NOT_FOUND", "保存済みの条件が見つかりません。先に設定を保存してください")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		pcm, e := p.voice.PreviewAudio(ctx, rule)
		if e != nil {
			fail(w, 503, "SPEECH_FAILED", e.Error())
			return
		}
		w.Header().Set("Content-Type", "audio/wav")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(platespeech.EncodeWAV(pcm))
	})
	s.mux.HandleFunc("GET /api/v1/voicevox", func(w http.ResponseWriter, r *http.Request) {
		endpoint, keyConfigured := p.voice.APIConfig()
		jsonOut(w, 200, map[string]any{"endpoint": endpoint, "defaultEndpoint": platespeech.DefaultAPIBase, "keyConfigured": keyConfigured})
	})
	s.mux.HandleFunc("PUT /api/v1/voicevox", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Endpoint string  `json:"endpoint"`
			Key      *string `json:"key"` // omitted: keep; "": clear
		}
		if !decode(w, r, &body) {
			return
		}
		if e := p.voice.SetAPI(body.Endpoint, body.Key); e != nil {
			fail(w, 400, "INVALID_VOICEVOX", e.Error())
			return
		}
		s.audit("voicevox.update", "accepted")
		endpoint, keyConfigured := p.voice.APIConfig()
		jsonOut(w, 200, map[string]any{"endpoint": endpoint, "defaultEndpoint": platespeech.DefaultAPIBase, "keyConfigured": keyConfigured})
	})
	s.mux.HandleFunc("POST /api/v1/voicevox/check", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		points, e := p.voice.CheckAPI(ctx)
		if e != nil {
			fail(w, 502, "VOICEVOX_UNAVAILABLE", e.Error())
			return
		}
		jsonOut(w, 200, points)
	})
}
