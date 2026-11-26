// Package androidauto runs NP1 as the phone side of Android Auto: it renders
// the landscape dashboard of the Android app and sends it to a head unit as a
// hardware-encoded H.264 projection.
//
// It runs as its own service (np1-control-android-auto.service). NP1CC only
// writes its settings file and reads its status socket.
package androidauto

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/np1-control-center/ncc/internal/androidauto/aap"
	"github.com/np1-control-center/ncc/internal/androidauto/render"
	"github.com/np1-control-center/ncc/internal/androidauto/stockbt"
)

// Socket is the status socket NP1CC reads.
const Socket = "/run/np1-control-android-auto/aa.sock"

// DataDir holds the certificate and the map tile cache.
const DataDir = "/overlay/owner-apps/np1-control/androidauto"

// DefaultPort is the port head unit apps probe for a phone's projection server.
const DefaultPort = 5277

// FrontExport is the stock front-camera NV12 export (1920x1080 + 16-byte stamp).
const FrontExport = "/tmp/application/imagerecog/imagedata/frontcamera/imagedata/value"

// Settings is stateDir/androidauto/settings.json, written by NP1CC.
type Settings struct {
	Enabled       bool   `json:"enabled"`
	MaxResolution string `json:"maxResolution"`
	FPS           int    `json:"fps"`
	BitrateKbps   int    `json:"bitrateKbps"`
	Theme         string `json:"theme"`
	MapZoom       int    `json:"mapZoom"`
	// Bluetooth connects paired head units over Bluetooth (wireless Android Auto).
	Bluetooth bool `json:"bluetooth"`
	Revision  int  `json:"revision"`
}

// DefaultSettings are used until the owner saves settings.
func DefaultSettings() Settings {
	return Settings{MaxResolution: "800x480", FPS: 15, BitrateKbps: 2500, Theme: "auto", MapZoom: 16}
}

// Normalize clamps every field to what the service supports.
func (s Settings) Normalize() Settings {
	d := DefaultSettings()
	switch s.MaxResolution {
	case "800x480", "1280x720", "1920x1080":
	default:
		s.MaxResolution = d.MaxResolution
	}
	switch s.FPS {
	case 10, 15, 20, 30:
	default:
		s.FPS = d.FPS
	}
	s.BitrateKbps = max(500, min(s.BitrateKbps, 12000))
	switch s.Theme {
	case "auto", "light", "dark":
	default:
		s.Theme = d.Theme
	}
	s.MapZoom = max(13, min(s.MapZoom, 18))
	return s
}

func (s Settings) maxSize() (int, int) {
	switch s.MaxResolution {
	case "1920x1080":
		return 1920, 1080
	case "1280x720":
		return 1280, 720
	}
	return 800, 480
}

// ReadSettings reads the settings file, falling back to defaults.
func ReadSettings(stateDir string) Settings {
	s := DefaultSettings()
	if b, e := os.ReadFile(filepath.Join(stateDir, "androidauto", "settings.json")); e == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s.Normalize()
}

// Status is served on the socket.
type Status struct {
	State       string           `json:"state"` // disabled, waiting, connecting, projecting, background, error
	Since       time.Time        `json:"since"`
	Listen      string           `json:"listen,omitempty"`
	Reason      string           `json:"reason,omitempty"`
	LastError   string           `json:"lastError,omitempty"`
	LastErrorAt *time.Time       `json:"lastErrorAt,omitempty"`
	Certificate string           `json:"certificate"` // self-signed or owner
	HeadUnit    *aap.HeadUnit    `json:"headUnit,omitempty"`
	Video       *aap.VideoConfig `json:"video,omitempty"`
	Stream      *StreamStats     `json:"stream,omitempty"`
	Sessions    int              `json:"sessions"`
	Peer        string           `json:"peer,omitempty"`
	Settings    Settings         `json:"settings"`
	Bluetooth   BTStatus         `json:"bluetooth"`
}

