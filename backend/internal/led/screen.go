package led

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/config"
)

const ScreenFramePath = "/run/np1-control/led-screen.json"
const ScreenFrameInterval = 100 * time.Millisecond
const ScreenIdleTimeout = 1500 * time.Millisecond
const maximumScreenSequence = uint64(1<<53 - 1)

type ScreenError struct {
	Status       int
	Code, Reason string
}

func (e *ScreenError) Error() string                    { return e.Reason }
func screenError(status int, code, reason string) error { return &ScreenError{status, code, reason} }
func ScreenErrorStatus(err error) (int, string, string) {
	var problem *ScreenError
	if errors.As(err, &problem) {
		return problem.Status, problem.Code, problem.Reason
	}
	return 503, "SCREEN_LED_UNAVAILABLE", "画面連動LEDを更新できません"
}

type ScreenSession struct {
	ID              string `json:"id"`
	FrameIntervalMS int    `json:"frameIntervalMs"`
	IdleTimeoutMS   int    `json:"idleTimeoutMs"`
}
type ScreenUpdate struct {
	Sequence uint64    `json:"sequence"`
	Zones    [5][3]int `json:"zones"`
}
type ScreenFrame struct {
	ScreenUpdate
	UpdatedAt time.Time `json:"updatedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// DecodeScreenObject rejects duplicate fields as well as unknown fields and trailing JSON.
func DecodeScreenObject(data []byte, allowed ...string) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	first, e := d.Token()
	if e != nil || first != json.Delim('{') {
		return nil, errors.New("JSON object required")
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		token, e := d.Token()
		if e != nil {
			return nil, e
		}
		key, ok := token.(string)
		if !ok {
			return nil, errors.New("invalid field")
		}
		if _, ok = out[key]; ok {
			return nil, errors.New("duplicate field")
		}
		known := false
		for _, name := range allowed {
			if name == key {
				known = true
			}
		}
		if !known {
			return nil, errors.New("unknown field")
		}
		var value json.RawMessage
		if e = d.Decode(&value); e != nil {
			return nil, e
		}
		out[key] = value
	}
	if token, e := d.Token(); e != nil || token != json.Delim('}') {
		return nil, errors.New("invalid object")
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, errors.New("trailing JSON")
	}
	return out, nil
}
func parseScreenUpdate(raw map[string]json.RawMessage) (ScreenUpdate, error) {
	var out ScreenUpdate
	sequence, e := strconv.ParseUint(string(raw["sequence"]), 10, 64)
	if e != nil || sequence == 0 || sequence > maximumScreenSequence {
		return out, errors.New("invalid sequence")
	}
	out.Sequence = sequence
	var zones [][]json.RawMessage
	if e = json.Unmarshal(raw["zones"], &zones); e != nil || len(zones) != 5 {
		return out, errors.New("five RGB zones required")
	}
	for i, zone := range zones {
		if len(zone) != 3 {
			return out, errors.New("three channels required")
		}
		for j, value := range zone {
			n, e := strconv.ParseUint(string(value), 10, 8)
			if e != nil {
				return out, errors.New("RGB integer out of range")
			}
			out.Zones[i][j] = int(n)
		}
	}
	return out, nil
}
func (u *ScreenUpdate) UnmarshalJSON(data []byte) error {
	raw, e := DecodeScreenObject(data, "sequence", "zones")
	if e != nil {
		return e
	}
	value, e := parseScreenUpdate(raw)
	if e == nil {
		*u = value
	}
	return e
}
func validScreenUpdate(value ScreenUpdate) bool {
	if value.Sequence == 0 || value.Sequence > maximumScreenSequence {
		return false
	}
	for _, rgb := range value.Zones {
		for _, v := range rgb {
			if v < 0 || v > 255 {
				return false
			}
		}
	}
	return true
}
func ValidScreenID(id string) bool {
	if len(id) != 64 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// A broker owns one in-memory capability. Only its latest RGB frame is written to tmpfs.
// A restart cannot recover an old capability; RegisterScreenLEDHandlers explicitly resets the file.
type ScreenStore struct {
	mu                sync.Mutex
	path              string
	now               func() time.Time
	random            io.Reader
	save              func(string, any) error
	id                string
	sequence          uint64
	updated, deadline time.Time
	waiting           bool
	changed           chan struct{}
}

func NewScreenStore(path string) *ScreenStore {
	return &ScreenStore{path: path, now: time.Now, random: rand.Reader, save: config.Atomic, changed: make(chan struct{})}
}

var DefaultScreenStore = NewScreenStore(ScreenFramePath)

func (s *ScreenStore) clearLocked() error {
	if e := screenPath(s.path, false); e != nil {
		return e
	}
	if e := os.Remove(s.path); e != nil && !os.IsNotExist(e) {
		return e
	}
	s.id = ""
	s.sequence = 0
	s.updated = time.Time{}
	s.deadline = time.Time{}
	s.waiting = false
	close(s.changed)
	s.changed = make(chan struct{})
	return nil
}
func (s *ScreenStore) Reset() error { s.mu.Lock(); defer s.mu.Unlock(); return s.clearLocked() }
func (s *ScreenStore) Begin() (ScreenSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if s.id != "" && !now.Before(s.updated) && now.Before(s.deadline) {
		return ScreenSession{}, screenError(409, "SCREEN_LED_IN_USE", "別の画面連動が動作中です。停止してから開始してください")
	}
	if e := s.clearLocked(); e != nil {
		return ScreenSession{}, e
	}
	token := make([]byte, 32)
	if _, e := io.ReadFull(s.random, token); e != nil {
		return ScreenSession{}, e
	}
	s.id = hex.EncodeToString(token)
	s.updated = now
	s.deadline = now.Add(ScreenIdleTimeout)
	return ScreenSession{s.id, int(ScreenFrameInterval / time.Millisecond), int(ScreenIdleTimeout / time.Millisecond)}, nil
}
func (s *ScreenStore) checkLocked(id string, now time.Time) error {
	if !ValidScreenID(id) || id != s.id {
		return screenError(404, "SCREEN_SESSION_NOT_FOUND", "画面連動セッションがありません。再度開始してください")
	}
	if now.Before(s.updated) || !now.Before(s.deadline) {
		return screenError(410, "SCREEN_SESSION_EXPIRED", "画面連動がタイムアウトしました。再度開始してください")
	}
	return nil
}

// Update waits at most one frame interval instead of rejecting harmless browser/network jitter.
// Only one waiting request is allowed; shutdown/reset wakes that request immediately.
func (s *ScreenStore) Update(ctx context.Context, id string, value ScreenUpdate) error {
	if !validScreenUpdate(value) {
		return screenError(400, "INVALID_SCREEN_FRAME", "RGB値と連番を確認してください")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.checkLocked(id, s.now()); e != nil {
		return e
	}
	if value.Sequence <= s.sequence {
		return screenError(409, "SCREEN_SEQUENCE_STALE", "画面連動の連番が古くなっています")
	}
	if s.waiting {
		return screenError(409, "SCREEN_UPDATE_BUSY", "画面連動を更新中です")
	}
	if !s.updated.IsZero() && s.sequence > 0 {
		if delay := s.updated.Add(ScreenFrameInterval).Sub(s.now()); delay > 0 {
			s.waiting = true
			changed := s.changed
			s.mu.Unlock()
			timer := time.NewTimer(delay)
			var waitError error
			select {
			case <-ctx.Done():
				waitError = ctx.Err()
			case <-changed:
				waitError = screenError(410, "SCREEN_SESSION_EXPIRED", "画面連動が終了しました")
			case <-timer.C:
			}
			timer.Stop()
			s.mu.Lock()
			if changed == s.changed {
				s.waiting = false
			}
			if waitError != nil {
				return waitError
			}
		}
	}
	now := s.now()
	if e := s.checkLocked(id, now); e != nil {
		return e
	}
	if value.Sequence <= s.sequence {
		return screenError(409, "SCREEN_SEQUENCE_STALE", "画面連動の連番が古くなっています")
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	frame := ScreenFrame{value, now.UTC(), now.Add(ScreenIdleTimeout).UTC()}
	if e := screenPath(s.path, true); e != nil {
		return e
	}
	if e := s.save(s.path, frame); e != nil {
		return e
	}
	s.sequence = value.Sequence
	s.updated = now
	s.deadline = now.Add(ScreenIdleTimeout)
	return nil
}
func (s *ScreenStore) End(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !ValidScreenID(id) || id != s.id {
		return screenError(404, "SCREEN_SESSION_NOT_FOUND", "画面連動セッションがありません")
	}
	return s.clearLocked()
}

// Reject symlinks/special files, including an ancestor directory. The production path is fixed.
func screenPath(path string, create bool) error {
	parent := filepath.Dir(path)
	for current := parent; ; current = filepath.Dir(current) {
		if st, e := os.Lstat(current); e == nil {
			if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
				return errors.New("unsafe screen runtime directory")
			}
		} else if !os.IsNotExist(e) {
			return e
		}
		if filepath.Dir(current) == current || current == "." {
			break
		}
	}
	if create {
		if e := os.MkdirAll(parent, 0700); e != nil {
			return e
		}
	}
	if st, e := os.Lstat(path); e == nil {
		if !st.Mode().IsRegular() || st.Size() > 4096 || st.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe screen runtime file")
		}
		if e = screenFilePermissions(st); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	return nil
}

// ReadScreenFrame never changes files. Missing/expired frames mean "use normal settings".
// Any error likewise must cause the worker to restore normal mode, never replay a bad frame.
func ReadScreenFrame(path string, now time.Time) (ScreenFrame, bool, error) {
	var frame ScreenFrame
	if e := screenPath(path, false); e != nil {
		return frame, false, e
	}
	f, e := os.Open(path)
	if os.IsNotExist(e) {
		return frame, false, nil
	}
	if e != nil {
		return frame, false, e
	}
	defer f.Close()
	data, e := io.ReadAll(io.LimitReader(f, 4097))
	if e != nil || len(data) > 4096 {
		return frame, false, errors.New("invalid screen runtime size")
	}
	raw, e := DecodeScreenObject(data, "sequence", "zones", "updatedAt", "expiresAt")
	if e != nil {
		return frame, false, e
	}
	value, e := parseScreenUpdate(raw)
	if e != nil {
		return frame, false, e
	}
	frame.ScreenUpdate = value
	if e = json.Unmarshal(raw["updatedAt"], &frame.UpdatedAt); e != nil {
		return frame, false, e
	}
	if e = json.Unmarshal(raw["expiresAt"], &frame.ExpiresAt); e != nil {
		return frame, false, e
	}
	if frame.UpdatedAt.IsZero() || frame.ExpiresAt.Sub(frame.UpdatedAt) != ScreenIdleTimeout {
		return frame, false, errors.New("invalid screen lease")
	}
	if now.Before(frame.UpdatedAt) || !now.Before(frame.ExpiresAt) {
		return frame, false, nil
	}
	return frame, true, nil
}
