package adapters

import (
	"errors"
	"strconv"
	"strings"
)

// hostapd prints zero even when the driver omits byte attributes. Only iw's
// explicitly present nl80211 byte fields can establish a measured zero.
func apDriverStationTraffic(raw string) (map[string]APClient, error) {
	if len(raw) > 65536 {
		return nil, errors.New("AP_STATION_TRAFFIC_INVALID")
	}
	out := map[string]APClient{}
	mac := ""
	seen := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Station ") {
			fields := strings.Fields(line)
			if len(fields) != 4 || fields[2] != "(on" || fields[3] != apInterface+")" {
				return nil, errors.New("AP_STATION_TRAFFIC_INVALID")
			}
			var ok bool
			mac, ok = apMAC(fields[1])
			if !ok {
				return nil, errors.New("AP_STATION_TRAFFIC_INVALID")
			}
			if _, exists := out[mac]; exists {
				return nil, errors.New("AP_STATION_TRAFFIC_INVALID")
			}
			out[mac] = APClient{MAC: mac}
			seen = map[string]bool{}
			continue
		}
		if mac == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || key != "rx bytes" && key != "tx bytes" {
			continue
		}
		client := out[mac]
		destination := &client.SentBytes
		if key == "tx bytes" {
			destination = &client.ReceivedBytes
		}
		*destination = nil
		if !seen[key] {
			if count, e := strconv.ParseUint(strings.TrimSpace(value), 10, 64); e == nil {
				text := strconv.FormatUint(count, 10)
				*destination = &text
			}
		}
		seen[key] = true
		out[mac] = client
	}
	return out, nil
}
func apApplyDriverTraffic(clients []APClient, raw string) {
	traffic, _ := apDriverStationTraffic(raw)
	for i := range clients {
		value := traffic[clients[i].MAC]
		clients[i].ReceivedBytes = value.ReceivedBytes
		clients[i].SentBytes = value.SentBytes
	}
}
