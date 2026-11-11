package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/np1-control-center/ncc/internal/config"
	"github.com/np1-control-center/ncc/internal/model"
	"github.com/np1-control-center/ncc/internal/radio"
)

type LoggerState struct {
	Enabled        bool      `json:"enabled"`
	Active         bool      `json:"active"`
	Stored         uint64    `json:"stored"`
	Located        uint64    `json:"located"`
	LastObservedAt time.Time `json:"lastObservedAt"`
	LastStoredAt   time.Time `json:"lastStoredAt"`
	Error          string    `json:"error,omitempty"`
}

func (s *Server) loadLogger() {
	if bytes, e := os.ReadFile(filepath.Join(s.cfg.StateDir, "radio-logger.json")); e == nil && len(bytes) <= 2048 {
		_ = json.Unmarshal(bytes, &s.logger)
	}
	// The owner explicitly requested continuous logging without a consent gate.
	s.logger.Enabled = true
	s.logger.Active = false
}
func (s *Server) loggerStatus(w http.ResponseWriter, r *http.Request) {
	s.loggerMu.Lock()
	defer s.loggerMu.Unlock()
	jsonOut(w, 200, model.Value("continuous observation logger", s.logger))
}
func (s *Server) storeRadio(sample radio.Observation, now time.Time) error {
	s.loggerMu.Lock()
	defer s.loggerMu.Unlock()
	if sample.At.IsZero() || sample.Source == "" || sample.At.After(now.Add(2*time.Second)) || now.Sub(sample.At) > 2*time.Minute {
		return nil
	}
	if !sample.At.After(s.logger.LastObservedAt) {
		return nil
	}
	if e := s.radioStore.Append(sample); e != nil {
		s.logger.Error = "storage-unavailable"
		return e
	}
	s.logger.LastObservedAt = sample.At
	s.logger.LastStoredAt = now
	s.logger.Stored++
	s.logger.Error = ""
	if sample.GPS != nil && sample.GPS.At.Sub(sample.At).Abs() <= 5*time.Second {
		s.logger.Located++
	}
	return config.Atomic(filepath.Join(s.cfg.StateDir, "radio-logger.json"), s.logger)
}
func (s *Server) collectRadio(ctx context.Context) {
	s.loggerMu.Lock()
	s.logger.Enabled = true
	s.logger.Active = true
	s.loggerMu.Unlock()
	defer func() { s.loggerMu.Lock(); s.logger.Active = false; s.loggerMu.Unlock() }()
	collect := func() {
		request, e := http.NewRequestWithContext(ctx, "GET", "http://broker/radio-sample", nil)
		if e != nil {
			return
		}
		response, e := s.client.Do(request)
		if e != nil {
			return
		}
		defer response.Body.Close()
		var result struct {
			Status string            `json:"status"`
			Data   radio.Observation `json:"data"`
		}
		if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 16*1024)).Decode(&result) != nil || result.Status != "available" {
			return
		}
		_ = s.storeRadio(result.Data, time.Now().UTC())
	}
	collect()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			collect()
		}
	}
}
