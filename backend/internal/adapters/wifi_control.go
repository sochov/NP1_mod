package adapters

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const WiFiDirectory = "/overlay/owner-apps/np1-control/private/wifi"

// WPATransport is an internal controller dependency, not a public command API.
// Tests supply an in-memory supplicant; production uses the stock control socket.
type WPATransport interface {
	Request(context.Context, string) (string, error)
}
type vendorWPATransport struct{}

func (vendorWPATransport) Request(ctx context.Context, command string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return vendorWPA(command)
}

type WiFiNetwork struct {
	ID       int    `json:"id"`
	SSID     string `json:"ssid"`
	Current  bool   `json:"current"`
	Disabled bool   `json:"disabled"`
	Managed  bool   `json:"managed"`
}
type WiFiConnection struct {
	State     string `json:"state"`
	SSID      string `json:"ssid"`
	NetworkID *int   `json:"networkID"`
	IPv4      string `json:"ipv4"`
}
type WiFiScanResult struct {
	SSID      string `json:"ssid"`
	BSSID     string `json:"bssid"`
	Frequency int    `json:"frequency"`
	SignalDBm int    `json:"signalDBm"`
	Security  string `json:"security"`
}
type WiFiScanView struct {
	ScanRequested    bool             `json:"scanRequested"`
	Results          []WiFiScanResult `json:"results"`
	ResultsAreCached bool             `json:"resultsAreCached"`
}
type WiFiSavedProfile struct {
	ID                 string `json:"id"`
	SSID               string `json:"ssid"`
	Security           string `json:"security"`
	PasswordConfigured bool   `json:"passwordConfigured"`
}
type WiFiTransaction struct {
	ID         string    `json:"id"`
	State      string    `json:"state"`
	ExpiresAt  time.Time `json:"expiresAt"`
	TargetSSID string    `json:"targetSSID"`
	CanConfirm bool      `json:"canConfirm"`
	Reason     string    `json:"reason,omitempty"`
}
type WiFiView struct {
	Status      WiFiConnection     `json:"status"`
	Networks    []WiFiNetwork      `json:"networks"`
	Saved       []WiFiSavedProfile `json:"saved"`
	Transaction *WiFiTransaction   `json:"transaction"`
}
type WiFiNewConnection struct {
	SSID     string `json:"ssid"`
	Security string `json:"security"`
	Password string `json:"password,omitempty"`
}
type wifiProfile struct {
	ID       string `json:"id"`
	SSID     string `json:"ssid"`
	Security string `json:"security"`
	Password string `json:"password"`
}
type wifiPending struct {
	Public    WiFiTransaction `json:"public"`
	OldID     *int            `json:"oldID"`
	OldSSID   string          `json:"oldSSID"`
	TargetID  int             `json:"targetID"`
	CreatedID *int            `json:"createdID"`
	Profile   *wifiProfile    `json:"profile,omitempty"`
}
type WiFiController struct {
	mu        sync.Mutex
	transport WPATransport
	directory string
	pending   *wifiPending
	last      *WiFiTransaction
	timer     *time.Timer
	timeout   time.Duration
}

func NewWiFiController(directory string, transport WPATransport) *WiFiController {
	if transport == nil {
		transport = vendorWPATransport{}
	}
	return &WiFiController{directory: directory, transport: transport, timeout: 90 * time.Second}
}

var wifiController = NewWiFiController(WiFiDirectory, nil)

func WiFiNetworks(ctx context.Context) (WiFiView, error) { return wifiController.List(ctx) }
func WiFiScan(ctx context.Context) (WiFiScanView, error) { return wifiController.Scan(ctx) }
func WiFiConnectSaved(ctx context.Context, networkID int) (WiFiTransaction, error) {
	return wifiController.ConnectSaved(ctx, networkID)
}
func WiFiConnectNew(ctx context.Context, change WiFiNewConnection) (WiFiTransaction, error) {
	return wifiController.ConnectNew(ctx, change)
}
func WiFiConnectProfile(ctx context.Context, profileID string) (WiFiTransaction, error) {
	return wifiController.ConnectProfile(ctx, profileID)
}
func WiFiConfirm(ctx context.Context, id string) (WiFiTransaction, error) {
	return wifiController.Confirm(ctx, id)
}
func WiFiRollback(ctx context.Context, id string) (WiFiTransaction, error) {
	return wifiController.Rollback(ctx, id)
}

