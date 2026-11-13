// Package platesensor is the plate-recognition sensor process. It reads the
// stock front-camera export, passes frames to an inference helper, joins
// each plate with the stock GNSS fix and serves the results on a Unix socket
// to the NCC server. It never opens camera devices or changes stock state.
package platesensor

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

type Config struct {
	StateDir   string // NCC state dir; plate/settings.json gates processing
	ModelDir   string // detector.tflite, ocr.tflite, plate-model.json
	EnginePath string
	Socket     string
	FPS        float64
	Threads    int
}

// Model files the engine needs. Missing files are reported, never faked.
var ModelFiles = []string{"detector.tflite", "ocr.tflite", "plate-model.json"}

const (
	maxQueued      = 2000
	maxBatch       = 500
	frameRingSize  = 24  // ~75 MB of NV12; detections wait up to locateWait for GNSS
	keepFrameConf  = 0.5 // frames are kept only for readings that could become records
	frameTTL       = 90 * time.Second
	locateWait     = 8 * time.Second
	engineTimeout  = 5 * time.Second
	engineStartup  = 30 * time.Second
	settingsPoll   = 5 * time.Second
	restartBackoff = 10 * time.Second
)

// Locator returns the stock GNSS fix for a time, or nil when none is valid.
type Locator func(ctx context.Context, at time.Time) *plate.Location

type EngineStarter func(ctx context.Context) (Engine, error)

type Status struct {
	State       string     `json:"state"`
	Reason      string     `json:"reason,omitempty"`
	Instance    string     `json:"instance"`
	Engine      string     `json:"engine,omitempty"`
	Backend     string     `json:"backend,omitempty"`
	FPS         float64    `json:"fps,omitempty"`
	LastFrameAt *time.Time `json:"lastFrameAt,omitempty"`
}

type pending struct {
	det   plate.Detection
	since time.Time
}

type storedFrame struct {
	data []byte
	at   time.Time
}

type Sensor struct {
	cfg    Config
	source FrameSource
	locate Locator
	start  EngineStarter
	clock  func() time.Time

	mu        sync.Mutex
	status    Status
	queue     []plate.Detection
	seq       uint64
	pending   []pending
	frames    map[string]storedFrame
	order     []string
	frameSeq  uint64
	processed []time.Time
}

func New(cfg Config, source FrameSource, locate Locator, start EngineStarter) *Sensor {
	// The start time makes every process's instance distinct even if two random draws met.
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	binary.BigEndian.PutUint16(b[:2], uint16(time.Now().UnixNano()))
	if cfg.FPS <= 0 {
		cfg.FPS = 4
	}
	return &Sensor{cfg: cfg, source: source, locate: locate, start: start, clock: time.Now,
		status: Status{State: "starting", Instance: hex.EncodeToString(b)}, frames: map[string]storedFrame{}}
}

// SetClock replaces the wall clock (replay of recorded clips uses recording time).
func (s *Sensor) SetClock(clock func() time.Time) { s.clock = clock }

func (s *Sensor) setStatus(state, reason string) {
	s.mu.Lock()
	s.status.State, s.status.Reason = state, reason
	if state != "running" {
		s.status.FPS = 0
	}
	s.mu.Unlock()
}

func (s *Sensor) enabled() bool {
	b, e := os.ReadFile(filepath.Join(s.cfg.StateDir, "plate", "settings.json"))
	if e != nil {
		return false
	}
	var v struct {
		Enabled bool `json:"enabled"`
	}
	return json.Unmarshal(b, &v) == nil && v.Enabled
}

// readiness explains why recognition cannot run, or returns "" when it can.
func (s *Sensor) readiness() (state, reason string) {
	if !s.enabled() {
		return "idle", "記録が停止中のため認識していません"
	}
	if st, e := os.Stat(s.cfg.EnginePath); e != nil || !st.Mode().IsRegular() {
		return "engine-missing", "認識エンジンが導入されていません"
	}
	for _, name := range ModelFiles {
		if st, e := os.Stat(filepath.Join(s.cfg.ModelDir, name)); e != nil || !st.Mode().IsRegular() || st.Size() == 0 {
			return "model-missing", "認識モデルが導入されていません（" + name + "）"
		}
	}
	return "", ""
}

