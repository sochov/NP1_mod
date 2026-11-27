// Package numbershare sends locally recognized licence plates to a configured
// NumberShare API and keeps an on-device retry queue. API credentials never
// leave the backend process.
package numbershare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/config"
	"github.com/np1-control-center/ncc/internal/plate"
)

const (
	maxResponseBytes = 2 << 20
	apiTimeLayout    = "2006-01-02T15:04:05.000Z07:00"
)

var (
	registrationHandlePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,23}$`)
	digitsOnlyPattern         = regexp.MustCompile(`^[0-9]+$`)
)

type APIConfig struct {
	Revision      int    `json:"revision"`
	Enabled       bool   `json:"enabled"`
	Endpoint      string `json:"endpoint"`
	APIKey        string `json:"apiKey"`
	ManagementURL string `json:"managementUrl,omitempty"`
	AdminAPIKey   string `json:"adminApiKey,omitempty"`
	UserHandle    string `json:"userHandle,omitempty"`
	DeviceID      string `json:"deviceId,omitempty"`
	RecoveryCode  string `json:"recoveryCode,omitempty"`
}

type PublicConfig struct {
	Revision           int    `json:"revision"`
	Enabled            bool   `json:"enabled"`
	Endpoint           string `json:"endpoint"`
	KeyConfigured      bool   `json:"keyConfigured"`
	APIKey             string `json:"apiKey"`
	ManagementURL      string `json:"managementUrl,omitempty"`
	AdminKeyConfigured bool   `json:"adminKeyConfigured"`
	AdminAPIKey        string `json:"adminApiKey,omitempty"`
	UserHandle         string `json:"userHandle,omitempty"`
}

type RegistrationRequest struct {
	ExpectedRevision int
	Endpoint         string
	UserHandle       string
	DeviceName       string
	BootstrapKey     string
}

type UIRegistrationResult struct {
	UserHandle string `json:"userHandle"`
	DeviceID   string `json:"deviceId"`
}

type Plate struct {
	SerialNumber         string `json:"serial_number"`
	Region               string `json:"region,omitempty"`
	ClassificationNumber string `json:"classification_number,omitempty"`
	Hiragana             string `json:"hiragana,omitempty"`
}

type Location struct {
	Latitude   float64  `json:"latitude"`
	Longitude  float64  `json:"longitude"`
	AccuracyM  *float64 `json:"accuracy_m,omitempty"`
	ObservedAt string   `json:"observed_at,omitempty"`
}

type SightingPayload struct {
	EventID    string    `json:"event_id"`
	ObservedAt string    `json:"observed_at"`
	Plate      Plate     `json:"plate"`
	Location   *Location `json:"location,omitempty"`
}

type QueueItem struct {
	EventID     string          `json:"eventId"`
	RecordID    string          `json:"recordId"`
	Payload     SightingPayload `json:"payload"`
	State       string          `json:"state"` // pending | sent | blocked
	Attempts    int             `json:"attempts"`
	QueuedAt    time.Time       `json:"queuedAt"`
	LastAttempt *time.Time      `json:"lastAttemptAt,omitempty"`
	NextAttempt *time.Time      `json:"nextAttemptAt,omitempty"`
	SentAt      *time.Time      `json:"sentAt,omitempty"`
	HTTPStatus  int             `json:"httpStatus,omitempty"`
	LastError   string          `json:"lastError,omitempty"`
	MatchStatus string          `json:"matchStatus,omitempty"`
	Manual      bool            `json:"manual,omitempty"`
}

type UploadResult struct {
	Total    int `json:"total"`
	Queued   int `json:"queued"`
	Requeued int `json:"requeued"`
	Existing int `json:"existing"`
	Skipped  int `json:"skipped"`
}

type Status struct {
	Config  PublicConfig `json:"config"`
	Pending int          `json:"pending"`
	Sent    int          `json:"sent"`
	Blocked int          `json:"blocked"`
	Items   []QueueItem  `json:"items"`
}

type Service struct {
	dir    string
	client *http.Client
	clock  func() time.Time
	settle time.Duration
	wake   chan struct{}

	mu       sync.Mutex
	cfg      APIConfig
	queue    []QueueItem
	inflight string // first POST may be in progress with Attempts still zero
}

func New(stateDir string, client *http.Client) *Service {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	s := &Service{dir: filepath.Join(stateDir, "numbershare"), client: client, clock: time.Now, settle: defaultGrace, wake: make(chan struct{}, 1), cfg: APIConfig{Revision: 1}, queue: []QueueItem{}}
	if b, err := os.ReadFile(s.configPath()); err == nil {
		var saved APIConfig
		if json.Unmarshal(b, &saved) == nil && saved.Revision > 0 {
			s.cfg = saved
		}
	}
	if b, err := os.ReadFile(s.queuePath()); err == nil {
		var saved []QueueItem
		if json.Unmarshal(b, &saved) == nil && saved != nil {
			s.queue = saved
			// Older clients emitted RFC3339Nano with up to nine fractional digits,
			// while NumberShare accepts the documented millisecond representation.
			// Keep rejected items blocked. Only an explicit period upload may return
			// records in the owner's selected date range to the retry queue.
			changed := false
			for i := range s.queue {
				item := &s.queue[i]
				parsed, parseErr := time.Parse(time.RFC3339Nano, item.Payload.ObservedAt)
				if parseErr != nil {
					continue
				}
				normalized := parsed.Format(apiTimeLayout)
				if normalized == item.Payload.ObservedAt {
					continue
				}
				item.Payload.ObservedAt = normalized
				changed = true
			}
			if changed {
				_ = s.saveQueueLocked()
			}
		}
	}
	return s
}

func (s *Service) configPath() string { return filepath.Join(s.dir, "config.json") }
func (s *Service) queuePath() string  { return filepath.Join(s.dir, "queue.json") }

// ManagedConfigPath is the single NumberShare credential/configuration source
// used by both the owner UI and automatic device registration.
func ManagedConfigPath(stateDir string) string {
	return filepath.Join(stateDir, "numbershare", "config.json")
}

func loadAPIConfig(path string) (APIConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return APIConfig{}, err
	}
	var cfg APIConfig
	if json.Unmarshal(b, &cfg) != nil || cfg.Revision < 1 {
		return APIConfig{}, errors.New("invalid-managed-config")
	}
	normalized, err := normalizeEndpoint(cfg.Endpoint)
	if err != nil || normalized != cfg.Endpoint || cfg.Enabled && (cfg.Endpoint == "" || strings.TrimSpace(cfg.APIKey) == "") {
		return APIConfig{}, errors.New("invalid-managed-config")
	}
	return cfg, nil
}

// MigrateLegacyConfig imports main's former root-managed sender configuration
// only when the owner UI has no configuration yet. The legacy sender itself is
// retired, so a detection always has exactly one runtime owner.
func MigrateLegacyConfig(legacyPath, stateDir string) (bool, error) {
	managedPath := ManagedConfigPath(stateDir)
	if _, err := loadAPIConfig(managedPath); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	legacy, err := loadConfig(legacyPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	managed := APIConfig{Revision: 1, Enabled: legacy.Enabled, Endpoint: legacy.BaseURL, APIKey: legacy.APIKey}
	var recovery struct {
		UserHandle   string `json:"userHandle"`
		RecoveryCode string `json:"recoveryCode"`
	}
	if data, readErr := os.ReadFile(filepath.Join(filepath.Dir(legacyPath), "numbershare-recovery.json")); readErr == nil && json.Unmarshal(data, &recovery) == nil {
		managed.UserHandle = strings.TrimSpace(recovery.UserHandle)
		managed.RecoveryCode = strings.TrimSpace(recovery.RecoveryCode)
	}
	if _, err = normalizeEndpoint(managed.Endpoint); err != nil {
		return false, err
	}
	return true, config.Atomic(managedPath, managed)
}

func (s *Service) reloadConfig() {
	saved, err := loadAPIConfig(s.configPath())
	if err != nil {
		return
	}
	s.mu.Lock()
	changed := saved != s.cfg
	if changed {
		s.cfg = saved
	}
	s.mu.Unlock()
	if changed {
		s.signal()
	}
}

func publicConfig(c APIConfig) PublicConfig {
	return PublicConfig{
		Revision: c.Revision, Enabled: c.Enabled, Endpoint: c.Endpoint,
		KeyConfigured: c.APIKey != "", APIKey: c.APIKey,
		ManagementURL: c.ManagementURL, AdminKeyConfigured: c.AdminAPIKey != "", AdminAPIKey: c.AdminAPIKey,
		UserHandle: c.UserHandle,
	}
}

func normalizeEndpoint(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", errors.New("APIエンドポイントを確認してください")
	}
	if u.Scheme != "https" {
		return "", errors.New("APIエンドポイントにはHTTPSを指定してください")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/"
	return u.String(), nil
}

func managementURLFor(endpoint, raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		u, err := url.Parse(endpoint)
		if err != nil {
			return "", errors.New("管理画面URLを確認してください")
		}
		u.Path = strings.TrimSuffix(u.Path, "api/")
		u.RawQuery, u.Fragment = "", ""
		raw = u.String()
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("管理画面URLにはHTTPSのURLを指定してください")
	}
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	return u.String(), nil
}

func (s *Service) ConfigureManagement(expected int, managementURL string, key *string) (PublicConfig, error) {
	s.reloadConfig()
	s.mu.Lock()
	defer s.mu.Unlock()
	if expected != s.cfg.Revision {
		return PublicConfig{}, errors.New("revision conflict")
	}
	next := s.cfg
	if key != nil {
		next.AdminAPIKey = strings.TrimSpace(*key)
	}
	if next.AdminAPIKey != "" {
		var err error
		next.ManagementURL, err = managementURLFor(next.Endpoint, managementURL)
		if err != nil {
			return PublicConfig{}, err
		}
	} else {
		next.ManagementURL = ""
	}
	next.Revision++
	if err := config.Atomic(s.configPath(), next); err != nil {
		return PublicConfig{}, err
	}
	s.cfg = next
	return publicConfig(next), nil
}

func (s *Service) requestJSON(ctx context.Context, method, target, key string, body any, into any) (int, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-Key", key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, errors.New("NumberShare APIへ接続できません")
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if readErr != nil || len(data) > maxResponseBytes {
		return resp.StatusCode, errors.New("NumberShare APIの応答を読み取れません")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &failure)
		return resp.StatusCode, errors.New(apiError(resp.StatusCode, failure.Error))
	}
	if into != nil && json.Unmarshal(data, into) != nil {
		return resp.StatusCode, errors.New("NumberShare APIの応答を読み取れません")
	}
	return resp.StatusCode, nil
}

func (s *Service) Register(ctx context.Context, request RegistrationRequest) (UIRegistrationResult, error) {
	s.reloadConfig()
	endpoint, err := normalizeEndpoint(request.Endpoint)
	if err != nil || endpoint == "" {
		return UIRegistrationResult{}, errors.New("APIエンドポイントを確認してください")
	}
	handle := strings.ToLower(strings.TrimSpace(request.UserHandle))
	if !registrationHandlePattern.MatchString(handle) || digitsOnlyPattern.MatchString(handle) {
		return UIRegistrationResult{}, errors.New("匿名ユーザーIDは英小文字・数字・_・-の3〜24文字で、数字だけにはできません")
	}
	deviceName := strings.TrimSpace(request.DeviceName)
	if deviceName == "" {
		deviceName = "NP1"
	}
	if len([]rune(deviceName)) > 100 {
		return UIRegistrationResult{}, errors.New("端末名は1〜100文字で入力してください")
	}
	bootstrapKey := strings.TrimSpace(request.BootstrapKey)
	if len(bootstrapKey) < 24 {
		return UIRegistrationResult{}, errors.New("接続用キー（DEVICE_API_KEY）を確認してください")
	}
	managementURL, err := managementURLFor(endpoint, "")
	if err != nil {
		return UIRegistrationResult{}, err
	}
	usersURL, _ := endpointURL(endpoint, "users")
	var issued registrationResponse
	status, err := s.requestJSON(ctx, http.MethodPost, usersURL, bootstrapKey, map[string]string{"user_handle": handle, "device_name": deviceName}, &issued)
	if err != nil || status != http.StatusCreated || issued.APIKey == "" || issued.DeviceID == "" || issued.UserHandle != handle {
		if err != nil {
			return UIRegistrationResult{}, err
		}
		return UIRegistrationResult{}, errors.New("初回登録の応答を確認してください")
	}
	meURL, _ := endpointURL(endpoint, "users/me")
	var identity registrationIdentity
	if _, err = s.requestJSON(ctx, http.MethodGet, meURL, issued.APIKey, nil, &identity); err != nil || identity.DeviceID != issued.DeviceID || identity.UserHandle != handle {
		return UIRegistrationResult{}, errors.New("発行された機器用認証キーを確認できませんでした")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if request.ExpectedRevision != s.cfg.Revision || s.cfg.APIKey != "" {
		return UIRegistrationResult{}, errors.New("revision conflict")
	}
	if s.cfg.ManagementURL != "" {
		managementURL = s.cfg.ManagementURL
	}
	next := APIConfig{
		Revision: s.cfg.Revision + 1, Enabled: false, Endpoint: endpoint, APIKey: issued.APIKey,
		ManagementURL: managementURL, AdminAPIKey: s.cfg.AdminAPIKey, UserHandle: handle,
		DeviceID: issued.DeviceID, RecoveryCode: issued.RecoveryCode,
	}
	if err = config.Atomic(s.configPath(), next); err != nil {
		return UIRegistrationResult{}, err
	}
	s.cfg = next
	return UIRegistrationResult{UserHandle: handle, DeviceID: issued.DeviceID}, nil
}

func (s *Service) Configure(expected int, enabled bool, endpoint string, key *string) (PublicConfig, error) {
	s.reloadConfig()
	normalized, err := normalizeEndpoint(endpoint)
	if err != nil {
		return PublicConfig{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if expected != s.cfg.Revision {
		return PublicConfig{}, errors.New("revision conflict")
	}
	next := s.cfg
	next.Revision++
	next.Enabled, next.Endpoint = enabled, normalized
	if key != nil {
		next.APIKey = strings.TrimSpace(*key)
	}
	if enabled && (next.Endpoint == "" || next.APIKey == "") {
		return PublicConfig{}, errors.New("送信を有効にするにはエンドポイントと認証キーが必要です")
	}
	if err := config.Atomic(s.configPath(), next); err != nil {
		return PublicConfig{}, err
	}
	credentialsChanged := next.Endpoint != s.cfg.Endpoint || next.APIKey != s.cfg.APIKey
	s.cfg = next
	if credentialsChanged {
		now := s.clock().UTC()
		for i := range s.queue {
			if s.queue[i].State == "blocked" {
				s.queue[i].State, s.queue[i].LastError, s.queue[i].HTTPStatus = "pending", "", 0
				s.queue[i].NextAttempt = &now
			}
		}
		if err := s.saveQueueLocked(); err != nil {
			return PublicConfig{}, err
		}
	}
	s.signal()
	return publicConfig(s.cfg), nil
}

func (s *Service) Config() PublicConfig {
	s.reloadConfig()
	s.mu.Lock()
	defer s.mu.Unlock()
	return publicConfig(s.cfg)
}

func payloadFor(record plate.Record, eventID string) (SightingPayload, bool) {
	validated, ok := payload(record, true)
	if !ok {
		return SightingPayload{}, false
	}
	validated.EventID = eventID
	p := SightingPayload{EventID: eventID, ObservedAt: record.FirstSeen.Format(apiTimeLayout), Plate: Plate{
		SerialNumber: validated.Plate.SerialNumber, Region: validated.Plate.Region,
		ClassificationNumber: validated.Plate.ClassificationNumber, Hiragana: validated.Plate.Hiragana,
	}}
	if validated.Location != nil {
		p.Location = &Location{Latitude: validated.Location.Latitude, Longitude: validated.Location.Longitude, AccuracyM: validated.Location.AccuracyM}
	}
	return p, true
}

func (s *Service) realtimeDue(record plate.Record, now time.Time) time.Time {
	seen := record.LastSeen
	if seen.IsZero() {
		seen = record.FirstSeen
	}
	due := seen.Add(s.settle).UTC()
	if due.Before(now) {
		return now
	}
	return due
}

// Enqueue persists a confirmed local record before allowing the worker to
// transmit it. Repeated calls for the same merged record keep the event ID.
func (s *Service) Enqueue(record plate.Record) error {
	return s.EnqueueTrack(record, "")
}

// EnqueueTrack retires an unsent, automatic fragment if the registration
// journal subsequently attaches it to an older encounter. An attempted POST
// keeps its immutable event and payload for an idempotent retry.
func (s *Service) EnqueueTrack(record plate.Record, sourceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sourceID != "" && sourceID != record.ID {
		for i, item := range s.queue {
			if item.RecordID == sourceID && !item.Manual && item.State == "pending" && item.Attempts == 0 && item.EventID != s.inflight {
				old := s.queue
				s.queue = append(append([]QueueItem{}, old[:i]...), old[i+1:]...)
				if err := s.saveQueueLocked(); err != nil {
					s.queue = old
					return err
				}
				break
			}
		}
	}
	if !s.cfg.Enabled {
		return nil
	}
	for i := range s.queue {
		if s.queue[i].RecordID == record.ID {
			// Before the first attempt, later OCR frames may fill fields. Once an
			// attempt occurred the POST body must remain byte-for-byte stable.
			if s.queue[i].Attempts == 0 && s.queue[i].EventID != s.inflight {
				updated, ok := payloadFor(record, s.queue[i].EventID)
				if !ok {
					return nil
				}
				s.queue[i].Payload = updated
				due := s.realtimeDue(record, s.clock().UTC())
				s.queue[i].NextAttempt = &due
				return s.saveQueueLocked()
			}
			return nil
		}
	}
	id := eventID(record.ID)
	payload, ok := payloadFor(record, id)
	if !ok {
		return nil
	}
	now := s.clock().UTC()
	due := s.realtimeDue(record, now)
	s.queue = append(s.queue, QueueItem{EventID: id, RecordID: record.ID, Payload: payload, State: "pending", QueuedAt: now, NextAttempt: &due})
	if err := s.saveQueueLocked(); err != nil {
		s.queue = s.queue[:len(s.queue)-1]
		return err
	}
	s.signal()
	return nil
}

// EnqueueUpload adds an explicitly selected historical range to the durable
// queue. Manual uploads are transmitted even when realtime sharing is off;
// realtime detections remain opt-in through APIConfig.Enabled.
func (s *Service) EnqueueUpload(records []plate.Record) (UploadResult, error) {
	result := UploadResult{Total: len(records)}
	s.mu.Lock()
	if s.cfg.Endpoint == "" || s.cfg.APIKey == "" {
		s.mu.Unlock()
		return result, errors.New("NumberShare APIが未設定です")
	}
	existing := make(map[string]int, len(s.queue))
	for i, item := range s.queue {
		existing[item.RecordID] = i
	}
	original := append([]QueueItem(nil), s.queue...)
	now := s.clock().UTC()
	for _, record := range records {
		id := eventID(record.ID)
		payload, valid := payloadFor(record, id)
		if !valid {
			result.Skipped++
			continue
		}
		if index, ok := existing[record.ID]; ok {
			result.Existing++
			item := &s.queue[index]
			if item.State == "blocked" {
				payload.EventID = item.EventID
				item.Payload = payload
				item.State, item.LastError, item.HTTPStatus, item.Manual = "pending", "", 0, true
				item.NextAttempt = &now
				result.Requeued++
			}
			continue
		}
		s.queue = append(s.queue, QueueItem{EventID: id, RecordID: record.ID, Payload: payload, State: "pending", QueuedAt: now, NextAttempt: &now, Manual: true})
		existing[record.ID] = len(s.queue) - 1
		result.Queued++
	}
	if result.Queued > 0 || result.Requeued > 0 {
		if err := s.saveQueueLocked(); err != nil {
			s.queue = original
			s.mu.Unlock()
			return result, err
		}
	}
	s.mu.Unlock()
	if result.Queued > 0 || result.Requeued > 0 {
		s.signal()
	}
	return result, nil
}

func (s *Service) saveQueueLocked() error { return config.Atomic(s.queuePath(), s.queue) }
func (s *Service) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) Status() Status {
	s.reloadConfig()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Status{Config: publicConfig(s.cfg), Items: make([]QueueItem, len(s.queue))}
	for _, item := range s.queue {
		switch item.State {
		case "sent":
			out.Sent++
		case "blocked":
			out.Blocked++
		default:
			out.Pending++
		}
	}
	// Return newest first and cap status detail while keeping aggregate counts.
	for i := range out.Items {
		out.Items[i] = s.queue[len(s.queue)-1-i]
	}
	if len(out.Items) > 100 {
		out.Items = out.Items[:100]
	}
	return out
}

func (s *Service) Retry(eventID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.queue {
		if s.queue[i].EventID == eventID && s.queue[i].State != "sent" {
			now := s.clock().UTC()
			s.queue[i].State, s.queue[i].LastError, s.queue[i].HTTPStatus = "pending", "", 0
			s.queue[i].NextAttempt = &now
			if err := s.saveQueueLocked(); err != nil {
				return err
			}
			s.signal()
			return nil
		}
	}
	return errors.New("再送対象が見つかりません")
}

func retryDelay(attempt int) time.Duration {
	d := 5 * time.Second
	for i := 1; i < attempt && d < 15*time.Minute; i++ {
		d *= 2
	}
	if d > 15*time.Minute {
		d = 15 * time.Minute
	}
	return d
}

func (s *Service) nextDue() (QueueItem, APIConfig, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.Endpoint == "" || s.cfg.APIKey == "" {
		return QueueItem{}, APIConfig{}, false
	}
	now := s.clock().UTC()
	for _, item := range s.queue {
		if item.State == "pending" && (s.cfg.Enabled || item.Manual) && (item.NextAttempt == nil || !item.NextAttempt.After(now)) {
			s.inflight = item.EventID
			return item, s.cfg, true
		}
	}
	return QueueItem{}, APIConfig{}, false
}

func (s *Service) saveResult(recordID, eventID string, result Result) error {
	syncer := NewSyncer("", filepath.Dir(s.dir))
	state := syncer.loadState()
	item := state.Items[recordID]
	item.EventID, item.Sent, item.Permanent, item.Result = eventID, true, false, &result
	item.NextAttempt = time.Time{}
	state.Items[recordID] = item
	state.LastSuccessAt, state.LastError = s.clock().UTC(), ""
	return syncer.saveState(state)
}

func (s *Service) complete(item QueueItem, status int, body []byte, requestErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inflight == item.EventID {
		s.inflight = ""
	}
	for i := range s.queue {
		q := &s.queue[i]
		if q.EventID != item.EventID || q.State != "pending" {
			continue
		}
		now := s.clock().UTC()
		q.Attempts++
		q.LastAttempt, q.HTTPStatus = &now, status
		q.NextAttempt = nil
		var response serverResponse
		_ = json.Unmarshal(body, &response)
		validationError := error(nil)
		if requestErr == nil && status >= 200 && status < 300 && response.Saved {
			result, resultErr := responseResult(response, now)
			if resultErr == nil {
				resultErr = s.saveResult(q.RecordID, q.EventID, *result)
			}
			if resultErr == nil {
				q.State, q.SentAt, q.LastError, q.MatchStatus = "sent", &now, "", result.Status
				_ = s.saveQueueLocked()
				return
			}
			validationError = resultErr
		}
		if requestErr == nil && ((status >= 200 && status < 300) || status == 401 || status == 403 || status == 404 || status == 409 || status == 410 || status == 413 || status == 415 || status == 422) {
			q.State = "blocked"
			if validationError != nil {
				q.LastError = "API応答を確認してください: " + validationError.Error()
			} else {
				q.LastError = apiError(status, response.Error)
			}
		} else {
			q.State = "pending"
			next := now.Add(retryDelay(q.Attempts))
			q.NextAttempt = &next
			if requestErr != nil {
				q.LastError = "APIへ接続できません"
			} else {
				q.LastError = apiError(status, response.Error)
			}
		}
		_ = s.saveQueueLocked()
		return
	}
}

func apiError(status int, detail string) string {
	if detail != "" {
		return fmt.Sprintf("API %d: %s", status, detail)
	}
	if status == 0 {
		return "APIへ接続できません"
	}
	return fmt.Sprintf("APIがHTTP %dを返しました", status)
}

func endpointURL(base, relative string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.TrimLeft(relative, "/")
	return u.String(), nil
}

func (s *Service) send(ctx context.Context, cfg APIConfig, item QueueItem) (int, []byte, error) {
	b, err := json.Marshal(item.Payload)
	if err != nil {
		return 0, nil, err
	}
	target, err := endpointURL(cfg.Endpoint, "sightings")
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(b))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-Key", cfg.APIKey)
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if len(body) > maxResponseBytes {
		return resp.StatusCode, nil, errors.New("API response too large")
	}
	return resp.StatusCode, body, nil
}

func (s *Service) Run(ctx context.Context) {
	for {
		s.reloadConfig()
		for {
			item, cfg, ok := s.nextDue()
			if !ok {
				break
			}
			requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			status, body, err := s.send(requestCtx, cfg, item)
			cancel()
			s.complete(item, status, body, err)
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

type SharedSighting struct {
	ID                  int64     `json:"id"`
	EventID             string    `json:"event_id"`
	ObservedAt          string    `json:"observed_at"`
	ReceivedAt          string    `json:"received_at"`
	PlateID             *int64    `json:"plate_id"`
	MatchStatus         string    `json:"match_status"`
	RecognitionRevision int       `json:"recognition_revision"`
	Plate               Plate     `json:"plate"`
	Location            *Location `json:"location"`
	Mine                bool      `json:"mine"`
}

type SharedQuery struct {
	Text, MatchStatus, ObservedFrom, ObservedTo string
	Scope                                       string
}

type SharedResponse struct {
	Items     []SharedSighting `json:"items"`
	Truncated bool             `json:"truncated"`
	SyncedAt  time.Time        `json:"syncedAt"`
}

func (s *Service) apiGET(ctx context.Context, relative string, query url.Values, administrative bool, into any) error {
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()
	key := cfg.APIKey
	if administrative && cfg.AdminAPIKey != "" {
		key = cfg.AdminAPIKey
	}
	if cfg.Endpoint == "" || key == "" {
		return errors.New("NumberShare APIが未設定です")
	}
	target, err := endpointURL(cfg.Endpoint, relative)
	if err != nil {
		return err
	}
	u, _ := url.Parse(target)
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-Key", key)
	resp, err := s.client.Do(req)
	if err != nil {
		return errors.New("NumberShare APIへ接続できません")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var v struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&v)
		return errors.New(apiError(resp.StatusCode, v.Error))
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(into); err != nil {
		return errors.New("NumberShare APIの応答を読み取れません")
	}
	return nil
}

func containsFold(values []string, q string) bool {
	if q == "" {
		return true
	}
	q = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(q), "-", ""), "・", ""))
	for _, value := range values {
		v := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(value, "-", ""), "・", ""))
		if strings.Contains(v, q) {
			return true
		}
	}
	return false
}

func (s *Service) Shared(ctx context.Context, q SharedQuery) (SharedResponse, error) {
	s.mu.Lock()
	mine := map[string]bool{}
	for _, item := range s.queue {
		mine[item.EventID] = true
	}
	s.mu.Unlock()
	for _, item := range NewSyncer("", filepath.Dir(s.dir)).loadState().Items {
		if item.EventID != "" {
			mine[item.EventID] = true
		}
	}
	values := url.Values{"limit": {"100"}}
	if q.MatchStatus != "" {
		values.Set("match_status", q.MatchStatus)
	}
	if q.ObservedFrom != "" {
		values.Set("observed_from", q.ObservedFrom)
	}
	if q.ObservedTo != "" {
		values.Set("observed_to", q.ObservedTo)
	}
	out := SharedResponse{Items: []SharedSighting{}, SyncedAt: s.clock().UTC()}
	seenCursors := map[string]bool{}
	for {
		var result struct {
			Items      []SharedSighting `json:"items"`
			NextCursor *string          `json:"next_cursor"`
		}
		if err := s.apiGET(ctx, "sightings", values, true, &result); err != nil {
			return out, err
		}
		for _, item := range result.Items {
			item.Mine = mine[item.EventID]
			if q.Scope == "mine" && !item.Mine || q.Scope == "others" && item.Mine {
				continue
			}
			if !containsFold([]string{item.Plate.SerialNumber, item.Plate.Region, item.Plate.ClassificationNumber, item.Plate.Hiragana}, q.Text) {
				continue
			}
			out.Items = append(out.Items, item)
		}
		if result.NextCursor == nil || *result.NextCursor == "" {
			return out, nil
		}
		if seenCursors[*result.NextCursor] {
			return out, errors.New("NumberShare APIのページ情報が循環しています")
		}
		seenCursors[*result.NextCursor] = true
		values.Set("cursor", *result.NextCursor)
	}
}

type Vehicle struct {
	ID                   int64     `json:"id"`
	SerialNumber         string    `json:"serial_number"`
	Region               string    `json:"region"`
	ClassificationNumber string    `json:"classification_number"`
	Hiragana             string    `json:"hiragana"`
	Category             string    `json:"category"`
	Comment              string    `json:"comment"`
	Sightings            int       `json:"sightings"`
	LastSeen             *string   `json:"last_seen"`
	LastLocation         *Location `json:"last_location"`
}

type VehicleResponse struct {
	Items      []Vehicle `json:"items"`
	Total      int       `json:"total"`
	Categories []string  `json:"categories"`
}

func (s *Service) Vehicles(ctx context.Context, text, serial, category string) (VehicleResponse, error) {
	q := url.Values{"limit": {"100"}, "offset": {"0"}}
	if text != "" {
		q.Set("q", text)
	}
	if serial != "" {
		q.Set("serial_number", serial)
	}
	if category != "" {
		q.Set("category", category)
	}
	var out VehicleResponse
	err := s.apiGET(ctx, "plates", q, true, &out)
	return out, err
}

func (s *Service) Check(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	err := s.apiGET(ctx, "users/me", url.Values{}, false, &out)
	if err != nil {
		return nil, err
	}
	// Never pass unexpected secret fields from an upstream response to the browser.
	for _, key := range []string{"api_key", "recovery_code", "key"} {
		delete(out, key)
	}
	return out, nil
}
