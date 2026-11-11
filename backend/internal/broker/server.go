package broker

import (
	"encoding/json"
	"errors"
	"github.com/np1-control-center/ncc/internal/adapters"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type listener struct {
	net.Listener
	uid uint32
}

func (l listener) Accept() (net.Conn, error) {
	for {
		c, e := l.Listener.Accept()
		if e != nil {
			return nil, e
		}
		if peerAllowed(c, l.uid) {
			return c, nil
		}
		c.Close()
	}
}
func Serve(socket string, uid uint32) error {
	if os.Geteuid() != 0 {
		return errors.New("broker requires root")
	}
	if e := os.MkdirAll(filepath.Dir(socket), 0750); e != nil {
		return e
	}
	// A systemd-owned runtime directory is required. Refuse to replace anything except a socket.
	if st, e := os.Lstat(socket); e == nil {
		if st.Mode()&os.ModeSocket == 0 {
			return errors.New("socket path occupied")
		}
		if e = os.Remove(socket); e != nil {
			return e
		}
	}
	l, e := net.Listen("unix", socket)
	if e != nil {
		return e
	}
	defer l.Close()
	if e = os.Chown(socket, int(uid), -1); e != nil {
		return e
	}
	if e = os.Chmod(socket, 0600); e != nil {
		return e
	}
	observer := &adapters.Observer{}
	mux := http.NewServeMux()
	var cameraMu sync.Mutex
	mux.HandleFunc("GET /observe", func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(observer.Get()) })
	mux.HandleFunc("GET /capabilities", func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(adapters.Capabilities()) })
	mux.HandleFunc("GET /files", func(w http.ResponseWriter, r *http.Request) {
		v, e := adapters.ListFiles(r.URL.Query().Get("path"))
		if e != nil {
			http.Error(w, "NO_SD or invalid directory", 400)
			return
		}
		json.NewEncoder(w).Encode(v)
	})
	mux.HandleFunc("GET /file", adapters.StreamFile)
	mux.HandleFunc("GET /snapshot", func(w http.ResponseWriter, r *http.Request) {
		if !cameraMu.TryLock() {
			http.Error(w, "RESOURCE_BUSY", 429)
			return
		}
		defer cameraMu.Unlock()
		mode := r.URL.Query().Get("color")
		if mode != "auto" && mode != "source" {
			http.Error(w, "invalid color mode", 400)
			return
		}
		b, e := adapters.Snapshot(r.URL.Query().Get("source"), mode)
		if e != nil {
			http.Error(w, "camera unavailable or stale", 503)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(b)
	})
	mux.HandleFunc("POST /tailscale/login", func(w http.ResponseWriter, r *http.Request) {
		// No command parameters, auth keys, paths or flags are accepted from the worker.
		if r.ContentLength > 0 {
			http.Error(w, "parameters not permitted", 400)
			return
		}
		u, e := adapters.TailscaleLogin()
		if e != nil {
			http.Error(w, "Tailscale login unavailable; check installation and WAN", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"authURL": u})
	})
	s := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 20 * time.Second, WriteTimeout: 120 * time.Second, MaxHeaderBytes: 8192}
	return s.Serve(listener{l, uid})
}