// Run processes frames until ctx ends.
func (s *Sensor) Run(ctx context.Context) {
	var engine Engine
	defer func() {
		if engine != nil {
			engine.Close()
		}
	}()
	buf := make([]byte, FrameBytes+stampBytes)
	interval := time.Duration(float64(time.Second) / s.cfg.FPS)
	var checkedAt, retryAt time.Time
	ready := false
	for ctx.Err() == nil {
		now := s.clock()
		s.flushPending(ctx, false)
		s.expireFrames(now)
		if now.Sub(checkedAt) >= settingsPoll {
			checkedAt = now
			state, reason := s.readiness()
			ready = state == ""
			if !ready {
				if engine != nil {
					engine.Close()
					engine = nil
				}
				s.setStatus(state, reason)
			}
		}
		if ready && engine == nil && !now.Before(retryAt) {
			s.setStatus("starting", "認識エンジンを起動しています")
			e2, e := s.start(ctx)
			if e != nil {
				log.Printf("plate engine start failed: %v", e)
				s.setStatus("error", "認識エンジンを起動できませんでした")
				retryAt = now.Add(restartBackoff)
			} else {
				engine = e2
				info := engine.Info()
				s.mu.Lock()
				s.status.Engine, s.status.Backend = info.Engine, info.Backend
				s.mu.Unlock()
				s.setStatus("running", "")
			}
		}
		if ready && engine != nil {
			if e := s.step(ctx, engine, buf); e != nil {
				log.Printf("plate engine failed: %v", e)
				engine.Close()
				engine = nil
				s.setStatus("error", "認識エンジンが応答しません。再起動します")
				retryAt = s.clock().Add(restartBackoff)
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(interval):
		}
	}
	s.flushPending(context.Background(), true)
}

// step processes one new frame. Frame-source errors are reported in the
// status but do not restart the engine.
func (s *Sensor) step(ctx context.Context, engine Engine, buf []byte) error {
	_, ok, e := s.source.Next(buf)
	if e != nil {
		s.setStatus("running", "カメラ画像を取得できません（純正の映像認識が停止中の可能性があります）")
		return nil
	}
	if !ok {
		return nil
	}
	at := s.clock()
	s.mu.Lock()
	s.frameSeq++
	seq := s.frameSeq
	s.mu.Unlock()
	callCtx, cancel := context.WithTimeout(ctx, engineTimeout)
	reply, e := engine.Process(callCtx, seq, buf, FrameWidth, FrameHeight)
	cancel()
	if e != nil {
		return e
	}
	s.mu.Lock()
	s.status.LastFrameAt = &at
	s.status.Reason = ""
	s.processed = append(s.processed, at)
	for len(s.processed) > 0 && at.Sub(s.processed[0]) > 10*time.Second {
		s.processed = s.processed[1:]
	}
	if n := len(s.processed); n > 1 {
		s.status.FPS = float64(n-1) / at.Sub(s.processed[0]).Seconds()
	}
	s.mu.Unlock()
	if reply.Error != "" {
		log.Printf("plate engine frame error: %s", reply.Error)
		return nil
	}
	if len(reply.Plates) == 0 {
		return nil
	}
	frameID := strconv.FormatUint(seq, 10)
	keep := false
	for _, p := range reply.Plates {
		keep = keep || (p.Reading.Number != "" && p.Reading.Confidence >= keepFrameConf)
	}
	if keep {
		s.keepFrame(frameID, buf[:FrameBytes], at)
	}
	s.mu.Lock()
	for _, p := range reply.Plates {
		d := plate.Detection{At: at, Reading: p.Reading, Box: p.Box}
		if keep {
			d.Frame = frameID
		}
		s.pending = append(s.pending, pending{det: d, since: at})
	}
	s.mu.Unlock()
	return nil
}

func (s *Sensor) keepFrame(id string, data []byte, at time.Time) {
	copyData := append([]byte(nil), data...)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frames[id] = storedFrame{copyData, at}
	s.order = append(s.order, id)
	// Evict the oldest frames, but never one a detection still waiting for
	// its GNSS fix refers to (the server fetches it right after publication).
	pinned := map[string]bool{}
	for _, p := range s.pending {
		pinned[p.det.Frame] = true
	}
	for i := 0; len(s.order) > frameRingSize && i < len(s.order); {
		if pinned[s.order[i]] {
			i++
			continue
		}
		delete(s.frames, s.order[i])
		s.order = append(s.order[:i], s.order[i+1:]...)
	}
}

func (s *Sensor) expireFrames(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.order) > 0 && now.Sub(s.frames[s.order[0]].at) > frameTTL {
		delete(s.frames, s.order[0])
		s.order = s.order[1:]
	}
}

