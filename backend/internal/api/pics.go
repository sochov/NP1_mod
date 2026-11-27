package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/np1-control-center/ncc/internal/led"
	"github.com/np1-control-center/ncc/internal/pics"
	"github.com/np1-control-center/ncc/internal/platespeech"
	"github.com/np1-control-center/ncc/internal/trips"
)

const picsSettingsFile = "pics/settings.json"

type picsSettings struct {
	Enabled     bool `json:"enabled"`
	Speech      bool `json:"speech"`
	Directional bool `json:"directional"`
	LED         bool `json:"led"`
	Style       int  `json:"style"`
	Volume      int  `json:"volume"`
}

func defaultPICSSettings() picsSettings {
	return picsSettings{Enabled: true, Style: 1, Volume: 60}
}

func (v picsSettings) validate() error {
	if v.Style < 0 || v.Style > 2147483647 || v.Volume < 0 || v.Volume > 100 {
		return errors.New("VOICEVOXの声と音量を確認してください")
	}
	return nil
}

type picsService struct {
	path   string
	voice  *platespeech.Manager
	scan   func(context.Context, func(), func(string, []byte, time.Time)) error
	setLED func(context.Context, led.TrafficColors, time.Time, time.Time) error

	mu                                  sync.Mutex
	settings                            picsSettings
	monitor                             pics.Monitor
	scanState, scanReason, speechReason string
	scanChanged, speechChanged          chan struct{}
	scanCancel, prepCancel, playCancel  context.CancelFunc
	playing                             *pics.Cue
	playingSince                        time.Time
	playToken                           uint64
	speechPrepared                      bool
	speechDecision                      string
	gps                                 func(context.Context) *trips.TrackPoint
	positions                           []trips.TrackPoint
	course                              *float64
	directions                          map[string]picsAxes
	directionsPath                      string
	historyDir                          string
	historyMu                           sync.Mutex
	receptions                          chan []picsReception
	historyIncomplete                   bool
	// heartbeat is the last speech tick (Unix nanoseconds); the systemd
	// watchdog stops being fed when it stalls, so the service restarts.
	heartbeat atomic.Int64
	speechLog chan picsSpeechEvent
	logPath   string
}

func newPICSService(dir string, voice *platespeech.Manager) *picsService {
	p := &picsService{
		path: filepath.Join(dir, picsSettingsFile), voice: voice, settings: defaultPICSSettings(), scanState: "starting",
		scanChanged: make(chan struct{}, 1), speechChanged: make(chan struct{}, 1),
		directionsPath: filepath.Join(dir, "pics", "directions.json"), historyDir: filepath.Join(dir, "pics", "receptions"),
		receptions: make(chan []picsReception, 128),
		speechLog:  make(chan picsSpeechEvent, 64), logPath: filepath.Join(dir, "pics", "speech-log.jsonl"),
	}
	p.directions = loadPICSDirections(p.directionsPath)
	if data, err := os.ReadFile(p.path); err == nil {
		settings := p.settings
		if json.Unmarshal(data, &settings) == nil && settings.validate() == nil {
			p.settings = settings
		}
	}
	return p
}

func wakePICS(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (p *picsService) save(settings picsSettings) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if settings == p.settings {
		return nil
	}
	data, _ := json.Marshal(settings)
	if err := writeStateFile(p.path, data); err != nil {
		return err
	}
	if settings.Enabled != p.settings.Enabled {
		if p.scanCancel != nil {
			p.scanCancel()
		}
		p.monitor = pics.Monitor{}
		p.positions, p.course = nil, nil
		p.scanState, p.scanReason = "starting", ""
		wakePICS(p.scanChanged)
	}
	if p.playCancel != nil {
		p.playCancel()
	}
	p.settings = settings
	p.restartPreparation()
	return nil
}

// restartPreparation is called with p.mu held.
func (p *picsService) restartPreparation() {
	if p.prepCancel != nil {
		p.prepCancel()
	}
	p.speechReason = ""
	p.speechPrepared = false
	wakePICS(p.speechChanged)
}

func picsWait(ctx context.Context, changed <-chan struct{}, retry time.Duration) {
	var next <-chan time.Time
	if retry > 0 {
		timer := time.NewTimer(retry)
		defer timer.Stop()
		next = timer.C
	}
	select {
	case <-ctx.Done():
	case <-changed:
	case <-next:
	}
}

