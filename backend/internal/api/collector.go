package api

import (
	"context"
	"encoding/json"
	"github.com/np1-control-center/ncc/internal/model"
	"github.com/np1-control-center/ncc/internal/store"
	"io"
	"net/http"
	"strings"
	"time"
)

func (s *Server) Close() { _ = s.traffic.Save() }
func (s *Server) Collect(ctx context.Context) {
	go s.collectRadio(ctx)
	go s.journeys.collect(ctx, s.client)
	go s.plates.collect(ctx)
	go s.collectTailnetHosts(ctx)
	timer := time.NewTicker(2 * time.Second)
	defer timer.Stop()
	sample := func() {
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://broker/counters", nil)
		resp, e := s.client.Do(req)
		if e != nil {
			return
		}
		defer resp.Body.Close()
		var m struct {
			Traffic    model.Observation `json:"traffic"`
			BootID     string            `json:"bootID"`
			Interfaces map[string]string `json:"interfaces"`
			WAN        string            `json:"wan"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&m) != nil {
			return
		}
		traffic := m.Traffic
		if traffic.Status != "available" {
			return
		}
		b, _ := json.Marshal(traffic.Data)
		var items []map[string]string
		if json.Unmarshal(b, &items) != nil {
			return
		}
		s.mu.Lock()
		s.wan = m.WAN
		s.mu.Unlock()
		s.traffic.FeedNetworks(traffic.ObservedAt, m.BootID, items, store.NetworkInterfaces{Cellular: m.Interfaces["cellular"], WiFi: m.Interfaces["wifi"]})
	}
	sample()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			sample()
		}
	}
}
func activeWAN(data any) string {
	routes, ok := data.(string)
	if !ok {
		return ""
	}
	for _, line := range strings.Split(routes, "\n") {
		f := strings.Fields(line)
		if len(f) > 0 && f[0] == "default" {
			for i, v := range f {
				if v == "dev" && i+1 < len(f) {
					return f[i+1]
				}
			}
		}
	}
	return ""
}
func (s *Server) usage(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	wan := s.wan
	s.mu.Unlock()
	value := s.traffic.Summary(wan, time.Now())
	if value == nil {
		jsonOut(w, 200, model.Missing("NCC traffic collector", "通信量の記録を開始しています"))
		return
	}
	result := model.Value("NCC traffic collector (2-second deltas)", value)
	result.RefreshSeconds = 2
	if value["stale"] == true {
		result.Status = "stale"
	}
	jsonOut(w, 200, result)
}
func (s *Server) summary(w http.ResponseWriter, r *http.Request) {
	req, _ := http.NewRequestWithContext(r.Context(), "GET", "http://broker/observe", nil)
	resp, e := s.client.Do(req)
	if e != nil {
		fail(w, 503, "BROKER_UNAVAILABLE", "本体に接続できません")
		return
	}
	defer resp.Body.Close()
	var m map[string]model.Observation
	if json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&m) != nil {
		fail(w, 503, "OBSERVATION_FAILED", "状態を取得できません")
		return
	}
	value := s.traffic.Summary(activeWAN(m["routes"].Data), time.Now())
	if value == nil {
		m["usage"] = model.Missing("NCC traffic collector", "通信量の記録を開始しています")
	} else {
		m["usage"] = model.Value("NCC traffic collector", value)
		if value["stale"] == true {
			o := m["usage"]
			o.Status = "stale"
			m["usage"] = o
		}
	}
	m["odometer"] = model.Missing("GNSS odometer", "走行データをまだ受信していません")
	jsonOut(w, 200, m)
}