// RecoverWiFi rolls back an interrupted transaction before allowing mutations.
func RecoverWiFi(ctx context.Context) error { return wifiController.Recover(ctx) }

func (c *WiFiController) request(ctx context.Context, command string) (string, error) {
	raw, e := c.transport.Request(ctx, command)
	if e != nil {
		return "", errors.New("WIFI_CONTROL_UNAVAILABLE")
	}
	if len(raw) > 32768 {
		return "", errors.New("WIFI_RESPONSE_TOO_LARGE")
	}
	raw = strings.TrimRight(raw, "\r\n")
	if strings.HasPrefix(raw, "FAIL") || strings.HasPrefix(raw, "UNKNOWN COMMAND") {
		return "", errors.New("WIFI_COMMAND_REJECTED")
	}
	return raw, nil
}
func (c *WiFiController) ok(ctx context.Context, command string) error {
	raw, e := c.request(ctx, command)
	if e != nil {
		return e
	}
	if raw != "OK" {
		return errors.New("WIFI_RESPONSE_INVALID")
	}
	return nil
}
func wifiID(raw string) (int, error) {
	if len(raw) == 0 || len(raw) > 9 {
		return 0, errors.New("WIFI_NETWORK_ID_INVALID")
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, errors.New("WIFI_NETWORK_ID_INVALID")
		}
	}
	value, e := strconv.Atoi(raw)
	if e != nil || value > 1000000 {
		return 0, errors.New("WIFI_NETWORK_ID_INVALID")
	}
	return value, nil
}
func wifiUUID() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
func wifiSSID(value string) bool {
	if !utf8.ValidString(value) || len(value) < 1 || len(value) > 32 {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func wifiPassword(value string) bool {
	if len(value) == 64 {
		_, e := hex.DecodeString(value)
		return e == nil
	}
	if len(value) < 8 || len(value) > 63 {
		return false
	}
	for _, b := range []byte(value) {
		if b < 32 || b > 126 {
			return false
		}
	}
	return true
}
func decodeWPASSID(value string) (string, error) {
	var out []byte
	for i := 0; i < len(value); i++ {
		if value[i] != '\\' {
			out = append(out, value[i])
			continue
		}
		i++
		if i >= len(value) {
			return "", errors.New("WIFI_SSID_ENCODING_INVALID")
		}
		switch value[i] {
		case 'x':
			if i+2 >= len(value) {
				return "", errors.New("WIFI_SSID_ENCODING_INVALID")
			}
			part, e := hex.DecodeString(value[i+1 : i+3])
			if e != nil {
				return "", errors.New("WIFI_SSID_ENCODING_INVALID")
			}
			out = append(out, part[0])
			i += 2
		case '\\', '"':
			out = append(out, value[i])
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		default:
			return "", errors.New("WIFI_SSID_ENCODING_INVALID")
		}
	}
	if !utf8.Valid(out) {
		return "", errors.New("WIFI_SSID_ENCODING_INVALID")
	}
	return string(out), nil
}
func parseWiFiStatus(raw string) (WiFiConnection, error) {
	out := WiFiConnection{}
	for _, line := range strings.Split(raw, "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		switch parts[0] {
		case "wpa_state":
			out.State = parts[1]
		case "ssid":
			value, e := decodeWPASSID(parts[1])
			if e != nil {
				return out, e
			}
			out.SSID = value
		case "id":
			value, e := wifiID(parts[1])
			if e != nil {
				return out, e
			}
			out.NetworkID = &value
		case "ip_address":
			out.IPv4 = parts[1]
		}
	}
	if out.State == "" {
		return out, errors.New("WIFI_STATUS_INVALID")
	}
	return out, nil
}
func parseWiFiNetworks(raw string) ([]WiFiNetwork, error) {
	lines := strings.Split(raw, "\n")
	if len(lines) == 0 || (lines[0] != "network id / ssid / bssid / flags" && lines[0] != "network id\tssid\tbssid\tflags") {
		return nil, errors.New("WIFI_NETWORK_LIST_INVALID")
	}
	out := []WiFiNetwork{}
	seen := map[int]bool{}
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		p := strings.Split(line, "\t")
		if len(p) != 4 || len(out) >= 128 {
			return nil, errors.New("WIFI_NETWORK_LIST_INVALID")
		}
		id, e := wifiID(p[0])
		if e != nil || seen[id] {
			return nil, errors.New("WIFI_NETWORK_LIST_INVALID")
		}
		ssid, e := decodeWPASSID(p[1])
		if e != nil {
			return nil, e
		}
		seen[id] = true
		out = append(out, WiFiNetwork{ID: id, SSID: ssid, Current: strings.Contains(p[3], "[CURRENT]"), Disabled: strings.Contains(p[3], "[DISABLED]")})
	}
	return out, nil
}
func (c *WiFiController) connection(ctx context.Context) (WiFiConnection, error) {
	raw, e := c.request(ctx, "STATUS")
	if e != nil {
		return WiFiConnection{}, e
	}
	return parseWiFiStatus(raw)
}
func (c *WiFiController) networks(ctx context.Context) ([]WiFiNetwork, error) {
	raw, e := c.request(ctx, "LIST_NETWORKS")
	if e != nil {
		return nil, e
	}
	return parseWiFiNetworks(raw)
}
func (c *WiFiController) List(ctx context.Context) (WiFiView, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.recoverIfNeeded(ctx); e != nil {
		return WiFiView{}, e
	}
	connection, e := c.connection(ctx)
	if e != nil {
		return WiFiView{}, e
	}
	networks, e := c.networks(ctx)
	if e != nil {
		return WiFiView{}, e
	}
	profiles, e := c.loadProfiles()
	if e != nil {
		return WiFiView{}, e
	}
	saved := []WiFiSavedProfile{}
	for _, p := range profiles {
		saved = append(saved, WiFiSavedProfile{p.ID, p.SSID, p.Security, p.Password != ""})
	}
	for i := range networks {
		if c.pending != nil && c.pending.CreatedID != nil && networks[i].ID == *c.pending.CreatedID {
			networks[i].Managed = true
		}
	}
	transaction := c.last
	if c.pending != nil {
		view := c.pending.Public
		view.CanConfirm = connection.State == "COMPLETED" && connection.NetworkID != nil && *connection.NetworkID == c.pending.TargetID
		transaction = &view
	}
	return WiFiView{connection, networks, saved, transaction}, nil
}
func (c *WiFiController) Scan(ctx context.Context) (WiFiScanView, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.ok(ctx, "SCAN"); e != nil {
		return WiFiScanView{}, e
	}
	raw, e := c.request(ctx, "SCAN_RESULTS")
	if e != nil {
		return WiFiScanView{}, e
	}
	lines := strings.Split(raw, "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "bssid / frequency") {
		return WiFiScanView{}, errors.New("WIFI_SCAN_INVALID")
	}
	out := []WiFiScanResult{}
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		p := strings.SplitN(line, "\t", 5)
		if len(p) != 5 || len(out) >= 256 {
			return WiFiScanView{}, errors.New("WIFI_SCAN_INVALID")
		}
		frequency, e := strconv.Atoi(p[1])
		if e != nil || frequency < 2000 || frequency > 7200 {
			return WiFiScanView{}, errors.New("WIFI_SCAN_INVALID")
		}
		signal, e := strconv.Atoi(p[2])
		if e != nil || signal < -150 || signal > 0 {
			return WiFiScanView{}, errors.New("WIFI_SCAN_INVALID")
		}
		ssid, e := decodeWPASSID(p[4])
		if e != nil || !wifiSSID(ssid) {
			continue
		}
		security := "unsupported"
		switch {
		case strings.Contains(p[3], "WPA2-PSK") && !strings.Contains(p[3], "SAE"):
			security = "wpa2"
		case !strings.Contains(p[3], "WPA") && !strings.Contains(p[3], "WEP") && !strings.Contains(p[3], "SAE") && !strings.Contains(p[3], "EAP"):
			security = "open"
		}
		out = append(out, WiFiScanResult{ssid, p[0], frequency, signal, security})
	}
	return WiFiScanView{true, out, true}, nil
}

