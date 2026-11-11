package store

import (
	"encoding/json"
	"github.com/np1-control-center/ncc/internal/config"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

type Counter struct {
	RX uint64 `json:"rx,string"`
	TX uint64 `json:"tx,string"`
}
type TrafficState struct {
	StartedAt       time.Time                           `json:"startedAt"`
	SampledAt       time.Time                           `json:"sampledAt"`
	BootID          string                              `json:"bootId"`
	Previous        map[string]Counter                  `json:"previous"`
	Days            map[string]map[string]Counter       `json:"days"`
	NetworkDays     map[string]map[string]NetworkBucket `json:"networkDays,omitempty"`
	NetworkPrevious map[string]NetworkSnapshot          `json:"networkPrevious,omitempty"`
	NetworkStarted  map[string]time.Time                `json:"networkStarted,omitempty"`
	NetworkLast     map[string]time.Time                `json:"networkLast,omitempty"`
}
type Traffic struct {
	mu       sync.Mutex
	path     string
	state    TrafficState
	lastSave time.Time
	rates    map[string]networkRate
	roles    NetworkInterfaces
}

func NewTraffic(dir string) *Traffic {
	t := &Traffic{path: filepath.Join(dir, "traffic.json"), state: TrafficState{Previous: map[string]Counter{}, Days: map[string]map[string]Counter{}}}
	if b, e := os.ReadFile(t.path); e == nil {
		_ = json.Unmarshal(b, &t.state)
	}
	if t.state.Previous == nil {
		t.state.Previous = map[string]Counter{}
	}
	if t.state.Days == nil {
		t.state.Days = map[string]map[string]Counter{}
	}
	t.initNetworks()
	return t
}
func Delta(previous, current uint64) uint64 {
	if current >= previous {
		return current - previous
	}
	if previous > 1<<63 && current < 1<<63 {
		return (^uint64(0) - previous) + 1 + current
	}
	return current
}
func (t *Traffic) Feed(at time.Time, boot string, items []map[string]string) {
	t.feed(at, boot, items, nil)
}
func (t *Traffic) FeedNetworks(at time.Time, boot string, items []map[string]string, interfaces NetworkInterfaces) {
	t.feed(at, boot, items, &interfaces)
}
func (t *Traffic) feed(at time.Time, boot string, items []map[string]string, interfaces *NetworkInterfaces) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !at.After(t.state.SampledAt) {
		return
	}
	if interfaces != nil {
		t.sampleNetworks(at, boot, items, *interfaces)
	}
	if t.state.StartedAt.IsZero() {
		t.state.StartedAt = at
	}
	day := at.In(time.FixedZone("JST", 9*3600)).Format("2006-01-02")
	if t.state.Days[day] == nil {
		t.state.Days[day] = map[string]Counter{}
	}
	for _, item := range items {
		name := item["interface"]
		if name == "" || len(name) > 32 {
			continue
		}
		rx, e1 := strconv.ParseUint(item["rxBytes"], 10, 64)
		tx, e2 := strconv.ParseUint(item["txBytes"], 10, 64)
		if e1 != nil || e2 != nil {
			continue
		}
		now := Counter{rx, tx}
		previous, known := t.state.Previous[name]
		delta := Counter{}
		if known {
			if boot != "" && t.state.BootID != "" && boot != t.state.BootID {
				delta = now
			} else {
				delta = Counter{Delta(previous.RX, rx), Delta(previous.TX, tx)}
			}
		}
		sum := t.state.Days[day][name]
		sum.RX += delta.RX
		sum.TX += delta.TX
		t.state.Days[day][name] = sum
		t.state.Previous[name] = now
	}
	t.state.SampledAt = at
	t.state.BootID = boot
	cutoff := at.In(trafficJapan).AddDate(0, 0, -90).Format("2006-01-02")
	for d := range t.state.Days {
		if d < cutoff {
			delete(t.state.Days, d)
		}
	}
	for d := range t.state.NetworkDays {
		if d < cutoff {
			delete(t.state.NetworkDays, d)
		}
	}
	if time.Since(t.lastSave) > time.Minute {
		_ = config.Atomic(t.path, t.state)
		t.lastSave = time.Now()
	}
}
func (t *Traffic) Save() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return config.Atomic(t.path, t.state)
}
func (t *Traffic) Summary(iface string, now time.Time) map[string]any {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.state.StartedAt.IsZero() || iface == "" && len(t.state.NetworkStarted) == 0 {
		return nil
	}
	day := now.In(time.FixedZone("JST", 9*3600)).Format("2006-01-02")
	today := t.state.Days[day][iface]
	month := Counter{}
	total := Counter{}
	for d, byIF := range t.state.Days {
		value := byIF[iface]
		total.RX += value.RX
		total.TX += value.TX
		if len(d) >= 7 && d[:7] == day[:7] {
			month.RX += value.RX
			month.TX += value.TX
		}
	}
	kind := "回線"
	if iface == "wlan0" {
		kind = "Wi-Fi"
	}
	value := map[string]any{"interface": iface, "wanType": kind, "startedAt": t.state.StartedAt, "sampledAt": t.state.SampledAt, "todayRX": strconv.FormatUint(today.RX, 10), "todayTX": strconv.FormatUint(today.TX, 10), "monthRX": strconv.FormatUint(month.RX, 10), "monthTX": strconv.FormatUint(month.TX, 10), "totalRX": strconv.FormatUint(total.RX, 10), "totalTX": strconv.FormatUint(total.TX, 10), "scope": "現在のWANインターフェース・記録開始からの実測分", "stale": now.Sub(t.state.SampledAt) > 35*time.Second}
	if iface == "" {
		for _, name := range []string{"todayRX", "todayTX", "monthRX", "monthTX", "totalRX", "totalTX"} {
			value[name] = nil
		}
	}
	value["networks"] = t.networksSummary(now)
	return value
}