// flushPending publishes detections once a GNSS fix is known or the wait
// expires. The stock NMEA sidecar can lag the camera by a few seconds.
func (s *Sensor) flushPending(ctx context.Context, all bool) {
	s.mu.Lock()
	items := s.pending
	s.pending = nil
	s.mu.Unlock()
	now := s.clock()
	keep := []pending{}
	ready := []plate.Detection{}
	for _, p := range items {
		if s.locate != nil {
			p.det.Location = s.locate(ctx, p.det.At)
		}
		if p.det.Location != nil || all || now.Sub(p.since) >= locateWait {
			ready = append(ready, p.det)
		} else {
			keep = append(keep, p)
		}
	}
	s.mu.Lock()
	s.pending = append(keep, s.pending...)
	for _, d := range ready {
		s.seq++
		d.Seq = s.seq
		s.queue = append(s.queue, d)
	}
	if len(s.queue) > maxQueued {
		s.queue = s.queue[len(s.queue)-maxQueued:]
	}
	s.mu.Unlock()
}

var frameIDPattern = regexp.MustCompile(`^[0-9]{1,20}$`)

func (s *Sensor) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		st := s.status
		s.mu.Unlock()
		writeJSON(w, st)
	})
	mux.HandleFunc("GET /detections", func(w http.ResponseWriter, r *http.Request) {
		after, e := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
		if e != nil && r.URL.Query().Get("after") != "" {
			http.Error(w, "invalid cursor", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		out := []plate.Detection{}
		next := after
		for _, d := range s.queue {
			if d.Seq > after && len(out) < maxBatch {
				out = append(out, d)
				next = d.Seq
			}
		}
		instance, last := s.status.Instance, s.seq
		s.mu.Unlock()
		// last lets a reader notice that this process numbers detections from 1 again,
		// even when its saved cursor is beyond anything this process has produced.
		writeJSON(w, map[string]any{"instance": instance, "next": next, "last": last, "detections": out})
	})
	mux.HandleFunc("GET /frames/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !frameIDPattern.MatchString(id) {
			http.Error(w, "invalid frame", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		f, ok := s.frames[id]
		s.mu.Unlock()
		if !ok {
			http.Error(w, "frame expired", http.StatusNotFound)
			return
		}
		b, e := EncodeJPEG(f.data, FrameWidth, FrameHeight, 85)
		if e != nil {
			http.Error(w, "encode failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(b)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// Serve listens on the Unix socket (group-readable for the NCC server user).
func (s *Sensor) Serve(ctx context.Context) error {
	if e := os.MkdirAll(filepath.Dir(s.cfg.Socket), 0750); e != nil {
		return e
	}
	if e := os.Remove(s.cfg.Socket); e != nil && !errors.Is(e, os.ErrNotExist) {
		return e
	}
	l, e := net.Listen("unix", s.cfg.Socket)
	if e != nil {
		return e
	}
	if e = os.Chmod(s.cfg.Socket, 0660); e != nil {
		l.Close()
		return e
	}
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() { <-ctx.Done(); srv.Close() }()
	if e = srv.Serve(l); errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}
