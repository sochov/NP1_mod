package store

import (
	"strconv"
	"time"
)

// Exact interfaces must be verified by the collector. Do not pass a physical
// parent together with its child, AP, bridge, CLAT or Tailscale interfaces.
// On the verified NP1: Cellular=usb0 (not usb0.1/.2), WiFi=STA wlan0.
type NetworkInterfaces struct {
	Cellular string
	WiFi     string
}
type NetworkBucket struct {
	Counter
	ObservedSeconds float64 `json:"observedSeconds"`
	Intervals       int     `json:"intervals"`
}
type NetworkSnapshot struct {
	Counter
	At        time.Time `json:"at"`
	BootID    string    `json:"bootId"`
	Interface string    `json:"interface"`
}
type networkRate struct {
	RX, TX *float64
	Status string
	Active bool
}

var trafficJapan = time.FixedZone("JST", 9*3600)

const maximumSpeedGap = 10 * time.Second
const speedStaleAfter = 6 * time.Second

func (t *Traffic) initNetworks() {
	if t.state.NetworkDays == nil {
		t.state.NetworkDays = map[string]map[string]NetworkBucket{}
	}
	if t.state.NetworkPrevious == nil {
		t.state.NetworkPrevious = map[string]NetworkSnapshot{}
	}
	if t.state.NetworkStarted == nil {
		t.state.NetworkStarted = map[string]time.Time{}
	}
	if t.state.NetworkLast == nil {
		t.state.NetworkLast = map[string]time.Time{}
	}
	if t.rates == nil {
		t.rates = map[string]networkRate{}
	}
}
func counterItems(items []map[string]string) map[string]Counter {
	result := map[string]Counter{}
	duplicates := map[string]bool{}
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
		if _, ok := result[name]; ok {
			duplicates[name] = true
		}
		result[name] = Counter{RX: rx, TX: tx}
	}
	for name := range duplicates {
		delete(result, name)
	}
	return result
}
func decreased(previous, current uint64) bool {
	return current < previous && !(previous > 1<<63 && current < 1<<63)
}
func (t *Traffic) sampleNetworks(at time.Time, boot string, items []map[string]string, roles NetworkInterfaces) {
	t.initNetworks()
	t.roles = roles
	values := counterItems(items)
	day := at.In(trafficJapan).Format("2006-01-02")
	if t.state.NetworkDays[day] == nil {
		t.state.NetworkDays[day] = map[string]NetworkBucket{}
	}
	for kind, name := range map[string]string{"cellular": roles.Cellular, "wifi": roles.WiFi} {
		rate := networkRate{Status: "inactive"}
		if name == "" || roles.Cellular != "" && roles.Cellular == roles.WiFi {
			delete(t.state.NetworkPrevious, kind)
			t.rates[kind] = rate
			continue
		}
		value, ok := values[name]
		if !ok {
			delete(t.state.NetworkPrevious, kind)
			rate.Status = "unknown"
			t.rates[kind] = rate
			continue
		}
		rate.Active = true
		rate.Status = "baseline"
		previous, known := t.state.NetworkPrevious[kind]
		t.state.NetworkPrevious[kind] = NetworkSnapshot{Counter: value, At: at, BootID: boot, Interface: name}
		t.state.NetworkLast[kind] = at
		if t.state.NetworkStarted[kind].IsZero() {
			t.state.NetworkStarted[kind] = at
		}
		if known && previous.Interface == name {
			dt := at.Sub(previous.At)
			switch {
			case boot == "" || previous.BootID == "":
				rate.Status = "unknown"
			case boot != previous.BootID:
				rate.Status = "reboot"
			case decreased(previous.RX, value.RX) || decreased(previous.TX, value.TX):
				rate.Status = "reset"
			case dt <= 0:
				rate.Status = "unknown"
			default:
				delta := Counter{RX: Delta(previous.RX, value.RX), TX: Delta(previous.TX, value.TX)}
				bucket := t.state.NetworkDays[day][kind]
				bucket.RX += delta.RX
				bucket.TX += delta.TX
				// Bytes are attributed to the collection date, while coverage
				// seconds stay inside that date even after a long cross-day gap.
				local := at.In(trafficJapan)
				midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, trafficJapan)
				coverageStart := previous.At
				if coverageStart.Before(midnight) {
					coverageStart = midnight
				}
				bucket.ObservedSeconds += at.Sub(coverageStart).Seconds()
				bucket.Intervals++
				t.state.NetworkDays[day][kind] = bucket
				if dt > maximumSpeedGap {
					rate.Status = "gap"
				} else {
					rx, tx := float64(delta.RX)/dt.Seconds(), float64(delta.TX)/dt.Seconds()
					rate.RX = &rx
					rate.TX = &tx
					rate.Status = "available"
				}
			}
		}
		t.rates[kind] = rate
	}
}
func stringsForBucket(bucket NetworkBucket) (any, any) {
	if bucket.Intervals == 0 {
		return nil, nil
	}
	return strconv.FormatUint(bucket.RX, 10), strconv.FormatUint(bucket.TX, 10)
}
func (t *Traffic) networksSummary(now time.Time) map[string]any {
	out := map[string]any{}
	day := now.In(trafficJapan).Format("2006-01-02")
	for _, kind := range []string{"cellular", "wifi"} {
		today := t.state.NetworkDays[day][kind]
		month, total := NetworkBucket{}, NetworkBucket{}
		for date, networks := range t.state.NetworkDays {
			if date > day {
				continue
			}
			value := networks[kind]
			total.RX += value.RX
			total.TX += value.TX
			total.ObservedSeconds += value.ObservedSeconds
			total.Intervals += value.Intervals
			if len(date) >= 7 && date[:7] == day[:7] {
				month.RX += value.RX
				month.TX += value.TX
				month.ObservedSeconds += value.ObservedSeconds
				month.Intervals += value.Intervals
			}
		}
		todayRX, todayTX := stringsForBucket(today)
		monthRX, monthTX := stringsForBucket(month)
		totalRX, totalTX := stringsForBucket(total)
		name := t.roles.Cellular
		scope := "4G物理インターフェースの収集済み実測分。通信会社の請求量とは異なります"
		if kind == "wifi" {
			name = t.roles.WiFi
			scope = "Wi-Fi STA通信量（管理画面等を含む）。AP・bridge・Tailscaleは合算しません"
		}
		rate := t.rates[kind]
		last := t.state.NetworkLast[kind]
		started := t.state.NetworkStarted[kind]
		stale := last.IsZero() || now.Sub(last) > speedStaleAfter || now.Before(last)
		speedStatus := rate.Status
		if speedStatus == "" {
			speedStatus = "unknown"
		}
		var rx, tx any
		if rate.Status == "available" && !stale {
			rx = *rate.RX
			tx = *rate.TX
		} else if rate.Status == "available" && stale {
			speedStatus = "stale"
		}
		var sampledAt, observedSince any
		if !last.IsZero() {
			sampledAt = last
		}
		if !started.IsZero() {
			observedSince = started
		}
		status := "unknown"
		if total.Intervals > 0 {
			status = "available"
		}
		out[kind] = map[string]any{"interface": name, "todayRX": todayRX, "todayTX": todayTX, "monthRX": monthRX, "monthTX": monthTX, "totalRX": totalRX, "totalTX": totalTX, "rxBytesPerSecond": rx, "txBytesPerSecond": tx, "sampledAt": sampledAt, "observedSince": observedSince, "status": status, "speedStatus": speedStatus, "active": rate.Active, "stale": stale, "todayObservedSeconds": today.ObservedSeconds, "monthObservedSeconds": month.ObservedSeconds, "scope": scope, "partial": true}
	}
	return out
}