// StreamStats describes the running stream.
type StreamStats struct {
	FPS        float64 `json:"fps"`
	Kbps       float64 `json:"kbps"`
	Frames     uint64  `json:"frames"`
	Dropped    uint64  `json:"dropped"`
	RenderMs   float64 `json:"renderMs"`
	EncodeMs   float64 `json:"encodeMs"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	Acks       uint64  `json:"acks"`
	Stalls     uint64  `json:"stalls"`
	Touches    uint64  `json:"touches"`
	MaxUnacked int     `json:"maxUnacked"`
}

// Config wires the service.
type Config struct {
	StateDir   string // NP1CC state: settings.json is read from StateDir/androidauto
	DataDir    string // this service's own files: certificate and map tile cache
	Socket     string
	APIBase    string // NP1CC API, http://127.0.0.1/api/v1
	Encoder    string // gst-launch-1.0
	CameraPath string
	// Accept decides whether a connecting address may project (the hotspot
	// and loopback by default).
	Accept func(net.Addr) bool
	// JoinWiFi joins a head unit's WiFi for a Bluetooth projection and returns
	// NP1's address there and a function that leaves it. Nil: not available.
	JoinWiFi func(context.Context, aap.WifiCredentials) (string, func(), error)
	Logf     func(string, ...any)
}

// Service is the projection service.
type Service struct {
	cfg   Config
	tiles *render.Tiles

	mu       sync.Mutex
	status   Status
	settings Settings
	active   atomic.Bool
	reload   chan struct{}
	tlsCfg   *tls.Config
	sessions sync.WaitGroup
	bt       *btManager
}

// New prepares the service.
func New(cfg Config) *Service {
	if cfg.Socket == "" {
		cfg.Socket = Socket
	}
	if cfg.APIBase == "" {
		cfg.APIBase = "http://127.0.0.1/api/v1"
	}
	if cfg.CameraPath == "" {
		cfg.CameraPath = FrontExport
	}
	if cfg.Accept == nil {
		cfg.Accept = DefaultAccept
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	if cfg.DataDir == "" {
		cfg.DataDir = DataDir
	}
	s := &Service{cfg: cfg, reload: make(chan struct{}, 1)}
	s.tiles = &render.Tiles{Dir: filepath.Join(cfg.DataDir, "tiles"), DiskLimit: 32 << 20}
	s.status = Status{State: "disabled", Since: time.Now(), Certificate: "self-signed"}
	s.bt = newBTManager(s)
	return s
}

// DefaultAccept admits the NP1 hotspot subnet and loopback (adb forwarding).
func DefaultAccept(a net.Addr) bool {
	t, ok := a.(*net.TCPAddr)
	if !ok {
		return false
	}
	if t.IP.IsLoopback() {
		return true
	}
	_, hotspot, _ := net.ParseCIDR("192.168.226.0/24")
	return hotspot.Contains(t.IP)
}

func (s *Service) setState(state, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status.State != state {
		s.status.Since = time.Now()
	}
	s.status.State, s.status.Reason = state, reason
}

func (s *Service) setError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.status.LastError, s.status.LastErrorAt = err.Error(), &now
	s.cfg.Logf("android auto: %v", err)
}

// Run serves until ctx ends.
func (s *Service) Run(ctx context.Context) error {
	certDir := filepath.Join(s.cfg.DataDir, "cert")
	cert, owner, err := aap.LoadOrCreateCert(certDir)
	if err != nil {
		return fmt.Errorf("android auto certificate: %w", err)
	}
	if owner {
		s.status.Certificate = "owner"
	}
	s.mu.Lock()
	s.tlsCfg = aap.ServerTLSConfig(cert)
	s.mu.Unlock()
	go s.serveSocket(ctx)
	btDone := make(chan struct{})
	go func() { defer close(btDone); s.bt.run(ctx) }()
	defer func() { <-btDone }()
	var ln net.Listener
	lnStop := func() {}
	stopListen := func() {
		if ln != nil {
			lnStop()
			ln.Close()
			ln = nil
		}
	}
	defer stopListen()
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		st := ReadSettings(s.cfg.StateDir)
		s.mu.Lock()
		s.settings, s.status.Settings = st, st
		s.mu.Unlock()
		if st.Enabled && ln == nil {
			l, err := net.Listen("tcp", fmt.Sprintf(":%d", DefaultPort))
			if err != nil {
				s.setError(err)
				s.setState("error", "待ち受けを開始できません")
			} else {
				ln = l
				lctx, cancel := context.WithCancel(ctx)
				lnStop = cancel
				s.mu.Lock()
				s.status.Listen = fmt.Sprintf("192.168.226.1:%d", DefaultPort)
				s.mu.Unlock()
				s.setState("waiting", "ヘッドユニットの接続を待っています")
				go s.acceptLoop(lctx, l)
			}
		} else if !st.Enabled && ln != nil {
			stopListen()
			s.mu.Lock()
			s.status.Listen = ""
			s.mu.Unlock()
		}
		if !st.Enabled && !s.active.Load() {
			s.setState("disabled", "")
		}
		select {
		case <-ctx.Done():
			stopListen()
			// Let a running session close its encoder cleanly.
			done := make(chan struct{})
			go func() { s.sessions.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(8 * time.Second):
			}
			return nil
		case <-tick.C:
		case <-s.reload:
		}
	}
}

func (s *Service) acceptLoop(ctx context.Context, l net.Listener) {
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		if !s.cfg.Accept(c.RemoteAddr()) {
			c.Close()
			continue
		}
		if !s.active.CompareAndSwap(false, true) {
			// One head unit at a time.
			c.Close()
			continue
		}
		s.sessions.Add(1)
		go func() {
			defer s.sessions.Done()
			defer s.active.Store(false)
			s.session(ctx, c)
		}()
	}
}

func (s *Service) session(ctx context.Context, c net.Conn) {
	defer c.Close()
	if t, ok := c.(*net.TCPConn); ok {
		_ = t.SetNoDelay(true)
	}
	s.mu.Lock()
	st := s.settings
	s.status.Sessions++
	s.status.Peer = c.RemoteAddr().String()
	s.status.HeadUnit, s.status.Video, s.status.Stream = nil, nil, nil
	certCfg := s.tlsCfg
	s.mu.Unlock()
	s.setState("connecting", "ヘッドユニットと認証しています")
	w, h := st.maxSize()
	var touches atomic.Uint64
	sess, err := aap.Connect(ctx, c, aap.Options{
		TLS: certCfg, PhoneName: "NP1", PhoneBrand: "NP1 Control Center", MaxWidth: w, MaxHeight: h,
		Logf:    s.cfg.Logf,
		OnTouch: func(aap.Touch) { touches.Add(1) },
	})
	if err != nil {
		s.setError(err)
		s.setState(s.idleState(), "接続できませんでした")
		return
	}
	hu, chosen := sess.HU, sess.Chosen
	s.mu.Lock()
	s.status.HeadUnit, s.status.Video = &hu, &chosen
	s.mu.Unlock()
	s.cfg.Logf("android auto: projecting %dx%d@%d to %s %s", chosen.Width, chosen.Height, chosen.FPS, hu.Make, hu.Model)
	err = s.project(ctx, sess, st)
	sess.Close()
	if err != nil && !errors.Is(err, context.Canceled) {
		s.setError(err)
	}
	s.mu.Lock()
	s.status.Peer = ""
	s.mu.Unlock()
	s.setState(s.idleState(), "接続が終了しました")
}

func (s *Service) idleState() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settings.Enabled {
		return "waiting"
	}
	return "disabled"
}

func (s *Service) serveSocket(ctx context.Context) {
	if err := os.MkdirAll(filepath.Dir(s.cfg.Socket), 0750); err != nil {
		s.setError(err)
		return
	}
	_ = os.Remove(s.cfg.Socket)
	l, err := net.Listen("unix", s.cfg.Socket)
	if err != nil {
		s.setError(err)
		return
	}
	_ = os.Chmod(s.cfg.Socket, 0660)
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() { <-ctx.Done(); srv.Close() }()
	_ = srv.Serve(l)
}

// Handler serves GET /status and POST /reload.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		st := s.status
		s.mu.Unlock()
		st.Bluetooth = s.bt.status()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(st)
	})
	mux.HandleFunc("POST /bt/pairing", func(w http.ResponseWriter, r *http.Request) {
		on := true
		s.btReply(w, s.bt.command(r.Context(), btCommand{pair: &on}))
	})
	mux.HandleFunc("DELETE /bt/pairing", func(w http.ResponseWriter, r *http.Request) {
		off := false
		s.btReply(w, s.bt.command(r.Context(), btCommand{pair: &off}))
	})
	mux.HandleFunc("DELETE /bt/headunits/{addr}", func(w http.ResponseWriter, r *http.Request) {
		a, err := stockbt.ParseAddr(r.PathValue("addr"))
		if err != nil {
			s.btReply(w, errors.New("アドレスの形式が正しくありません"))
			return
		}
		s.btReply(w, s.bt.command(r.Context(), btCommand{remove: a.String()}))
	})
	mux.HandleFunc("POST /reload", func(w http.ResponseWriter, r *http.Request) {
		select {
		case s.reload <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

// btReply answers a Bluetooth command: 204, or 409 with the reason.
func (s *Service) btReply(w http.ResponseWriter, err error) {
	if err == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// isClosed reports a network error from a closed connection.
func isClosed(err error) bool {
	return errors.Is(err, net.ErrClosed) || errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET)
}
