package adapters

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// APClient is an associated station, optionally enriched from our DHCP leases.
// A lease alone can never create an online client.
type APClient struct {
	MAC              string  `json:"mac"`
	IPv4             string  `json:"ipv4,omitempty"`
	Hostname         string  `json:"hostname,omitempty"`
	Authorized       bool    `json:"authorized"`
	SignalDBm        *int    `json:"signalDBm,omitempty"`
	ConnectedSeconds *uint64 `json:"connectedSeconds,omitempty"`
}

func apMAC(value string) (string, bool) {
	mac, e := net.ParseMAC(value)
	if e != nil || len(mac) != 6 || mac[0]&1 != 0 {
		return "", false
	}
	return strings.ToLower(mac.String()), true
}
func apParseStations(raw string) ([]APClient, error) {
	if len(raw) > 65536 || strings.Contains(raw, "FAIL") {
		return nil, errors.New("AP_STATION_LIST_INVALID")
	}
	out := []APClient{}
	seen := map[string]bool{}
	var station *APClient
	flagsSeen := false
	associated := false
	flush := func() error {
		if station == nil {
			return nil
		}
		if !flagsSeen {
			return errors.New("AP_STATION_FLAGS_UNAVAILABLE")
		}
		if associated {
			out = append(out, *station)
		}
		return nil
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "Selected interface '"+apInterface+"'" {
			continue
		}
		if mac, ok := apMAC(line); ok {
			if e := flush(); e != nil {
				return nil, e
			}
			if seen[mac] || len(seen) >= 128 {
				return nil, errors.New("AP_STATION_LIST_INVALID")
			}
			seen[mac] = true
			station = &APClient{MAC: mac}
			flagsSeen = false
			associated = false
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || station == nil {
			return nil, errors.New("AP_STATION_LIST_INVALID")
		}
		switch key {
		case "flags":
			if flagsSeen {
				return nil, errors.New("AP_STATION_LIST_INVALID")
			}
			flagsSeen = true
			associated = strings.Contains(value, "[ASSOC]")
			station.Authorized = strings.Contains(value, "[AUTHORIZED]")
		case "signal":
			signal, e := strconv.Atoi(value)
			if e == nil && signal >= -150 && signal <= 0 {
				station.SignalDBm = &signal
			}
		case "connected_time":
			seconds, e := strconv.ParseUint(value, 10, 64)
			if e == nil {
				station.ConnectedSeconds = &seconds
			}
		}
	}
	if e := flush(); e != nil {
		return nil, e
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MAC < out[j].MAC })
	return out, nil
}

type apLease struct {
	IPv4, Hostname string
	Expires        uint64
}

func apParseLeases(raw string, now time.Time) map[string]apLease {
	out := map[string]apLease{}
	if len(raw) > 131072 {
		return out
	}
	for index, line := range strings.Split(raw, "\n") {
		if index >= 512 {
			break
		}
		fields := strings.Fields(line)
		if len(fields) != 5 {
			continue
		}
		expires, e := strconv.ParseUint(fields[0], 10, 64)
		if e != nil || expires != 0 && expires <= uint64(now.Unix()) {
			continue
		}
		mac, ok := apMAC(fields[1])
		if !ok {
			continue
		}
		ip := net.ParseIP(fields[2]).To4()
		if ip == nil || ip[0] != 192 || ip[1] != 168 || ip[2] != 226 || ip[3] < 20 || ip[3] > 99 {
			continue
		}
		hostname := fields[3]
		if hostname == "*" || !utf8.ValidString(hostname) || len(hostname) > 253 {
			hostname = ""
		}
		for _, r := range hostname {
			if unicode.IsControl(r) {
				hostname = ""
				break
			}
		}
		previous, exists := out[mac]
		if !exists || expires == 0 || previous.Expires != 0 && expires > previous.Expires {
			out[mac] = apLease{ip.String(), hostname, expires}
		}
	}
	return out
}
func apReadLeases(path string) (string, error) {
	st, e := os.Lstat(path)
	if e != nil || !st.Mode().IsRegular() || st.Size() > 131072 {
		return "", errors.New("AP_LEASES_UNAVAILABLE")
	}
	f, e := os.Open(path)
	if e != nil {
		return "", errors.New("AP_LEASES_UNAVAILABLE")
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, 131073))
	if e != nil || len(raw) > 131072 {
		return "", errors.New("AP_LEASES_UNAVAILABLE")
	}
	return string(raw), nil
}
func (r *apRuntime) captureClients(ctx context.Context) {
	r.state.Clients = nil
	r.state.ClientsAvailable = false
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	raw, e := apRun(ctx, "hostapd_cli", "-s", apPrivate(r.directory), "-p", filepath.Join(apPrivate(r.directory), "ctrl"), "-i", apInterface, "all_sta")
	if e != nil {
		return
	}
	clients, e := apParseStations(raw)
	if e != nil {
		return
	}
	if leasesRaw, e := apReadLeases(filepath.Join(apPrivate(r.directory), "leases")); e == nil {
		leases := apParseLeases(leasesRaw, time.Now())
		for index := range clients {
			if lease, ok := leases[clients[index].MAC]; ok {
				clients[index].IPv4 = lease.IPv4
				clients[index].Hostname = lease.Hostname
			}
		}
	}
	count := len(clients)
	r.state.ClientCount = &count
	r.state.Clients = clients
	r.state.ClientsAvailable = true
}
