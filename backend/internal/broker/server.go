package broker

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/np1-control-center/ncc/internal/adapters"
	"github.com/np1-control-center/ncc/internal/audio"
	"github.com/np1-control-center/ncc/internal/led"
	"github.com/np1-control-center/ncc/internal/model"
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	modemObserver := adapters.NewModemObserver(ctx)
	mux := http.NewServeMux()
	adapters.RegisterNASBackupHandlers(mux)
	go adapters.RunNASBackupScheduler(ctx)
	defer func() {
		shutdown, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = adapters.ShutdownNASBackup(shutdown)
	}()
	adapters.RegisterStockServiceHandlers(mux, observer.Invalidate)
	audioOutput := audio.New(nil)
	defer audioOutput.Close()
	audioOutput.Register(mux)
	if e := adapters.RegisterScreenLEDHandlers(mux); e != nil {
		return e
	}
	mux.HandleFunc("GET /journey-intervals", adapters.HandleJourneyIntervals)
	mux.HandleFunc("GET /alexa", adapters.HandleAlexaRemote)
	mux.HandleFunc("PUT /tailscale/name", func(w http.ResponseWriter, r *http.Request) {
		defer observer.Invalidate()
		adapters.HandleTailscaleName(w, r)
	})
	mux.HandleFunc("PUT /alexa", adapters.HandleAlexaRemote)
	mux.HandleFunc("GET /device/ota-policy", adapters.HandleOTAPolicy)
	mux.HandleFunc("PUT /device/ota-policy", adapters.HandleOTAPolicy)
	adapters.RegisterWiFiHandlers(mux)
	// Recovery owns its own rollback context and does not wait for a browser visit.
	go func() { _ = adapters.RecoverWiFi(ctx) }()
	mux.HandleFunc("GET /apn", adapters.HandleAPNSettings)
	mux.HandleFunc("PUT /apn", adapters.HandleSaveAPNSettings)
	mux.HandleFunc("POST /apn/apply", adapters.HandleAPNApplyRemote)
	mux.HandleFunc("GET /apn/jobs/{id}", adapters.HandleAPNApplyRemote)
	mux.HandleFunc("GET /radio-sample", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(adapters.RadioSample(r.Context(), modemObserver.Get()))
	})
	mux.HandleFunc("GET /native-settings", adapters.HandleNativeSettings)
	mux.HandleFunc("GET /native-audio", adapters.HandleNativeAudioRemote)
	mux.HandleFunc("PUT /native-audio/{id}", adapters.HandleNativeAudioRemote)
	mux.HandleFunc("GET /trips", adapters.HandleTrips)
	mux.HandleFunc("PUT /native-settings/{id}", adapters.HandleNativeSetting)
	mux.HandleFunc("GET /counters", func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(adapters.TrafficSnapshot()) })
	var cameraMu sync.Mutex
	mux.HandleFunc("GET /led", func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(adapters.LEDStatus()) })
	mux.HandleFunc("PUT /led", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			ExpectedRevision int          `json:"expectedRevision"`
			Settings         led.Settings `json:"settings"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		d.DisallowUnknownFields()
		if e := d.Decode(&v); e != nil {
			http.Error(w, "invalid LED request", 400)
			return
		}
		result, e := adapters.ApplyLED(v.ExpectedRevision, v.Settings)
		if e != nil {
			status, code, reason := adapters.LEDErrorStatus(e)
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "code": code, "reason": reason})
			return
		}
		json.NewEncoder(w).Encode(result)
	})
	mux.HandleFunc("GET /observe", func(w http.ResponseWriter, r *http.Request) {
		values := observer.Get()
		result := make(map[string]model.Observation, len(values)+2)
		for k, v := range values {
			result[k] = v
		}
		result["tripStats"] = adapters.TripStats(r.Context())
		result["gps"] = adapters.GNSSSummary(r.Context())
		result["modem"] = modemObserver.Get()
		result["recording"] = adapters.RecordingSummary(r.Context())
		result["hotspot"] = adapters.APStatus()
		json.NewEncoder(w).Encode(result)
	})
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
	mux.HandleFunc("GET /recordings", adapters.HandleRecordings)
	mux.HandleFunc("GET /recordings/track", adapters.HandleRecordingTrack)
	mux.HandleFunc("GET /camera/h264", adapters.HandleCameraH264)
	mux.HandleFunc("GET /recordings/days", adapters.HandleRecordingDays)
	mux.HandleFunc("GET /recordings/{id}/stream", adapters.HandleRecordingStream)
	mux.HandleFunc("GET /recordings/{id}/download", adapters.HandleRecordingDownload)
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