func (p *picsService) run(ctx context.Context) {
	go p.runScan(ctx)
	go p.prepareSpeech(ctx)
	if p.setLED != nil {
		go p.runLED(ctx)
	}
	go p.collectPICSHistory(ctx)
	go p.writeSpeechLog(ctx)
	if p.gps != nil {
		go p.collectPICSGPS(ctx)
	}
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		p.heartbeat.Store(time.Now().UnixNano())
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			p.tickSpeech(ctx)
		}
	}
}

// picsStallLimit is how long the speech loop may stop ticking before the
// process reports itself unhealthy (systemd WatchdogSec=30 then restarts it).
const picsStallLimit = 20 * time.Second

// healthy reports a stalled speech loop. Before run starts there is nothing
// to check.
func (p *picsService) healthy(now time.Time) error {
	beat := p.heartbeat.Load()
	if beat == 0 {
		return nil
	}
	if age := now.Sub(time.Unix(0, beat)); age > picsStallLimit {
		return errors.New("PICS speech loop stalled for " + age.Round(time.Second).String())
	}
	return nil
}

func (p *picsService) runScan(ctx context.Context) {
	var retry time.Duration
	for ctx.Err() == nil {
		p.mu.Lock()
		if !p.settings.Enabled {
			retry = 0
			p.mu.Unlock()
			picsWait(ctx, p.scanChanged, 0)
			continue
		}
		scanCtx, cancel := context.WithCancel(ctx)
		p.scanCancel = cancel
		p.mu.Unlock()
		var confirmed time.Time
		err := p.scan(scanCtx, func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			if scanCtx.Err() == nil {
				if confirmed.IsZero() {
					confirmed = time.Now()
				}
				p.scanState, p.scanReason = "scanning", ""
			}
		}, func(source string, data []byte, received time.Time) {
			p.mu.Lock()
			defer p.mu.Unlock()
			if scanCtx.Err() == nil {
				now := time.Now()
				skipped := p.monitor.Diagnostics().SkippedChanges
				p.recordPICS(p.monitor.Receive(source, data, received), received)
				if p.monitor.Diagnostics().SkippedChanges > skipped {
					p.notePICSSpeech("observation-gap")
				}
				p.cancelInvalidCue(now)
			}
		})
		p.mu.Lock()
		p.scanCancel = nil
		p.monitor.Interrupt()
		p.cancelInvalidCue(time.Now())
		if scanCtx.Err() == nil {
			if err == nil {
				err = errors.New("純正Bluetoothからの受信が切断されました")
			}
			p.scanState, p.scanReason = "unavailable", err.Error()
			if !confirmed.IsZero() && time.Since(confirmed) >= 5*time.Second {
				retry = 0
			}
			retry = min(max(250*time.Millisecond, 2*retry), 4*time.Second)
		} else {
			retry = 0
		}
		p.mu.Unlock()
		cancel()
		if retry > 0 {
			picsWait(ctx, p.scanChanged, retry)
		}
	}
}

func (p *picsService) prepareSpeech(ctx context.Context) {
	for ctx.Err() == nil {
		// The fixed messages are prepared as soon as the API is configured,
		// even while PICS or its speech is off, so turning them on just
		// before a drive does not leave the first crossings silent.
		p.mu.Lock()
		cfg := p.settings
		prep, cancel := context.WithCancel(ctx)
		p.prepCancel = cancel
		p.mu.Unlock()
		err := p.voice.Prepare(prep, cfg.Style, pics.Vocabulary())
		p.mu.Lock()
		p.prepCancel = nil
		if prep.Err() == nil {
			p.speechPrepared = err == nil
			if err == nil {
				p.speechReason = ""
			} else {
				p.speechReason = err.Error()
			}
		}
		p.mu.Unlock()
		cancel()
		// Re-check the cache regularly so a clip lost from storage is fetched
		// again before it is needed; failures retry sooner.
		retry := 30 * time.Minute
		if err != nil {
			retry = 2 * time.Minute
		}
		picsWait(ctx, p.speechChanged, retry)
	}
}

func (p *picsService) cancelInvalidCue(now time.Time) {
	if p.playing != nil {
		id, slot := p.playing.SignalKey()
		if !p.monitor.StillValid(*p.playing, now) || !p.directionAllows(id, slot, now) {
			p.playCancel()
		}
	}
}