func (c *WiFiController) begin(ctx context.Context, target int, ssid string, created *int, profile *wifiProfile) (WiFiTransaction, error) {
	connection, e := c.connection(ctx)
	if e != nil {
		return WiFiTransaction{}, e
	}
	id, e := wifiUUID()
	if e != nil {
		return WiFiTransaction{}, errors.New("WIFI_RANDOM_UNAVAILABLE")
	}
	pending := &wifiPending{Public: WiFiTransaction{ID: id, State: "pending", ExpiresAt: time.Now().Add(c.timeout), TargetSSID: ssid}, OldID: connection.NetworkID, TargetID: target, CreatedID: created, Profile: profile}
	if pending.OldID != nil {
		networks, e := c.networks(ctx)
		if e != nil {
			return WiFiTransaction{}, e
		}
		found := false
		for _, network := range networks {
			if network.ID == *pending.OldID {
				pending.OldSSID = network.SSID
				found = true
				break
			}
		}
		if !found {
			return WiFiTransaction{}, errors.New("WIFI_PREVIOUS_NETWORK_UNAVAILABLE")
		}
	}
	if e = c.savePending(pending); e != nil {
		return WiFiTransaction{}, e
	}
	c.pending = pending
	if e = c.ok(ctx, "SELECT_NETWORK "+strconv.Itoa(target)); e != nil {
		_, rollback := c.rollbackLocked("connect-failed")
		if rollback != nil {
			return WiFiTransaction{}, rollback
		}
		return WiFiTransaction{}, e
	}
	c.timer = time.AfterFunc(c.timeout, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.pending != nil && c.pending.Public.ID == id {
			_, _ = c.rollbackLocked("confirmation-timeout")
		}
	})
	return pending.Public, nil
}
func (c *WiFiController) ready(ctx context.Context) error {
	if e := c.recoverIfNeeded(ctx); e != nil {
		return e
	}
	if c.pending != nil {
		return errors.New("WIFI_TRANSACTION_PENDING")
	}
	return nil
}
func (c *WiFiController) ConnectSaved(ctx context.Context, id int) (WiFiTransaction, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id < 0 || id > 1000000 {
		return WiFiTransaction{}, errors.New("WIFI_NETWORK_ID_INVALID")
	}
	if e := c.ready(ctx); e != nil {
		return WiFiTransaction{}, e
	}
	networks, e := c.networks(ctx)
	if e != nil {
		return WiFiTransaction{}, e
	}
	for _, network := range networks {
		if network.ID == id {
			return c.begin(ctx, id, network.SSID, nil, nil)
		}
	}
	return WiFiTransaction{}, errors.New("WIFI_NETWORK_NOT_FOUND")
}
func (c *WiFiController) ConnectNew(ctx context.Context, change WiFiNewConnection) (WiFiTransaction, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connectNewLocked(ctx, change, "")
}
func (c *WiFiController) ConnectProfile(ctx context.Context, profileID string) (WiFiTransaction, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	profiles, e := c.loadProfiles()
	if e != nil {
		return WiFiTransaction{}, e
	}
	for _, p := range profiles {
		if p.ID == profileID {
			return c.connectNewLocked(ctx, WiFiNewConnection{p.SSID, p.Security, p.Password}, p.ID)
		}
	}
	return WiFiTransaction{}, errors.New("WIFI_SAVED_PROFILE_NOT_FOUND")
}
func (c *WiFiController) connectNewLocked(ctx context.Context, change WiFiNewConnection, profileID string) (WiFiTransaction, error) {
	if !wifiSSID(change.SSID) || !(change.Security == "open" && change.Password == "" || change.Security == "wpa2" && wifiPassword(change.Password)) {
		return WiFiTransaction{}, errors.New("WIFI_CONNECTION_INVALID")
	}
	if e := c.ready(ctx); e != nil {
		return WiFiTransaction{}, e
	}
	uuid, e := wifiUUID()
	if e != nil {
		return WiFiTransaction{}, errors.New("WIFI_RANDOM_UNAVAILABLE")
	}
	raw, e := c.request(ctx, "ADD_NETWORK")
	if e != nil {
		return WiFiTransaction{}, e
	}
	id, e := wifiID(raw)
	if e != nil {
		return WiFiTransaction{}, e
	}
	if profileID == "" {
		profileID = "ncc-" + uuid
	}
	profile := wifiProfile{ID: profileID, SSID: change.SSID, Security: change.Security, Password: change.Password}
	if e = c.ok(ctx, "SET_NETWORK "+strconv.Itoa(id)+" id_str "+strconv.Quote(profile.ID)); e != nil {
		background, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		_ = c.ok(background, "REMOVE_NETWORK "+strconv.Itoa(id)) // ID is the freshly returned ADD_NETWORK object.
		return WiFiTransaction{}, e
	}
	cleanup := func() {
		background, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		owner, e := c.request(background, "GET_NETWORK "+strconv.Itoa(id)+" id_str")
		if e == nil && owner == strconv.Quote(profile.ID) {
			_ = c.ok(background, "REMOVE_NETWORK "+strconv.Itoa(id))
		}
	}
	commands := []string{"ssid " + hex.EncodeToString([]byte(change.SSID))}
	if change.Security == "open" {
		commands = append(commands, "key_mgmt NONE")
	} else {
		// Stock wpa_config_parse_psk copies bytes between the first and last quote;
		// it does not unescape Go/JSON backslashes. Printable ASCII was validated.
		psk := "\"" + change.Password + "\""
		if len(change.Password) == 64 {
			psk = change.Password
		}
		commands = append(commands, "key_mgmt WPA-PSK", "proto RSN", "psk "+psk)
	}
	for _, command := range commands {
		if e = c.ok(ctx, "SET_NETWORK "+strconv.Itoa(id)+" "+command); e != nil {
			cleanup()
			return WiFiTransaction{}, e
		}
	}
	transaction, e := c.begin(ctx, id, change.SSID, &id, &profile)
	if e != nil && c.pending == nil {
		cleanup()
	}
	return transaction, e
}
func (c *WiFiController) Confirm(ctx context.Context, id string) (WiFiTransaction, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.recoverIfNeeded(ctx); e != nil {
		return WiFiTransaction{}, e
	}
	if c.pending == nil || c.pending.Public.ID != id {
		return WiFiTransaction{}, errors.New("WIFI_TRANSACTION_NOT_FOUND")
	}
	if !time.Now().Before(c.pending.Public.ExpiresAt) {
		return c.rollbackLocked("confirmation-timeout")
	}
	connection, e := c.connection(ctx)
	if e != nil {
		return WiFiTransaction{}, e
	}
	if connection.State != "COMPLETED" || connection.NetworkID == nil || *connection.NetworkID != c.pending.TargetID || connection.SSID != c.pending.Public.TargetSSID {
		return WiFiTransaction{}, errors.New("WIFI_TARGET_NOT_CONNECTED")
	}
	if c.pending.CreatedID != nil {
		owner, e := c.request(ctx, "GET_NETWORK "+strconv.Itoa(*c.pending.CreatedID)+" id_str")
		if e != nil || c.pending.Profile == nil || owner != strconv.Quote(c.pending.Profile.ID) {
			return WiFiTransaction{}, errors.New("WIFI_TARGET_OWNERSHIP_MISMATCH")
		}
	}
	if c.pending.Profile != nil {
		if e = c.storeProfile(*c.pending.Profile); e != nil {
			return WiFiTransaction{}, e
		}
	}
	result := c.pending.Public
	result.State = "confirmed"
	result.CanConfirm = false
	if e = c.clearPending(); e != nil {
		return WiFiTransaction{}, e
	}
	if c.timer != nil {
		c.timer.Stop()
	}
	c.pending = nil
	c.last = &result
	return result, nil
}
func (c *WiFiController) Rollback(_ context.Context, id string) (WiFiTransaction, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.recoverIfNeeded(context.Background()); e != nil {
		return WiFiTransaction{}, e
	}
	if c.pending == nil || c.pending.Public.ID != id {
		return WiFiTransaction{}, errors.New("WIFI_TRANSACTION_NOT_FOUND")
	}
	return c.rollbackLocked("user-request")
}
func (c *WiFiController) rollbackLocked(reason string) (WiFiTransaction, error) {
	if c.pending == nil {
		return WiFiTransaction{}, errors.New("WIFI_TRANSACTION_NOT_FOUND")
	}
	p := c.pending
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	command := "DISCONNECT"
	if p.OldID != nil {
		networks, e := c.networks(ctx)
		if e != nil {
			return WiFiTransaction{}, errors.New("WIFI_ROLLBACK_RESTORE_FAILED")
		}
		found := false
		for _, network := range networks {
			if network.ID == *p.OldID && network.SSID == p.OldSSID {
				found = true
				break
			}
		}
		if !found {
			return WiFiTransaction{}, errors.New("WIFI_ROLLBACK_PREVIOUS_ID_CHANGED")
		}
		command = "SELECT_NETWORK " + strconv.Itoa(*p.OldID)
	}
	if e := c.ok(ctx, command); e != nil {
		return WiFiTransaction{}, errors.New("WIFI_ROLLBACK_RESTORE_FAILED")
	}
	if p.CreatedID != nil {
		owner, e := c.request(ctx, "GET_NETWORK "+strconv.Itoa(*p.CreatedID)+" id_str")
		absent := false
		if e != nil {
			networks, listErr := c.networks(ctx)
			if listErr != nil {
				return WiFiTransaction{}, errors.New("WIFI_ROLLBACK_OWNERSHIP_MISMATCH")
			}
			absent = true
			for _, network := range networks {
				if network.ID == *p.CreatedID {
					absent = false
				}
			}
		}
		if !absent {
			if e != nil || p.Profile == nil || owner != strconv.Quote(p.Profile.ID) {
				return WiFiTransaction{}, errors.New("WIFI_ROLLBACK_OWNERSHIP_MISMATCH")
			}
			if e = c.ok(ctx, "REMOVE_NETWORK "+strconv.Itoa(*p.CreatedID)); e != nil {
				return WiFiTransaction{}, errors.New("WIFI_ROLLBACK_REMOVE_FAILED")
			}
		}
	}
	verified := false
	until := time.Now().Add(8 * time.Second)
	for time.Now().Before(until) && ctx.Err() == nil {
		connection, e := c.connection(ctx)
		if e == nil {
			if p.OldID == nil {
				verified = connection.NetworkID == nil && (connection.State == "DISCONNECTED" || connection.State == "INACTIVE")
			} else {
				verified = connection.State == "COMPLETED" && connection.NetworkID != nil && *connection.NetworkID == *p.OldID && connection.SSID == p.OldSSID
			}
		}
		if verified {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(200 * time.Millisecond):
		}
	}
	if !verified {
		return WiFiTransaction{}, errors.New("WIFI_ROLLBACK_CONNECTION_UNVERIFIED")
	}
	result := p.Public
	result.State = "rolled-back"
	result.CanConfirm = false
	result.Reason = reason
	if e := c.clearPending(); e != nil {
		return WiFiTransaction{}, e
	}
	if c.timer != nil {
		c.timer.Stop()
	}
	c.pending = nil
	c.last = &result
	return result, nil
}
