package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/np1-control-center/ncc/internal/adapters"
	"github.com/np1-control-center/ncc/internal/auth"
	"github.com/np1-control-center/ncc/internal/config"
	"github.com/np1-control-center/ncc/internal/plate"
)

// Full settings backup (schema 2).
//
// Besides the display settings it carries everything the owner set up on screens: the plate
// rules and speech (with uploaded sounds), the VOICEVOX endpoint and key, and, through the
// root broker, the front LED, hotspot, NCC-added Wi-Fi, APN, NAS and uplink settings with
// their passwords. The browser adds its own layout choices. Two things are recorded but never
// written back: the stock saved Wi-Fi list (the stock service does not reveal passwords) and
// the OTA policy (a restore must not re-enable firmware updates).

// The VOICEVOX style list is a cache fetched again from VOICEVOX, and the radio logger file
// is mostly runtime counters, so neither is part of a backup.
var serverBackupFiles = []string{"plate/settings.json", "plate/speech/api.json"}

var uploadName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}\.pcm$`)

const backupUploadsMax = 8 << 20

type fullBackup struct {
	SchemaVersion int                        `json:"schemaVersion"`
	CreatedAt     time.Time                  `json:"createdAt"`
	Settings      Settings                   `json:"settings"`
	Server        map[string]json.RawMessage `json:"server"`
	Uploads       map[string]string          `json:"uploads,omitempty"`
	Owner         map[string]json.RawMessage `json:"owner"`
	Browser       map[string]string          `json:"browser,omitempty"`
	Recorded      map[string]json.RawMessage `json:"recordedOnly,omitempty"`
	Notes         []string                   `json:"notes"`
}

func (s *Server) brokerJSON(r *http.Request, method, target string, body any, out any) (int, error) {
	var reader io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return 0, e
		}
		reader = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(r.Context(), method, "http://broker"+target, reader)
	if e != nil {
		return 0, e
	}
	resp, e := s.client.Do(req)
	if e != nil {
		return 0, e
	}
	defer resp.Body.Close()
	if out != nil {
		if e = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out); e != nil {
			return resp.StatusCode, e
		}
	}
	return resp.StatusCode, nil
}

func (s *Server) exportFullBackup(w http.ResponseWriter, r *http.Request) {
	session, _ := s.auth.Get(r)
	if !auth.Allowed(session.Role, "settings") {
		fail(w, 403, "PERMISSION_DENIED", "管理者権限が必要です")
		return
	}
	b := fullBackup{SchemaVersion: 2, CreatedAt: time.Now().UTC(), Server: map[string]json.RawMessage{}, Uploads: map[string]string{}, Recorded: map[string]json.RawMessage{},
		Notes: []string{
			"パスワードやAPIキーを含みます。安全な場所に保管してください。",
			"純正側に保存されたWi-Fiはパスワードを読み出せないため、名前だけを記録しています（復元はしません）。",
			"本体の自動更新（OTA）の設定は記録のみで、復元しても有効には戻しません。",
		}}
	s.mu.Lock()
	b.Settings = s.settings
	s.mu.Unlock()
	for _, name := range serverBackupFiles {
		raw, e := os.ReadFile(filepath.Join(s.cfg.StateDir, name))
		if e == nil && json.Valid(raw) {
			b.Server[name] = raw
		}
	}
	total := 0
	if entries, e := os.ReadDir(filepath.Join(s.cfg.StateDir, "plate", "speech", "uploads")); e == nil {
		for _, entry := range entries {
			if !uploadName.MatchString(entry.Name()) {
				continue
			}
			raw, e := os.ReadFile(filepath.Join(s.cfg.StateDir, "plate", "speech", "uploads", entry.Name()))
			if e != nil || total+len(raw) > backupUploadsMax {
				continue
			}
			total += len(raw)
			b.Uploads[entry.Name()] = base64.StdEncoding.EncodeToString(raw)
		}
	}
	var owner struct {
		Files map[string]json.RawMessage `json:"files"`
	}
	if code, e := s.brokerJSON(r, "GET", "/backup/owner", nil, &owner); e != nil || code != 200 {
		fail(w, 503, "BACKUP_UNAVAILABLE", "本体の設定を読み出せません")
		return
	}
	b.Owner = owner.Files
	var wifi json.RawMessage
	if code, e := s.brokerJSON(r, "GET", "/wifi/networks", nil, &wifi); e == nil && code == 200 {
		b.Recorded["stockWifi"] = wifi
	}
	var ota json.RawMessage
	if code, e := s.brokerJSON(r, "GET", "/device/ota-policy", nil, &ota); e == nil && code == 200 {
		b.Recorded["otaPolicy"] = ota
	}
	s.audit("backup.export", "full")
	w.Header().Set("Content-Disposition", "attachment; filename=ncc-backup.json")
	jsonOut(w, 200, b)
}

type restorePlan struct {
	DryRun        bool     `json:"dryRun"`
	Settings      Settings `json:"settings"`
	ServerChanged []string `json:"serverChanged"`
	Uploads       int      `json:"uploads"`
	OwnerChanged  []string `json:"ownerChanged"`
	Restarts      []string `json:"restarts"`
	Browser       int      `json:"browser"`
}

func (s *Server) restoreFullBackup(w http.ResponseWriter, r *http.Request, raw []byte) {
	session, _ := s.auth.Get(r)
	if !auth.Allowed(session.Role, "settings") {
		fail(w, 403, "PERMISSION_DENIED", "管理者権限が必要です")
		return
	}
	var v struct {
		fullBackup
		ExpectedRevision int  `json:"expectedRevision"`
		DryRun           bool `json:"dryRun"`
	}
	if json.Unmarshal(raw, &v) != nil || v.SchemaVersion != 2 {
		fail(w, 400, "INVALID_BACKUP", "バックアップ形式を確認してください")
		return
	}
	if v.Settings.CellularLimitBytes == "" {
		v.Settings.CellularLimitBytes = "0"
	}
	if !validateSettings(v.Settings) {
		fail(w, 400, "INVALID_BACKUP", "表示設定の値を確認してください")
		return
	}
	contents, e := validateServerBackup(v.Server, v.Uploads)
	if e != nil {
		fail(w, 400, "INVALID_BACKUP", "ナンバー認識や読み上げの設定を確認してください")
		return
	}
	var owner struct {
		Changed  []string `json:"changed"`
		Restarts []string `json:"restarts"`
		Code     string   `json:"code"`
	}
	code, e := s.brokerJSON(r, "POST", "/backup/owner", map[string]any{"dryRun": true, "files": v.Owner}, &owner)
	if e != nil || code != 200 {
		fail(w, 400, "INVALID_BACKUP", "本体の設定（LED・ホットスポット・Wi-Fi・APN・NAS・回線）を確認してください")
		return
	}
	plan := restorePlan{DryRun: v.DryRun, Settings: v.Settings, OwnerChanged: owner.Changed, Restarts: owner.Restarts, Uploads: len(v.Uploads), Browser: len(v.Browser), ServerChanged: []string{}}
	for name, b := range contents {
		if old, e := os.ReadFile(filepath.Join(s.cfg.StateDir, name)); e != nil || !adapters.SameJSON(old, b) {
			plan.ServerChanged = append(plan.ServerChanged, name)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if v.ExpectedRevision != s.settings.Revision {
		fail(w, 409, "REVISION_CONFLICT", "設定が更新されています")
		return
	}
	if v.DryRun {
		jsonOut(w, 200, plan)
		return
	}
	for _, name := range plan.ServerChanged {
		path := filepath.Join(s.cfg.StateDir, name)
		if old, e := os.ReadFile(path); e == nil {
			_ = writeStateFile(path+".before-restore", old)
		}
		if e := writeStateFile(path, contents[name]); e != nil {
			fail(w, 500, "STORE_ERROR", "設定の保存に失敗しました")
			return
		}
	}
	for name, encoded := range v.Uploads {
		data, _ := base64.StdEncoding.DecodeString(encoded)
		if e := writeStateFile(filepath.Join(s.cfg.StateDir, "plate", "speech", "uploads", name), data); e != nil {
			fail(w, 500, "STORE_ERROR", "読み上げ音声の保存に失敗しました")
			return
		}
	}
	settings := v.Settings
	settings.Revision = s.settings.Revision + 1
	if e := config.Atomic(filepath.Join(s.cfg.StateDir, "settings.json"), settings); e != nil {
		fail(w, 500, "STORE_ERROR", "設定の保存に失敗しました")
		return
	}
	s.settings = settings
	// The broker writes its files and then restarts the services, this one included, so that
	// every restored setting is loaded the same way as at start.
	code, e = s.brokerJSON(r, "POST", "/backup/owner", map[string]any{"dryRun": false, "files": v.Owner, "restartWeb": len(plan.ServerChanged) > 0 || len(v.Uploads) > 0}, &owner)
	if e != nil || code != 200 {
		s.audit("backup.restore", "partial")
		fail(w, 500, "RESTORE_PARTIAL", "本体側の設定を書き込めませんでした（表示・ナンバー認識の設定は復元済み）")
		return
	}
	plan.OwnerChanged, plan.Restarts = owner.Changed, owner.Restarts
	s.audit("backup.restore", "full")
	jsonOut(w, 200, plan)
}

func validateServerBackup(files map[string]json.RawMessage, uploads map[string]string) (map[string][]byte, error) {
	allowed := map[string]bool{}
	for _, name := range serverBackupFiles {
		allowed[name] = true
	}
	out := map[string][]byte{}
	for name, raw := range files {
		if !allowed[name] || len(raw) > 256<<10 {
			return nil, errors.New("unknown file")
		}
		var object map[string]any
		if json.Unmarshal(raw, &object) != nil || object == nil {
			return nil, errors.New("not an object")
		}
		if name == "plate/settings.json" {
			var p plate.Settings
			if json.Unmarshal(raw, &p) != nil {
				return nil, errors.New("plate settings")
			}
			if _, e := plate.ValidateSettings(p); e != nil {
				return nil, e
			}
		}
		out[name] = raw
	}
	total := 0
	for name, encoded := range uploads {
		data, e := base64.StdEncoding.DecodeString(encoded)
		if !uploadName.MatchString(name) || e != nil {
			return nil, errors.New("upload")
		}
		total += len(data)
		if total > backupUploadsMax {
			return nil, errors.New("uploads too large")
		}
	}
	return out, nil
}

func writeStateFile(path string, b []byte) error {
	if e := os.MkdirAll(filepath.Dir(path), 0o700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".ncc-restore-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0o600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	if c := f.Close(); e == nil {
		e = c
	}
	if e != nil {
		return e
	}
	return os.Rename(name, path)
}
