package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/config"
	"github.com/np1-control-center/ncc/internal/led"
	"github.com/np1-control-center/ncc/internal/numbershare"
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
	platePollInterval     = time.Second
	plateStatusEvery      = 2 * time.Second
	platePruneInterval    = 10 * time.Minute
	plateHealthEvery      = time.Minute
	plateTrialHealthEvery = 10 * time.Second
	plateHealthDays       = 7
	plateBatchLimit       = 2 << 20
	plateFrameLimit       = 4 << 20
	plateSettingsLimit    = 64 * 1024
	plateUploadLimit      = platespeech.MaxClipBytes + 64*1024
)

// PlateSensorStatus is reported by the sensor; the server never infers it.
type PlateSensorStatus struct {
	InputSource      string     `json:"inputSource,omitempty"`
	State            string     `json:"state"`
	Reason           string     `json:"reason,omitempty"`
	Instance         string     `json:"instance,omitempty"`
	Engine           string     `json:"engine,omitempty"`
	Backend          string     `json:"backend,omitempty"`
	FPS              float64    `json:"fps,omitempty"`
	TargetFPS        float64    `json:"targetFps,omitempty"`
	LastFrameAt      *time.Time `json:"lastFrameAt,omitempty"`
	OCRQueuePending  int        `json:"ocrQueuePending"`
	OCRQueueCapacity int        `json:"ocrQueueCapacity"`
	OCRCompleted     uint64     `json:"ocrCompleted"`
	OCRDropped       uint64     `json:"ocrDropped"`
	OCRSuppressed    uint64     `json:"ocrSuppressed"`
}

type plateBatch struct {
	Instance   string            `json:"instance"`
	Next       uint64            `json:"next"`
	Last       *uint64           `json:"last"`
	Detections []plate.Detection `json:"detections"`
}

// plateSpeaker announces a rule match on the device speaker. Announce
// queues the announcement and reports the real playback outcome to done; a
// record is marked spoken only after playback succeeded.
type plateSpeaker interface {
	Available() (bool, string)
	Announce(ctx context.Context, rule plate.Rule, reading plate.Reading, done func(error)) bool
	AnnounceReading(ctx context.Context, reading plate.Reading, light led.Notification, done func(error)) bool
}

type plateService struct {
	dir     string
	store   *plate.Store
	sensor  *http.Client
	speaker plateSpeaker
	voice   *platespeech.Manager
	share   *numbershare.Service
	clock   func() time.Time

	mu        sync.Mutex
	settings  plate.Settings
	status    PlateSensorStatus
	statusAt  time.Time
	instance  string
	cursor    uint64
	lastPoll  time.Time
	errorCode string
	// Health log inputs: the sensor's newest sequence number and records ingested since the
	// last health line.
	sensorLast  uint64
	ingested    int
	trialCounts map[string]int
	// positions looks up logged positions for records detected without one (nil: disabled);
	// backfill holds the retry schedule of records it could not fill yet.
	positions     func(context.Context, []time.Time) ([]*plate.Location, error)
	backfill      map[string]plateBackfillState
	livePositions func(context.Context, []time.Time) ([]*plate.Location, error)
	livePending   map[string]plateTimeRange
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
	p := &plateService{dir: dir, store: plate.NewStoreWithExternalImages(dir, os.Getenv("NCC_PLATE_IMAGE_DIR")), sensor: sensor, speaker: voice, voice: voice, clock: time.Now, settings: plate.DefaultSettings(), status: PlateSensorStatus{State: "unavailable"}}
	if repaired, err := p.store.RepairRecent(p.clock()); err != nil {
		slog.Warn("plate encounter repair failed", "error", err)
	} else if repaired > 0 {
		slog.Info("plate encounter fragments repaired", "records", repaired)
	}
	if os.Getenv("NCC_PLATE_DIAGNOSTICS") == "1" {
		p.trialCounts = map[string]int{}
	}
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
		// Decode over defaults so settings written by older/minimal deployments
		// keep their explicit fields while newly introduced fields receive safe
		// defaults. Decoding into a zero value made {"enabled":true} invalid and
		// silently disabled collection.
		s := plate.DefaultSettings()
		if json.Unmarshal(b, &s) == nil {
			if valid, e := plate.ValidateSettings(s); e == nil {
				p.settings = valid
			}
		}
	}
	voice.Configure(p.settings)
	return p
}