func (p *picsService) tickSpeech(ctx context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	p.cancelInvalidCue(now)
	if p.playing != nil && now.Sub(p.playingSince) > picsPlayLimit {
		// Playback is bounded by its own timeouts; this only frees the
		// speaker for new cues if one ever hangs.
		p.playCancel()
		p.playing, p.playCancel = nil, nil
		p.notePICSSpeech("playback-stalled")
	}
	cfg := p.settings
	if !cfg.Enabled || p.playing != nil {
		return
	}
	cue, ok := p.monitor.NextCueFor(now, func(id string, slot int) bool { return p.directionAllows(id, slot, now) })
	if !ok {
		if cfg.Directional {
			if _, filtered := p.monitor.NextCue(now); filtered {
				p.notePICSSpeech("direction-filter")
			}
		}
		return
	}
	switch {
	case !cfg.Speech:
		p.notePICSSpeech("disabled")
		return
	case cfg.Volume == 0:
		p.notePICSSpeech("zero-volume")
		return
	}
	if p.voice.HardwareAvailable() != nil {
		p.notePICSSpeech("audio-unavailable")
		return
	}
	// Each cue needs only its own clips: while the rest of the set is still
	// being prepared (first setup, a new voice, offline on the road) the
	// cached ones are spoken.
	texts := pics.SpeechParts(cue)
	for _, text := range texts {
		if !p.voice.Prepared(cfg.Style, text) {
			p.notePICSSpeech("clip-unavailable")
			// Fetch the missing clip again, without interrupting a
			// preparation that is already running; a failed one retries on
			// its own schedule.
			if p.speechPrepared {
				p.speechPrepared = false
				wakePICS(p.speechChanged)
			}
			return
		}
	}
	play, cancel := context.WithCancel(ctx)
	p.playToken++
	token := p.playToken
	p.playing, p.playCancel, p.playingSince = &cue, cancel, now
	go func() {
		defer cancel()
		rejected := false
		err := p.voice.PlayPrepared(play, cfg.Style, cfg.Volume, texts, func(length time.Duration) bool {
			p.mu.Lock()
			defer p.mu.Unlock()
			now := time.Now()
			id, slot := cue.SignalKey()
			if !p.monitor.CanStart(cue, now, length) || !p.directionAllows(id, slot, now) || play.Err() != nil {
				rejected = true
				return false
			}
			p.monitor.Claim(cue, now)
			return true
		})
		p.mu.Lock()
		switch {
		case play.Err() != nil:
			p.notePICSSpeech("cancelled")
		case rejected:
			p.notePICSSpeech("no-longer-valid")
		case err != nil:
			p.notePICSSpeech("playback-unavailable")
		default:
			p.notePICSSpeech("played-" + cue.Kind)
		}
		if err != nil && play.Err() == nil && !rejected {
			slog.Info("PICS speech failed", "reason", err.Error())
		}
		if p.playToken == token {
			p.playing, p.playCancel = nil, nil
		}
		p.mu.Unlock()
	}()
}

// picsPlayLimit frees the speaker slot if one announcement never finishes:
// the longest wait for the speaker plus the longest clip set and timeouts.
const picsPlayLimit = 60 * time.Second

// Called under p.mu. No location, device address, or crossing identifier enters the journal.
func (p *picsService) notePICSSpeech(decision string) {
	if decision != p.speechDecision {
		p.speechDecision = decision
		slog.Info("PICS speech", "decision", decision)
		select {
		case p.speechLog <- picsSpeechEvent{At: time.Now().UTC(), Decision: decision}:
		default:
		}
	}
}

type picsSpeechEvent struct {
	At       time.Time `json:"at"`
	Decision string    `json:"decision"`
}

// picsSpeechLogLimit bounds the persisted decision log; the journal is
// volatile on the device, so this keeps the last few days across reboots.
const picsSpeechLogLimit = 256 << 10

func (p *picsService) writeSpeechLog(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-p.speechLog:
			line, _ := json.Marshal(event)
			if info, err := os.Stat(p.logPath); err == nil && info.Size()+int64(len(line))+1 > picsSpeechLogLimit {
				_ = os.Rename(p.logPath, p.logPath+".1")
			}
			if err := os.MkdirAll(filepath.Dir(p.logPath), 0o700); err != nil {
				continue
			}
			f, err := os.OpenFile(p.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
			if err != nil {
				continue
			}
			_, _ = f.Write(append(line, '\n'))
			_ = f.Close()
		}
	}
}

