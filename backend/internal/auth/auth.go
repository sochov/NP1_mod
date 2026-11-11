package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/config"
)

type Session struct {
	CSRF    string    `json:"csrf"`
	Role    string    `json:"role"`
	Expires time.Time `json:"expires"`
}
type attempt struct {
	Count int
	Reset time.Time
}
type Manager struct {
	mu       sync.Mutex
	sessions map[string]Session
	attempts map[string]attempt
	config   config.Config
}

func New(c config.Config) *Manager {
	return &Manager{sessions: map[string]Session{}, attempts: map[string]attempt{}, config: c}
}
func token() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func (m *Manager) Login(ip, user, password string) (string, Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for k, v := range m.attempts {
		if now.After(v.Reset) {
			delete(m.attempts, k)
		}
	}
	for k, v := range m.sessions {
		if now.After(v.Expires) {
			delete(m.sessions, k)
		}
	}
	a := m.attempts[ip]
	if a.Count >= 5 && now.Before(a.Reset) {
		return "", Session{}, false
	}
	if len(m.attempts) >= 1024 {
		return "", Session{}, false
	}
	a.Count++
	a.Reset = now.Add(5 * time.Minute)
	m.attempts[ip] = a
	h := config.Hash(password, m.config.Salt)
	if subtle.ConstantTimeCompare(h, m.config.PasswordHash) != 1 || user != m.config.Username {
		return "", Session{}, false
	}
	if len(m.sessions) >= 64 {
		return "", Session{}, false
	}
	delete(m.attempts, ip)
	id := token()
	s := Session{token(), "admin", now.Add(8 * time.Hour)}
	m.sessions[id] = s
	return id, s, true
}
func (m *Manager) Get(r *http.Request) (Session, bool) {
	c, e := r.Cookie(CookieName(r))
	if e != nil {
		return Session{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[c.Value]
	if !ok || time.Now().After(s.Expires) {
		delete(m.sessions, c.Value)
		return Session{}, false
	}
	return s, true
}
func (m *Manager) Revoke(r *http.Request) {
	c, e := r.Cookie(CookieName(r))
	if e == nil {
		m.mu.Lock()
		delete(m.sessions, c.Value)
		m.mu.Unlock()
	}
}
func CookieName(r *http.Request) string {
	if r.TLS == nil {
		return "ncc_http_session"
	}
	return "ncc_session"
}
func (m *Manager) Bootstrap(w http.ResponseWriter, r *http.Request) (Session, bool) {
	if s, ok := m.Get(r); ok {
		return s, true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for k, s := range m.sessions {
		if now.After(s.Expires) {
			delete(m.sessions, k)
		}
	}
	if len(m.sessions) >= 256 {
		return Session{}, false
	}
	id := token()
	s := Session{token(), "admin", now.Add(8 * time.Hour)}
	m.sessions[id] = s
	http.SetCookie(w, &http.Cookie{Name: CookieName(r), Value: id, Path: "/", Secure: r.TLS != nil, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 8 * 3600})
	return s, true
}
func IP(r *http.Request) string { h, _, _ := net.SplitHostPort(r.RemoteAddr); return h }
func Allowed(role, action string) bool {
	if role == "admin" {
		return true
	}
	if action == "read" {
		return role == "viewer" || role == "operator"
	}
	return role == "operator" && action == "camera"
}
