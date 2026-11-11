package adapters

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	coreconfig "github.com/np1-control-center/ncc/internal/config"
	"github.com/np1-control-center/ncc/internal/model"
	"github.com/np1-control-center/ncc/internal/nasbackup"
)

const NASBackupDirectory = "/overlay/owner-apps/np1-control/private/nas"

type NASBackupSettings struct {
	Revision int `json:"revision"`
	nasbackup.TargetView
	Configured         bool                            `json:"configured"`
	UsernameConfigured bool                            `json:"usernameConfigured"`
	PasswordConfigured bool                            `json:"passwordConfigured"`
	Policies           map[string]nasbackup.PolicyView `json:"policies"`
}
type NASConnectionState struct {
	Status     string    `json:"status"`
	Code       string    `json:"code,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	ObservedAt time.Time `json:"observedAt"`
}
type NASBackupJob struct {
	ID            string             `json:"id"`
	RecordingID   string             `json:"recordingID"`
	Kind          string             `json:"kind"`
	Route         string             `json:"route"`
	OriginalName  string             `json:"originalName"`
	Status        string             `json:"status"`
	Active        bool               `json:"active"`
	CanCancel     bool               `json:"canCancel"`
	Progress      nasbackup.Progress `json:"progress"`
	Result        *nasbackup.Result  `json:"result,omitempty"`
	Code          string             `json:"code,omitempty"`
	Reason        string             `json:"reason,omitempty"`
	StartedAt     time.Time          `json:"startedAt"`
	UpdatedAt     time.Time          `json:"updatedAt"`
	completionKey string
}
type NASBackupData struct {
	Settings    NASBackupSettings                        `json:"settings"`
	Connection  NASConnectionState                       `json:"connection"`
	Job         *NASBackupJob                            `json:"job,omitempty"`
	Connections map[string]map[string]NASConnectionState `json:"connections"`
	Automatic   map[string]NASAutomaticState             `json:"automatic"`
}
type nasRemote interface {
	nasbackup.Remote
	Close() error
}
type nasRecording struct {
	file  *os.File
	clip  RecordingClip
	close func()
}
type nasBackupController struct {
	mu          sync.Mutex
	directory   string
	connection  NASConnectionState
	job         *NASBackupJob
	testing     bool
	cancel      context.CancelFunc
	done        chan struct{}
	open        func(context.Context, string) (nasRecording, error)
	connect     func(context.Context, nasbackup.Config, nasbackup.TransportOptions) (nasRemote, error)
	prepare     func(context.Context, nasRouteRule, nasbackup.TargetView) (*nasRouteBinding, error)
	connections map[string]map[string]NASConnectionState
	paused      map[string]bool
	completed   map[string]bool
	automatic   map[string]NASAutomaticState
	nextTry     map[string]time.Time
	catalog     func(context.Context) (RecordingCatalog, error)
	version     func(string) (string, error)
}

func newNASBackupController(directory string) *nasBackupController {
	c := &nasBackupController{directory: directory, connection: NASConnectionState{Status: "unknown"}, connect: func(ctx context.Context, cfg nasbackup.Config, options nasbackup.TransportOptions) (nasRemote, error) {
		return nasbackup.ConnectWithTransport(ctx, cfg, options)
	}, prepare: prepareNASRoute, connections: map[string]map[string]NASConnectionState{"video": {}, "event": {}}}
	c.open = func(ctx context.Context, id string) (nasRecording, error) {
		root, file, clip, err := recordings.Open(ctx, id)
		if err != nil {
			return nasRecording{}, err
		}
		return nasRecording{file, clip, func() { _ = file.Close(); _ = root.Close() }}, nil
	}
	c.catalog = func(ctx context.Context) (RecordingCatalog, error) { return recordings.Catalog(ctx, "") }
	c.version = recordings.recordingVersion
	c.loadAutomatic()
	filename := filepath.Join(directory, "job.json")
	if info, err := os.Lstat(filename); err == nil && info.Mode().IsRegular() && info.Size() <= 16384 {
		if raw, err := os.ReadFile(filename); err == nil {
			var job NASBackupJob
			if json.Unmarshal(raw, &job) == nil && len(job.ID) == 32 && len(job.RecordingID) <= 1024 {
				if job.Active {
					if release, lockErr := nasbackup.ClaimController(directory); lockErr == nil {
						release()
						job.Active = false
						job.Status = "interrupted"
						job.Code = "NAS_INTERRUPTED"
						job.Reason = "前回の転送が中断されました。同じ録画から再開できます"
					} else {
						job.Status = "running-elsewhere"
						job.Reason = "別のプロセスが転送を管理しています"
					}
				}
				c.job = &job
			}
		}
	}
	return c
}

var nasBackups = newNASBackupController(NASBackupDirectory)

func (c *nasBackupController) config() (nasbackup.Config, error) {
	filename := filepath.Join(c.directory, "config.json")
	if _, err := os.Lstat(filename); os.IsNotExist(err) {
		return nasbackup.Config{}, nil
	}
	return nasbackup.LoadConfig(filename)
}
func (c *nasBackupController) policies() (nasbackup.Policies, error) {
	p, err := nasbackup.LoadPolicies(filepath.Join(c.directory, "policies.json"))
	if os.IsNotExist(err) {
		cfg, e := c.config()
		if e != nil {
			return nasbackup.Policies{}, e
		}
		return nasbackup.LegacyPolicies(cfg), nil
	}
	return p, err
}
func (c *nasBackupController) refreshExternalJob() {
	if c.cancel != nil {
		return
	}
	filename := filepath.Join(c.directory, "job.json")
	info, err := os.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16384 {
		return
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		return
	}
	var job NASBackupJob
	if json.Unmarshal(raw, &job) != nil || len(job.ID) != 32 || len(job.RecordingID) > 1024 {
		return
	}
	if _, err = hex.DecodeString(job.ID); err != nil {
		return
	}
	if job.Active {
		if release, lockErr := nasbackup.ClaimController(c.directory); lockErr == nil {
			release()
			job.Active = false
			job.Status = "interrupted"
			job.Code = "NAS_INTERRUPTED"
			job.Reason = "前回の転送が中断されました。同じ録画から再開できます"
		} else {
			job.Status = "running-elsewhere"
			job.CanCancel = false
			job.Reason = "別のプロセスが転送を管理しています"
		}
	}
	c.job = &job
}
func (c *nasBackupController) busy() bool {
	c.refreshExternalJob()
	return c.testing || c.job != nil && c.job.Active
}
func (c *nasBackupController) data() (NASBackupData, error) {
	c.refreshExternalJob()
	cfg, err := c.config()
	if err != nil {
		return NASBackupData{}, err
	}
	data := c.snapshot(cfg)
	policies, err := c.policies()
	if err != nil {
		return NASBackupData{}, err
	}
	view := policies.View()
	data.Settings.Revision = view.Revision
	data.Settings.Policies = view.Policies
	return data, nil
}
func (c *nasBackupController) snapshot(cfg nasbackup.Config) NASBackupData {
	view := cfg.View()
	data := NASBackupData{Settings: NASBackupSettings{Revision: cfg.Revision(), TargetView: cfg.Target(), Configured: view.Configured, UsernameConfigured: view.UsernameConfigured, PasswordConfigured: view.PasswordConfigured}, Connection: c.connection, Connections: map[string]map[string]NASConnectionState{}}
	data.Automatic = map[string]NASAutomaticState{}
	for kind, state := range c.automatic {
		data.Automatic[kind] = state
	}
	for kind, routes := range c.connections {
		copy := map[string]NASConnectionState{}
		for route, state := range routes {
			copy[route] = state
		}
		data.Connections[kind] = copy
	}
	if c.job != nil {
		copy := *c.job
		copy.CanCancel = copy.Active && c.cancel != nil
		if copy.Result != nil {
			r := *copy.Result
			copy.Result = &r
		}
		data.Job = &copy
	}
	return data
}
func (c *nasBackupController) status() model.Observation {
	c.mu.Lock()
	defer c.mu.Unlock()
	value, err := c.data()
	if err != nil {
		return model.Missing("NAS backup private controller", "NAS設定を読み取れません")
	}
	return model.Value("NAS backup private controller", value)
}
func (c *nasBackupController) saveJob() error {
	if c.job == nil {
		return nil
	}
	return coreconfig.Atomic(filepath.Join(c.directory, "job.json"), c.job)
}
func (c *nasBackupController) save(expected int, input nasbackup.ConfigInput) (model.Observation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.busy() {
		return model.Observation{}, errors.New("NAS_BUSY")
	}
	if _, err := nasbackup.UpdateConfig(filepath.Join(c.directory, "config.json"), expected, input); err != nil {
		return model.Observation{}, err
	}
	c.connection = NASConnectionState{Status: "unknown"}
	value, err := c.data()
	if err != nil {
		return model.Observation{}, err
	}
	return model.Value("NAS backup private controller", value), nil
}
func (c *nasBackupController) test(ctx context.Context, expected int) (model.Observation, error) {
	return c.testPolicy(ctx, expected, "video", "")
}
func (c *nasBackupController) start(expected int, id string) (model.Observation, error) {
	return c.startJob(expected, id, false)
}
func (c *nasBackupController) startJob(expected int, id string, automatic bool) (model.Observation, error) {
	if id == "" || len(id) > 1024 {
		return model.Observation{}, errors.New("NAS_RECORDING_ID_INVALID")
	}
	c.mu.Lock()
	if c.busy() {
		c.mu.Unlock()
		return model.Observation{}, errors.New("NAS_BUSY")
	}
	policies, err := c.policies()
	if err != nil {
		c.mu.Unlock()
		return model.Observation{}, err
	}
	if policies.Revision() != expected {
		c.mu.Unlock()
		return model.Observation{}, errors.New("NAS_REVISION_CONFLICT")
	}
	release, err := nasbackup.ClaimController(c.directory)
	if err != nil {
		c.mu.Unlock()
		return model.Observation{}, err
	}
	fail := func(err error) (model.Observation, error) { release(); c.mu.Unlock(); return model.Observation{}, err }
	current, err := c.policies()
	if err != nil {
		return fail(err)
	}
	if current.Revision() != expected {
		return fail(errors.New("NAS_REVISION_CONFLICT"))
	}
	openCtx, cancelOpen := context.WithTimeout(context.Background(), 5*time.Second)
	recording, err := c.open(openCtx, id)
	cancelOpen()
	if err != nil {
		return fail(err)
	}
	kind := ""
	switch recording.clip.Kind {
	case "normal":
		kind = "video"
	case "event":
		kind = "event"
	default:
		recording.close()
		return fail(errors.New("NAS_POLICY_KIND_INVALID"))
	}
	policy, err := policies.Policy(kind)
	if err != nil {
		recording.close()
		return fail(err)
	}
	if automatic && !policy.Enabled {
		recording.close()
		return fail(errors.New("NAS_POLICY_DISABLED"))
	}
	cfg, err := policies.Target(kind, policy.Route)
	if err != nil {
		recording.close()
		return fail(err)
	}
	rule := nasRouteRule{route: policy.Route, timing: policy.Timing, ssid: policy.SSID}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		recording.close()
		return fail(errors.New("NAS_JOB_ID_UNAVAILABLE"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	now := time.Now().UTC()
	c.job = &NASBackupJob{ID: hex.EncodeToString(random[:]), RecordingID: id, Kind: kind, Route: policy.Route, OriginalName: recording.clip.Name, Status: "starting", Active: true, StartedAt: now, UpdatedAt: now, Progress: nasbackup.Progress{Stage: "starting", Bytes: "0", TotalBytes: recording.clip.SizeBytes, ResumedBytes: "0"}}
	if info, e := recording.file.Stat(); e == nil {
		c.job.completionKey = nasCompletionKey(cfg.TargetKey(), kind, recordingVersionHash(id, info.Size(), info.ModTime()))
	}
	if !automatic {
		c.paused[kind] = false
		c.nextTry[kind] = time.Time{}
		c.saveAutomatic()
	}
	c.cancel = cancel
	c.done = make(chan struct{})
	jobID, done := c.job.ID, c.done
	if err = c.saveJob(); err != nil {
		c.job.Active = false
		c.job.Status = "failed"
		cancel()
		c.cancel = nil
		recording.close()
		close(done)
		return fail(errors.New("NAS_STATE_WRITE_FAILED"))
	}
	value := c.snapshot(cfg)
	view := policies.View()
	value.Settings.Revision, value.Settings.Policies = view.Revision, view.Policies
	c.mu.Unlock()
	go c.run(ctx, cfg, recording, jobID, done, release, rule, cancel)
	return model.Value("NAS backup private controller", value), nil
}
func (c *nasBackupController) run(ctx context.Context, cfg nasbackup.Config, recording nasRecording, id string, done chan struct{}, release func(), rule nasRouteRule, cancel context.CancelFunc) {
	defer close(done)
	defer release()
	defer recording.close()
	var result nasbackup.Result
	binding, err := c.prepare(ctx, rule, cfg.Target())
	var remote nasRemote
	if err == nil {
		go binding.monitor(ctx, cancel, func(error) {
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.job != nil && c.job.ID == id && c.job.Active {
				c.job.Code = "NAS_ROUTE_CHANGED"
			}
		})
		remote, err = c.connect(ctx, cfg, binding.transport())
	}
	if err == nil {
		defer remote.Close()
		var engine *nasbackup.Engine
		engine, err = nasbackup.NewEngine(filepath.Join(c.directory, "state"), remote)
		if err == nil {
			lastSave := time.Time{}
			lastStage := ""
			result, err = engine.BackupWithProgress(ctx, recording.file, func(progress nasbackup.Progress) {
				c.mu.Lock()
				defer c.mu.Unlock()
				if c.job == nil || c.job.ID != id {
					return
				}
				c.job.Progress = progress
				if c.job.Status != "cancelling" {
					c.job.Status = progress.Stage
				}
				c.job.UpdatedAt = time.Now().UTC()
				if progress.Stage != lastStage || time.Since(lastSave) > time.Second {
					_ = c.saveJob()
					lastSave = time.Now()
					lastStage = progress.Stage
				}
			})
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.job == nil || c.job.ID != id {
		return
	}
	c.job.Active = false
	c.job.UpdatedAt = time.Now().UTC()
	c.job.Result = &result
	if err == nil && (result.Status == "complete" || result.Status == "already-complete") {
		c.job.Status = result.Status
		c.job.Code = ""
		c.job.Reason = ""
		c.markCompleted(c.job.completionKey)
	} else if ctx.Err() != nil && c.job.Code == "NAS_ROUTE_CHANGED" {
		c.job.Status = "interrupted"
		c.job.Reason = "\u9078\u629e\u3057\u305f\u901a\u4fe1\u7d4c\u8def\u304c\u5207\u308a\u66ff\u308f\u3063\u305f\u305f\u3081\u8ee2\u9001\u3092\u4e2d\u65ad\u3057\u307e\u3057\u305f"
	} else if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		c.job.Status = "failed"
		c.job.Code = "NAS_TIMEOUT"
		c.job.Reason = "転送が時間内に完了しませんでした。途中データを確認して再開できます"
	} else if ctx.Err() != nil {
		c.job.Status = "cancelled"
		c.job.Code = "NAS_CANCELLED"
		c.job.Reason = "転送を中断しました。検証済みの途中データから再開できます"
	} else {
		c.job.Status = "failed"
		_, c.job.Code, c.job.Reason = NASBackupErrorStatus(err)
		c.nextTry[c.job.Kind] = time.Now().Add(time.Minute)
	}
	_ = c.saveJob()
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
}
func (c *nasBackupController) cancelJob(id string) (model.Observation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.job == nil || c.job.ID != id {
		return model.Observation{}, errors.New("NAS_JOB_NOT_FOUND")
	}
	if !c.job.Active || c.cancel == nil {
		return model.Observation{}, errors.New("NAS_JOB_NOT_ACTIVE")
	}
	c.job.Status = "cancelling"
	c.job.Code = "NAS_CANCELLED"
	c.paused[c.job.Kind] = true
	c.saveAutomatic()
	c.cancel()
	_ = c.saveJob()
	value, err := c.data()
	if err != nil {
		return model.Observation{}, err
	}
	return model.Value("NAS backup private controller", value), nil
}
func NASBackupStatus() model.Observation { return nasBackups.status() }
func SaveNASBackup(expected int, input nasbackup.ConfigInput) (model.Observation, error) {
	return nasBackups.save(expected, input)
}
func TestNASBackup(ctx context.Context, expected int) (model.Observation, error) {
	return nasBackups.test(ctx, expected)
}
func StartNASBackup(expected int, recordingID string) (model.Observation, error) {
	return nasBackups.start(expected, recordingID)
}
func CancelNASBackup(id string) (model.Observation, error) { return nasBackups.cancelJob(id) }
func ShutdownNASBackup(ctx context.Context) error {
	nasBackups.mu.Lock()
	if !nasBackups.busy() || nasBackups.cancel == nil {
		nasBackups.mu.Unlock()
		return nil
	}
	nasBackups.cancel()
	done := nasBackups.done
	nasBackups.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func NASBackupErrorStatus(err error) (int, string, string) {
	code := "NAS_OPERATION_FAILED"
	if err != nil && (strings.HasPrefix(err.Error(), "NAS_") || err.Error() == "SD_CHANGED" || err.Error() == "NO_SD" || strings.HasPrefix(err.Error(), "RECORDING_")) {
		code = err.Error()
	}
	status, reason := 503, "NASへの操作を完了できませんでした。接続先と通信状態を確認してください"
	switch code {
	case "NAS_BUSY":
		status, reason = 409, "接続確認または転送が進行中です"
	case "NAS_REVISION_CONFLICT":
		status, reason = 409, "NAS設定が更新されました。表示を更新してください"
	case "NAS_CONFIG_INVALID", "NAS_RECORDING_ID_INVALID":
		status, reason = 400, "NAS設定または録画の指定を確認してください"
	case "NAS_NOT_CONFIGURED":
		status, reason = 409, "先にNASの接続先と認証情報を保存してください"
	case "SD_CHANGED", "NO_SD", "RECORDING_CHANGED", "RECORDING_NOT_FINALIZED", "RECORDING_UNAVAILABLE", "NAS_ORIGINAL_MEDIA_UNCONFIRMED":
		status, reason = 409, "SDカードまたは確定録画を確認できません。録画一覧を更新してください"
	case "NAS_JOB_NOT_FOUND", "NAS_JOB_NOT_ACTIVE":
		status, reason = 409, "この転送は既に終了したか、更新されています"
	case "NAS_PART_NOT_OWNED", "NAS_READBACK_MISMATCH", "NAS_FINALIZE_CONFLICT", "NAS_STATE_CONFLICT":
		status, reason = 409, "転送先の途中データまたは同名ファイルを確認できないため、上書きせず停止しました"
	}
	return status, code, reason
}