func (s *Server) registerPICS() {
	p := s.pics
	s.mux.HandleFunc("GET /api/v1/pics/history", s.picsHistory)
	s.mux.HandleFunc("GET /api/v1/pics/directions", s.picsDirectionRules)
	s.mux.HandleFunc("PUT /api/v1/pics/directions/{id}", s.savePICSDirectionRule)
	s.mux.HandleFunc("GET /api/v1/pics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		p.mu.Lock()
		now := time.Now()
		signals := p.monitor.Signals(now)
		if p.settings.Directional {
			for i := range signals {
				signals[i].DirectionMatch = p.directionAllows(signals[i].ID, signals[i].Slot, now)
			}
		}
		out := struct {
			Settings    picsSettings              `json:"settings"`
			State       string                    `json:"state"`
			Reason      string                    `json:"reason,omitempty"`
			Course      *float64                  `json:"course,omitempty"`
			Signals     []pics.Signal             `json:"signals"`
			Stale       bool                      `json:"stale,omitempty"`
			Diagnostics pics.ReceptionDiagnostics `json:"diagnostics"`
		}{p.settings, p.scanState, p.scanReason, p.currentPICSCourse(now), signals, p.scanState == "scanning" && len(signals) == 0 && p.monitor.HasSeenSignal(), p.monitor.Diagnostics()}
		p.mu.Unlock()
		if !out.Settings.Enabled {
			out.State, out.Reason = "off", ""
		}
		jsonOut(w, http.StatusOK, out)
	})
	s.mux.HandleFunc("GET /api/v1/pics/speech", func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		cfg, reason, prepared, decision := p.settings, p.speechReason, p.speechPrepared, p.speechDecision
		p.mu.Unlock()
		ready := p.voice.Prepared(cfg.Style, pics.PreviewText)
		if err := p.voice.HardwareAvailable(); err != nil {
			ready, reason = false, err.Error()
		}
		styles := p.voice.CachedStyles()
		credit := "VOICEVOX"
		if cfg.Style == 1 {
			credit = "VOICEVOX:ずんだもん"
		}
		for _, style := range styles {
			if style.ID == cfg.Style {
				credit = "VOICEVOX:" + style.Speaker
				break
			}
		}
		jsonOut(w, http.StatusOK, map[string]any{"ready": ready, "prepared": prepared, "reason": reason, "styles": styles, "credit": credit, "lastDecision": decision})
	})
	s.mux.HandleFunc("PUT /api/v1/pics", func(w http.ResponseWriter, r *http.Request) {
		var body *picsSettings
		if !decode(w, r, &body) {
			return
		}
		if body == nil {
			fail(w, http.StatusBadRequest, "INVALID_PICS_SETTINGS", "高度化PICSの設定を指定してください")
			return
		}
		cfg := *body
		if err := cfg.validate(); err != nil {
			fail(w, http.StatusBadRequest, "INVALID_PICS_SETTINGS", err.Error())
			return
		}
		if err := p.save(cfg); err != nil {
			fail(w, http.StatusInternalServerError, "PICS_SAVE_FAILED", "高度化PICSの設定を保存できません")
			return
		}
		s.audit("pics.settings", "saved")
		jsonOut(w, http.StatusOK, cfg)
	})
	s.mux.HandleFunc("POST /api/v1/pics/speech/preview", func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		cfg := p.settings
		p.mu.Unlock()
		if !cfg.Enabled || !cfg.Speech || cfg.Volume == 0 {
			fail(w, http.StatusConflict, "PICS_SPEECH_OFF", "発話と再生音量を有効にしてください")
			return
		}
		if err := p.voice.PlayPrepared(r.Context(), cfg.Style, cfg.Volume, []string{pics.PreviewText}, func(_ time.Duration) bool {
			p.mu.Lock()
			defer p.mu.Unlock()
			return cfg == p.settings
		}); err != nil {
			fail(w, http.StatusConflict, "PICS_SPEECH_PREVIEW_FAILED", err.Error())
			return
		}
		jsonOut(w, http.StatusOK, map[string]bool{"played": true})
	})
}
