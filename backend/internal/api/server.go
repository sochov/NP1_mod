package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/auth"
	"github.com/np1-control-center/ncc/internal/broker"
	"github.com/np1-control-center/ncc/internal/config"
	"github.com/np1-control-center/ncc/internal/radio"
	"github.com/np1-control-center/ncc/internal/store"
	"github.com/np1-control-center/ncc/internal/webui"
)

type Settings struct {
	Revision           int      `json:"revision"`
	Theme              string   `json:"theme"`
	Locale             string   `json:"locale"`
	Favorites          []string `json:"favorites"`
	CellularLimitBytes string   `json:"cellularLimitBytes"`
}
type cameraSession struct {
	Source, Owner, Color string
	Expires              time.Time
}
type Server struct {
	cfg      config.Config
	auth     *auth.Manager
	client   *http.Client
	mux      *http.ServeMux
	mu       sync.Mutex
	settings Settings
	cameras  map[string]cameraSession
	auditMu  sync.Mutex
	traffic  *store.Traffic
	wan      string
}

func New(c config.Config) *Server {
	s := &Server{cfg: c, auth: auth.New(c), client: broker.Client(c.Broker), mux: http.NewServeMux(), settings: Settings{1, "system", "ja", []string{"system", "network", "storage"}, "0"}, cameras: map[string]cameraSession{}}
	s.traffic = store.NewTraffic(c.StateDir)
	if b, e := os.ReadFile(filepath.Join(c.StateDir, "settings.json")); e == nil {
		_ = json.Unmarshal(b, &s.settings)
	}
	if s.settings.CellularLimitBytes == "" {
		s.settings.CellularLimitBytes = "0"
	}
	s.routes()
	return s
}
func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, code, reason string) {
	jsonOut(w, status, map[string]any{"status": "error", "code": code, "reason": reason})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		fail(w, 400, "INVALID_INPUT", "入力を確認してください")
		return false
	}
	if d.Decode(&struct{}{}) != io.EOF {
		fail(w, 400, "INVALID_INPUT", "JSONは1件のみ指定できます")
		return false
	}
	return true
}
func (s *Server) audit(action, result string) {
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	_ = os.MkdirAll(s.cfg.StateDir, 0700)
	p := filepath.Join(s.cfg.StateDir, "audit.jsonl")
	if st, e := os.Stat(p); e == nil && st.Size() > 512*1024 {
		_ = os.Rename(p, p+".previous")
	}
	f, e := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if e == nil {
		defer f.Close()
		_ = json.NewEncoder(f).Encode(map[string]string{"at": time.Now().UTC().Format(time.RFC3339), "actor": "owner", "action": action, "result": result})
	}
}
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.NetworkTrust {
			host := r.Host
			if h, _, e := net.SplitHostPort(host); e == nil {
				host = h
			}
			allowed := net.ParseIP(host) != nil || host == "localhost"
			for _, h := range s.cfg.TrustedHosts {
				if h == host {
					allowed = true
				}
			}
			if !allowed {
				fail(w, 403, "HOST_DENIED", "管理用IPアドレスで開いてください")
				return
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' blob: data: https://tile.openstreetmap.org; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; media-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				fail(w, 403, "ORIGIN_DENIED", "外部サイトからは操作できません")
				return
			}
			if r.Method != "GET" && r.Method != "HEAD" {
				origin, e := url.Parse(r.Header.Get("Origin"))
				scheme := "http"
				if r.TLS != nil {
					scheme = "https"
				}
				if e != nil || origin.Scheme != scheme || origin.Host != r.Host {
					fail(w, 403, "ORIGIN_DENIED", "同一Originが必要です")
					return
				}
			}
			if r.URL.Path != "/api/v1/auth/login" {
				session, ok := s.auth.Get(r)
				if !ok && !s.cfg.NetworkTrust {
					fail(w, 401, "AUTH_REQUIRED", "ログインしてください")
					return
				}
				if r.Method != "GET" && r.Method != "HEAD" {
					if !ok || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) != 1 {
						fail(w, 403, "CSRF_REQUIRED", "セッションを再確認してください")
						return
					}
					if r.Header.Get("X-Request-ID") == "" || len(r.Header.Get("X-Request-ID")) > 80 {
						fail(w, 400, "REQUEST_ID_REQUIRED", "requestIdが必要です")
						return
					}
				}
			}
		}
		s.mux.ServeHTTP(w, r)
	})
}
func (s *Server) proxy(w http.ResponseWriter, r *http.Request, target string) {
	req, e := http.NewRequestWithContext(r.Context(), "GET", "http://broker"+target, nil)
	if e != nil {
		fail(w, 400, "INVALID_INPUT", "不正なリクエスト")
		return
	}
	for _, k := range []string{"Range", "If-Range", "If-Modified-Since"} {
		if v := r.Header.Get(k); v != "" {
			req.Header.Set(k, v)
		}
	}
	resp, e := s.client.Do(req)
	if e != nil {
		fail(w, 503, "BROKER_UNAVAILABLE", "実機brokerに接続できません")
		return
	}
	defer resp.Body.Close()
	for _, k := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "Content-Disposition"} {
		if v := resp.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
func (s *Server) observe(w http.ResponseWriter, r *http.Request) { s.proxy(w, r, "/observe") }
func (s *Server) observation(key string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, _ := http.NewRequestWithContext(r.Context(), "GET", "http://broker/observe", nil)
		resp, e := s.client.Do(req)
		if e != nil {
			fail(w, 503, "BROKER_UNAVAILABLE", "実機brokerに接続できません")
			return
		}
		defer resp.Body.Close()
		var data map[string]json.RawMessage
		if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&data) != nil {
			fail(w, 503, "OBSERVATION_FAILED", "実機データを取得できません")
			return
		}
		value, ok := data[key]
		if !ok {
			fail(w, 501, "NOT_VERIFIED", "取得経路は未検証です")
			return
		}
		jsonOut(w, 200, value)
	}
}
func (s *Server) routes() {
	s.mux.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.NetworkTrust {
			fail(w, 410, "NETWORK_AUTH_ONLY", "Webパスワードは不要です")
			return
		}
		var v struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if !decode(w, r, &v) {
			return
		}
		if len(v.Password) > 256 {
			fail(w, 400, "INVALID_INPUT", "入力が長すぎます")
			return
		}
		id, session, ok := s.auth.Login(auth.IP(r), v.Username, v.Password)
		if !ok {
			s.audit("login", "denied")
			fail(w, 429, "LOGIN_DENIED", "資格情報を確認してください。失敗が続く場合は5分後に再試行してください")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "ncc_session", Value: id, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 8 * 3600})
		s.audit("login", "accepted")
		jsonOut(w, 200, session)
	})
	s.mux.HandleFunc("GET /api/v1/auth/session", func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.NetworkTrust {
			v, ok := s.auth.Bootstrap(w, r)
			if !ok {
				fail(w, 429, "SESSION_LIMIT", "接続数の上限です")
				return
			}
			jsonOut(w, 200, v)
			return
		}
		v, _ := s.auth.Get(r)
		jsonOut(w, 200, v)
	})
	s.mux.HandleFunc("POST /api/v1/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		s.auth.Revoke(r)
		http.SetCookie(w, &http.Cookie{Name: "ncc_session", Value: "", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		s.audit("logout", "accepted")
		jsonOut(w, 200, map[string]bool{"ok": true})
	})
	s.mux.HandleFunc("GET /api/v1/system/summary", s.summary)
	s.mux.HandleFunc("GET /api/v1/traffic/usage", s.usage)
	for path, key := range map[string]string{"/system/services": "services", "/network/wan": "routes", "/network/modem": "modem", "/network/apn": "modem", "/network/hotspot": "hotspot", "/network/wifi-sta": "wifi", "/network/traffic": "traffic", "/storage/sd": "sd", "/device/usb": "usb", "/device/gps": "gps", "/device/sensors": "temperatures", "/vpn/tailscale/status": "tailscale"} {
		s.mux.HandleFunc("GET /api/v1"+path, s.observation(key))
	}
	s.mux.HandleFunc("GET /api/v1/device/led", func(w http.ResponseWriter, r *http.Request) { s.proxy(w, r, "/led") })
	s.mux.HandleFunc("PUT /api/v1/device/led", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		req, _ := http.NewRequestWithContext(r.Context(), "PUT", "http://broker/led", r.Body)
		resp, e := s.client.Do(req)
		if e != nil {
			fail(w, 503, "LED_UNAVAILABLE", "LED brokerに接続できません")
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			reason, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			fail(w, resp.StatusCode, "LED_REJECTED", strings.TrimSpace(string(reason)))
			s.audit("led.update", "rejected")
			return
		}
		s.audit("led.update", "accepted")
		w.Header().Set("Content-Type", "application/json")
		io.Copy(w, io.LimitReader(resp.Body, 8192))
	})
	s.mux.HandleFunc("POST /api/v1/vpn/tailscale/login", func(w http.ResponseWriter, r *http.Request) {
		v, _ := s.auth.Get(r)
		if v.Role != "admin" {
			fail(w, 403, "PERMISSION_DENIED", "管理者権限が必要です")
			return
		}
		req, _ := http.NewRequestWithContext(r.Context(), "POST", "http://broker/tailscale/login", nil)
		resp, e := s.client.Do(req)
		if e != nil {
			fail(w, 503, "VPN_UNAVAILABLE", "Tailscale状態を確認してください")
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			fail(w, 503, "VPN_UNAVAILABLE", "Tailscale導入とWAN疎通を確認してください")
			return
		}
		s.audit("tailscale.login", "requested")
		w.Header().Set("Content-Type", "application/json")
		io.Copy(w, io.LimitReader(resp.Body, 8192))
	})
	s.mux.HandleFunc("GET /api/v1/capabilities", func(w http.ResponseWriter, r *http.Request) { s.proxy(w, r, "/capabilities") })
	s.mux.HandleFunc("GET /api/v1/storage/files", func(w http.ResponseWriter, r *http.Request) { s.proxy(w, r, "/files?"+r.URL.Query().Encode()) })
	s.mux.HandleFunc("GET /api/v1/storage/file", func(w http.ResponseWriter, r *http.Request) { s.proxy(w, r, "/file?"+r.URL.Query().Encode()) })
	s.mux.HandleFunc("GET /api/v1/recordings", func(w http.ResponseWriter, r *http.Request) { s.proxy(w, r, "/recordings?"+r.URL.Query().Encode()) })
	s.mux.HandleFunc("GET /api/v1/recordings/days", func(w http.ResponseWriter, r *http.Request) { s.proxy(w, r, "/recordings/days") })
	s.mux.HandleFunc("GET /api/v1/recordings/{id}/stream", func(w http.ResponseWriter, r *http.Request) {
		s.proxy(w, r, "/recordings/"+url.PathEscape(r.PathValue("id"))+"/stream")
	})
	s.mux.HandleFunc("GET /api/v1/recordings/{id}/download", func(w http.ResponseWriter, r *http.Request) {
		s.proxy(w, r, "/recordings/"+url.PathEscape(r.PathValue("id"))+"/download")
	})
	s.mux.HandleFunc("GET /api/v1/settings", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		jsonOut(w, 200, s.settings)
	})
	s.mux.HandleFunc("PUT /api/v1/settings", s.saveSettings)
	s.mux.HandleFunc("POST /api/v1/backup/export", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.audit("backup.export", "settings-only")
		w.Header().Set("Content-Disposition", "attachment; filename=ncc-settings.json")
		jsonOut(w, 200, map[string]any{"schemaVersion": 1, "settings": s.settings, "excludes": []string{"credentials", "tls", "tailscale", "position", "media", "vendor-settings"}})
	})
	s.mux.HandleFunc("POST /api/v1/backup/restore", s.restoreSettings)
	s.mux.HandleFunc("GET /api/v1/logs", func(w http.ResponseWriter, r *http.Request) {
		s.auditMu.Lock()
		defer s.auditMu.Unlock()
		b, e := os.ReadFile(filepath.Join(s.cfg.StateDir, "audit.jsonl"))
		if e != nil {
			b = []byte{}
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Write(b)
	})
	s.mux.HandleFunc("POST /api/v1/camera/sessions", func(w http.ResponseWriter, r *http.Request) {
		session, _ := s.auth.Get(r)
		if !auth.Allowed(session.Role, "camera") {
			fail(w, 403, "PERMISSION_DENIED", "カメラ権限が必要です")
			return
		}
		var v struct {
			Source       string `json:"source"`
			Color        string `json:"color"`
			Consent      bool   `json:"consent"`
			CabinConsent bool   `json:"cabinConsent"`
		}
		if !decode(w, r, &v) {
			return
		}
		if (v.Source != "front" && v.Source != "in") || (v.Color != "" && v.Color != "auto" && v.Color != "source") {
			fail(w, 400, "INVALID_CAMERA", "カメラと色調を確認してください")
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		for k, c := range s.cameras {
			if time.Now().After(c.Expires) {
				delete(s.cameras, k)
			}
		}
		if len(s.cameras) >= 4 {
			fail(w, 429, "RESOURCE_BUSY", "配信数の上限です")
			return
		}
		b := make([]byte, 16)
		_, e := rand.Read(b)
		if e != nil {
			fail(w, 500, "RANDOM_FAILED", "セッション生成失敗")
			return
		}
		id := hex.EncodeToString(b)
		if v.Color == "" {
			v.Color = "auto"
		}
		s.cameras[id] = cameraSession{v.Source, session.CSRF, v.Color, time.Now().Add(30 * time.Second)}
		s.audit("camera.start", "started")
		jsonOut(w, 201, map[string]any{"id": id, "idleTimeout": 30, "fps": 1, "width": 640, "height": 360})
	})
	s.mux.HandleFunc("GET /api/v1/camera/sessions/{id}/stream", func(w http.ResponseWriter, r *http.Request) {
		session, _ := s.auth.Get(r)
		s.mu.Lock()
		c, ok := s.cameras[r.PathValue("id")]
		s.mu.Unlock()
		if !ok || c.Owner != session.CSRF || time.Now().After(c.Expires) {
			fail(w, 403, "CAMERA_SESSION_EXPIRED", "配信を再開してください")
			return
		}
		s.mu.Lock()
		if current, exists := s.cameras[r.PathValue("id")]; exists {
			current.Expires = time.Now().Add(30 * time.Second)
			s.cameras[r.PathValue("id")] = current
		}
		s.mu.Unlock()
		s.proxy(w, r, "/snapshot?source="+c.Source+"&color="+c.Color)
	})
	s.mux.HandleFunc("DELETE /api/v1/camera/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		session, _ := s.auth.Get(r)
		s.mu.Lock()
		defer s.mu.Unlock()
		c, ok := s.cameras[r.PathValue("id")]
		if ok && c.Owner == session.CSRF {
			delete(s.cameras, r.PathValue("id"))
		}
		jsonOut(w, 200, map[string]bool{"ok": true})
	})
	s.mux.HandleFunc("GET /api/v1/events", func(w http.ResponseWriter, r *http.Request) {
		f, ok := w.(http.Flusher)
		if !ok {
			fail(w, 500, "STREAM_UNAVAILABLE", "SSE非対応")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Accel-Buffering", "no")
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			if _, ok := s.auth.Get(r); !ok {
				return
			}
			fmt.Fprintf(w, "event: refresh\ndata: {\"observedAt\":%q}\n\n", time.Now().UTC().Format(time.RFC3339))
			f.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-t.C:
			}
		}
	})
	s.mux.HandleFunc("GET /api/v1/radio/history", s.radioHistory)
	s.mux.HandleFunc("GET /api/v1/radio/heatmap/legend", func(w http.ResponseWriter, r *http.Request) {
		v, e := radio.Legend(r.URL.Query().Get("metric"))
		if e != nil {
			fail(w, 400, "INVALID_METRIC", e.Error())
			return
		}
		jsonOut(w, 200, v)
	})
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		fail(w, 501, "NOT_VERIFIED", "この機能は実機制御経路の検証が完了していません。capabilitiesを確認してください")
	})
	files, _ := fs.Sub(webui.Files, "dist")
	s.mux.Handle("/", http.FileServer(http.FS(files)))
}
func validateSettings(v Settings) bool {
	if v.CellularLimitBytes != "" {
		limit, e := strconv.ParseUint(v.CellularLimitBytes, 10, 64)
		if e != nil || limit > 100_000_000_000_000 || strconv.FormatUint(limit, 10) != v.CellularLimitBytes {
			return false
		}
	}
	if v.Theme != "system" && v.Theme != "light" && v.Theme != "dark" {
		return false
	}
	if v.Locale != "ja" && v.Locale != "en" {
		return false
	}
	if len(v.Favorites) > 14 {
		return false
	}
	for _, f := range v.Favorites {
		if len(f) > 32 || strings.ContainsAny(f, "/\\\x00") {
			return false
		}
	}
	return true
}
func (s *Server) commit(w http.ResponseWriter, v Settings, expected int) {
	if v.CellularLimitBytes == "" {
		v.CellularLimitBytes = "0"
	}
	if expected != s.settings.Revision {
		fail(w, 409, "REVISION_CONFLICT", "他の画面で設定が更新されました")
		return
	}
	if !validateSettings(v) {
		fail(w, 400, "INVALID_SETTINGS", "設定値を確認してください")
		return
	}
	v.Revision = s.settings.Revision + 1
	if e := config.Atomic(filepath.Join(s.cfg.StateDir, "settings.json"), v); e != nil {
		fail(w, 500, "STORE_ERROR", "設定の保存に失敗しました")
		return
	}
	s.settings = v
	s.audit("settings.update", "committed")
	jsonOut(w, 200, v)
}
func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	var v struct {
		ExpectedRevision int      `json:"expectedRevision"`
		Settings         Settings `json:"settings"`
	}
	if !decode(w, r, &v) {
		return
	}
	session, _ := s.auth.Get(r)
	if !auth.Allowed(session.Role, "settings") {
		fail(w, 403, "PERMISSION_DENIED", "管理者権限が必要です")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commit(w, v.Settings, v.ExpectedRevision)
}
func (s *Server) restoreSettings(w http.ResponseWriter, r *http.Request) {
	var v struct {
		SchemaVersion    int      `json:"schemaVersion"`
		ExpectedRevision int      `json:"expectedRevision"`
		DryRun           bool     `json:"dryRun"`
		Settings         Settings `json:"settings"`
	}
	if !decode(w, r, &v) {
		return
	}
	if v.SchemaVersion != 1 || !validateSettings(v.Settings) {
		fail(w, 400, "INVALID_BACKUP", "バックアップ形式を確認してください")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if v.ExpectedRevision != s.settings.Revision {
		fail(w, 409, "REVISION_CONFLICT", "設定が更新されています")
		return
	}
	if v.DryRun {
		jsonOut(w, 200, map[string]any{"before": s.settings, "after": v.Settings, "dryRun": true})
		return
	}
	s.commit(w, v.Settings, v.ExpectedRevision)
}
