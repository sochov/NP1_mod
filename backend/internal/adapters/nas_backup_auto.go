package adapters

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"

	coreconfig "github.com/np1-control-center/ncc/internal/config"
)

type NASAutomaticState struct {
	State      string    `json:"state"`
	Code       string    `json:"code,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	Paused     bool      `json:"paused"`
	ObservedAt time.Time `json:"observedAt"`
}
type nasAutomaticDisk struct {
	Paused    map[string]bool `json:"paused"`
	Completed map[string]bool `json:"completed"`
}

func (c *nasBackupController) loadAutomatic() {
	c.paused = map[string]bool{}
	c.completed = map[string]bool{}
	c.automatic = map[string]NASAutomaticState{}
	c.nextTry = map[string]time.Time{}
	filename := filepath.Join(c.directory, "automatic.json")
	info, err := os.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 5*1024*1024 {
		return
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		return
	}
	var data nasAutomaticDisk
	if json.Unmarshal(raw, &data) != nil || len(data.Completed) > 50000 {
		return
	}
	for _, kind := range []string{"video", "event"} {
		c.paused[kind] = data.Paused[kind]
	}
	for key, value := range data.Completed {
		if len(key) != 64 {
			continue
		}
		if _, err := hex.DecodeString(key); err == nil && value {
			c.completed[key] = true
		}
	}
}
func (c *nasBackupController) saveAutomatic() {
	_ = coreconfig.Atomic(filepath.Join(c.directory, "automatic.json"), nasAutomaticDisk{c.paused, c.completed})
}
func recordingVersionHash(id string, size int64, modified time.Time) string {
	sum := sha256.Sum256([]byte(id + "|" + strconv.FormatInt(size, 10) + "|" + strconv.FormatInt(modified.UnixNano(), 10)))
	return hex.EncodeToString(sum[:])
}
func (index *RecordingIndex) recordingVersion(id string) (string, error) {
	index.mu.Lock()
	defer index.mu.Unlock()
	relative, err := recordingPath(id, index.token)
	if err != nil {
		return "", errors.New("SD_CHANGED")
	}
	file, ok := index.files[relative]
	if !ok {
		return "", errors.New("RECORDING_CHANGED")
	}
	return recordingVersionHash(id, file.size, file.modified), nil
}
func nasCompletionKey(target, kind, version string) string {
	sum := sha256.Sum256([]byte(target + "|" + kind + "|" + version))
	return hex.EncodeToString(sum[:])
}
func (c *nasBackupController) markCompleted(key string) {
	if key == "" {
		return
	}
	if len(c.completed) >= 50000 {
		for old := range c.completed {
			delete(c.completed, old)
			break
		}
	}
	c.completed[key] = true
	c.saveAutomatic()
}
func (c *nasBackupController) setAutomatic(kind, state, code string) {
	reason := ""
	switch state {
	case "disabled":
		reason = "自動転送はOFFです"
	case "paused":
		reason = "手動で一時停止しています。転送を再開するか、設定を保存すると再開します"
	case "idle":
		reason = "新しい確定録画を待っています"
	case "running":
		reason = "確定した録画を転送しています"
	case "waiting":
		_, _, reason = NASBackupErrorStatus(errors.New(code))
	}
	c.automatic[kind] = NASAutomaticState{state, code, reason, c.paused[kind], time.Now().UTC()}
}
func (c *nasBackupController) automaticOnce(ctx context.Context) {
	c.mu.Lock()
	if c.busy() {
		if c.job != nil && c.job.Active {
			c.setAutomatic(c.job.Kind, "running", "")
		}
		c.mu.Unlock()
		return
	}
	policies, err := c.policies()
	if err != nil {
		c.mu.Unlock()
		return
	}
	view := policies.View()
	enabled := false
	for kind, policy := range view.Policies {
		switch {
		case !policy.Enabled:
			c.setAutomatic(kind, "disabled", "")
		case c.paused[kind]:
			c.setAutomatic(kind, "paused", "")
		case time.Now().Before(c.nextTry[kind]):
			c.setAutomatic(kind, "waiting", "NAS_RETRY_WAIT")
		default:
			enabled = true
			c.setAutomatic(kind, "idle", "")
		}
	}
	c.mu.Unlock()
	if !enabled {
		return
	}
	catalog, err := c.catalog(ctx)
	if err != nil {
		c.mu.Lock()
		for kind, policy := range view.Policies {
			if policy.Enabled && !c.paused[kind] {
				c.setAutomatic(kind, "waiting", "NO_SD")
			}
		}
		c.mu.Unlock()
		return
	}
	for _, clip := range catalog.Clips {
		kind := ""
		switch clip.Kind {
		case "normal":
			kind = "video"
		case "event":
			kind = "event"
		default:
			continue
		}
		policy := view.Policies[kind]
		if !policy.Enabled {
			continue
		}
		cfg, err := policies.Target(kind, policy.Route)
		if err != nil {
			continue
		}
		version, err := c.version(clip.ID)
		if err != nil {
			continue
		}
		key := nasCompletionKey(cfg.TargetKey(), kind, version)
		c.mu.Lock()
		skip := c.paused[kind] || c.completed[key] || time.Now().Before(c.nextTry[kind])
		c.mu.Unlock()
		if skip {
			continue
		}
		binding, err := c.prepare(ctx, nasRouteRule{route: policy.Route, timing: policy.Timing, ssid: policy.SSID}, cfg.Target())
		if err == nil {
			err = binding.check(ctx)
		}
		if err != nil {
			c.mu.Lock()
			_, code, _ := NASBackupErrorStatus(err)
			c.setAutomatic(kind, "waiting", code)
			c.nextTry[kind] = time.Now().Add(5 * time.Second)
			c.mu.Unlock()
			continue
		}
		if _, err = c.startJob(policies.Revision(), clip.ID, true); err == nil {
			return
		}
		if ctx.Err() != nil {
			return
		}
	}
}

// RunNASBackupScheduler queues one finalized original at a time. It never
// starts until a policy is explicitly enabled, and never deletes SD files.
func RunNASBackupScheduler(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			nasBackups.automaticOnce(ctx)
		}
	}
}