// RunPlateCollector persists confirmed readings from the already-running
// plate sensor without opening the management web listeners. This is used on
// installations whose owner dashboard occupies ports 80/443 but still need
// the local record store for NumberShare's offline queue.
func RunPlateCollector(ctx context.Context, stateDir, positionURL string) {
	p := newPlateService(stateDir, unixClient(PlateSensorSocketPath()))
	p.livePositions = plateDashboardPositions(&http.Client{Timeout: 4 * time.Second}, positionURL)
	p.collect(ctx)
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
	var previousID string
	var previous []byte
	return func(id string) ([]byte, error) {
		if previousID == id && previous != nil {
			return previous, nil
		}
		var data []byte
		e := p.sensorGet(ctx, "/frames/"+url.PathEscape(id), plateFrameLimit, &data)
		if e == nil {
			previousID, previous = id, data
		}
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
	if batch.Instance != p.instance || (batch.Last != nil && *batch.Last < cursor) {
		// The sensor restarted; its sequence numbers start again. A saved cursor beyond the
		// sensor's newest number means the same: without this check every new detection
		// would be ignored until the sensor caught up with the old cursor.
		p.instance, p.cursor = batch.Instance, 0
		cursor = 0
		p.mu.Unlock()
		if e := p.sensorGet(ctx, "/detections?after=0", plateBatchLimit, &batch); e != nil {
			p.recordPoll("sensor-unavailable")
			return 0
		}
		p.mu.Lock()
	}
	if batch.Last != nil {
		p.sensorLast = *batch.Last
	}
	p.mu.Unlock()
	if batch.Next < cursor {
		p.recordPoll("invalid-sensor-cursor")
		return 0
	}
	p.attachLocations(ctx, batch.Detections)
	count := 0
	code := ""
	frameImage := p.frame(ctx)
	for _, d := range batch.Detections {
		if d.Seq <= cursor {
			continue
		}
		outcome, e := p.store.Ingest(d, settings, frameImage)
		if p.trialCounts != nil {
			p.trialCounts["received"]++
			switch {
			case e != nil:
				p.trialCounts["storage-error"]++
			case outcome.Skipped != "":
				p.trialCounts[outcome.Skipped]++
			case outcome.New:
				p.trialCounts["new-record"]++
				if outcome.Record.Location != nil {
					p.trialCounts["saved-with-position"]++
				} else {
					p.trialCounts["awaiting-position"]++
				}
				if outcome.Record.ImageID != "" {
					p.trialCounts["saved-image"]++
				} else if outcome.Record.ImageNote != "" {
					p.trialCounts["image-unavailable"]++
				}
			default:
				p.trialCounts["updated-record"]++
			}
		}
		if e != nil {
			code = "storage-unavailable"
		}
		if e == nil && outcome.Skipped == "" {
			p.rememberLiveLocation(outcome.Record)
		}
		if e == nil && outcome.Skipped == "" && outcome.Record.ID != "" && p.share != nil {
			sourceID := ""
			if d.Track != nil {
				sourceID = d.Track.ID
			}
			if shareErr := p.share.EnqueueTrack(outcome.Record, sourceID); shareErr != nil {
				code = "storage-unavailable"
			}
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
	p.ingested += count
	p.mu.Unlock()
	if advanced {
		if e := config.Atomic(filepath.Join(p.dir, "cursor.json"), map[string]any{"instance": instance, "cursor": next}); e != nil {
			code = "storage-unavailable"
		}
	}
	p.recordPoll(code)
	return count
}

func (p *plateService) attachLocations(ctx context.Context, detections []plate.Detection) {
	if p.livePositions == nil {
		return
	}
	indices := make([]int, 0, len(detections))
	ranges := make([]plateTimeRange, 0, len(detections))
	for i := range detections {
		if detections[i].Location == nil {
			indices = append(indices, i)
			d := detections[i]
			last := d.At
			if d.Track != nil && d.Track.LastSeen.After(last) {
				last = d.Track.LastSeen
			}
			ranges = append(ranges, plateTimeRange{d.At, last})
		}
	}
	for i, location := range p.matchLiveLocations(ctx, ranges) {
		if location != nil {
			detections[indices[i]].Location = location
		}
	}
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
	if lightRule == nil && outcome.New && p.currentSettings().DetectionLED {
		lightRule = &plate.Rule{LED: led.Notification{Mode: "solid", Effect: "breathe", RGB: [3]int{255, 255, 255}, Brightness: 112}}
	}
	if speechRule == nil && outcome.New && p.currentSettings().SpeakAllDetections {
		// Debug: read every new record from cached clips only.
		var light led.Notification
		if lightRule != nil {
			light = lightRule.LED
		}
		if !p.speaker.AnnounceReading(context.WithoutCancel(ctx), outcome.Record.Reading, light, func(e error) {
			if e != nil {
				slog.Info("plate debug speech", "result", "failed", "reason", e.Error())
			}
		}) {
			slog.Info("plate debug speech", "result", "dropped")
		}
		return
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
	if p.positions != nil {
		go p.collectLocationBackfill(ctx)
	}
	ticker := time.NewTicker(platePollInterval)
	defer ticker.Stop()
	defer p.store.Flush()
	healthEvery := plateHealthEvery
	if p.trialCounts != nil {
		healthEvery = plateTrialHealthEvery
	}
	var statusAt, pruneAt, healthAt time.Time
	for {
		now := p.clock()
		if now.Sub(statusAt) >= plateStatusEvery {
			p.refreshStatus(ctx)
			statusAt = now
		}
		if now.Sub(healthAt) >= healthEvery {
			p.writeHealth(now)
			healthAt = now
		}
		if now.Sub(pruneAt) >= platePruneInterval {
			_, _ = p.store.Prune(p.currentSettings())
			pruneAt = now
		}
		if p.currentSettings().Enabled {
			p.retryLiveLocations(ctx, now)
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
		shared := numbershare.Results(filepath.Dir(p.dir))
		out := make([]map[string]any, 0, len(records))
		for _, record := range records {
			_, imageErr := p.store.ImagePath(record.ImageID)
			item := map[string]any{"record": record, "imageAvailable": record.ImageID != "" && imageErr == nil}
			if result, ok := shared[record.ID]; ok {
				item["numberShare"] = result
			}
			out = append(out, item)
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
	s.registerVoiceCache()
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
		endpoint, key := p.voice.APIConfigValue()
		jsonOut(w, 200, map[string]any{"endpoint": endpoint, "defaultEndpoint": platespeech.DefaultAPIBase, "keyConfigured": key != "", "apiKey": key})
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
		s.pics.mu.Lock()
		s.pics.restartPreparation()
		s.pics.mu.Unlock()
		s.audit("voicevox.update", "accepted")
		endpoint, savedKey := p.voice.APIConfigValue()
		jsonOut(w, 200, map[string]any{"endpoint": endpoint, "defaultEndpoint": platespeech.DefaultAPIBase, "keyConfigured": savedKey != "", "apiKey": savedKey})
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

// writeHealth appends one line per minute to state/plate/health/<date>.jsonl, kept for a week.
// The device journal does not survive a reboot, so this is what tells afterwards whether a
// gap in records came from the collector (no lines), the camera (old lastFrameAt), the
// detector (sensorLast not moving) or simply no plates in view.
func (p *plateService) writeHealth(now time.Time) {
	p.mu.Lock()
	line := map[string]any{"at": now.UTC(), "enabled": p.settings.Enabled, "sensor": p.status.State, "reason": p.status.Reason,
		"fps": p.status.FPS, "targetFps": p.status.TargetFPS, "lastFrameAt": p.status.LastFrameAt, "instance": p.instance, "cursor": p.cursor,
		"sensorLast": p.sensorLast, "ingested": p.ingested, "lastPollAt": p.lastPoll, "pollError": p.errorCode,
		"ocrQueuePending": p.status.OCRQueuePending, "ocrQueueCapacity": p.status.OCRQueueCapacity, "ocrCompleted": p.status.OCRCompleted, "ocrDropped": p.status.OCRDropped,
		"ocrSuppressed": p.status.OCRSuppressed}
	if p.trialCounts != nil {
		line["engine"], line["backend"], line["intervalCounts"] = p.status.Engine, p.status.Backend, p.trialCounts
		p.trialCounts = map[string]int{}
	}
	p.ingested = 0
	p.mu.Unlock()
	dir := filepath.Join(p.dir, "health")
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	b, e := json.Marshal(line)
	if e != nil {
		return
	}
	f, e := os.OpenFile(filepath.Join(dir, now.UTC().Format("2006-01-02")+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if e != nil {
		return
	}
	f.Write(append(b, '\n'))
	f.Close()
	if entries, e := os.ReadDir(dir); e == nil {
		cutoff := now.UTC().AddDate(0, 0, -plateHealthDays).Format("2006-01-02")
		for _, entry := range entries {
			if name := entry.Name(); len(name) >= 10 && name[:10] < cutoff {
				os.Remove(filepath.Join(dir, name))
			}
		}
	}
}
