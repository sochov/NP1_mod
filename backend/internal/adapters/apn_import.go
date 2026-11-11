package adapters

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"time"

	"github.com/np1-control-center/ncc/internal/modem"
)

type APNEditorView struct {
	APNSettingsView
	ConfiguredProfile1      *modem.ConfiguredAPNProfile1 `json:"configuredProfile1"`
	ConfiguredProfileReason string                       `json:"configuredProfileReason,omitempty"`
}

// APNEditorSettings performs file reads only. Seeding and modem queries are
// explicit root maintenance actions and are never triggered by a browser GET.
func APNEditorSettings() (APNEditorView, error) {
	view, e := APNSettings()
	if e != nil {
		return APNEditorView{}, e
	}
	out := APNEditorView{APNSettingsView: view}
	saved, e := modem.ReadConfiguredAPNProfile1()
	if e == nil {
		out.ConfiguredProfile1 = &saved
	} else {
		out.ConfiguredProfileReason = "本体の保存プロファイルはまだ取得していません"
	}
	return out, nil
}

type apnSeedLoader func(func(modem.ConfiguredAPNProfile1, string, string) error) error

// SeedAPNEditorFromRecovery never changes a modem profile. Existing owner
// settings, including revision zero, are preserved without replacement.
func SeedAPNEditorFromRecovery() (bool, error) {
	_, existsError := os.Lstat(filepath.Join(APNDirectory, "config.json"))
	exists := existsError == nil
	if e := modem.RebuildProfileRecoveryIndex(); e != nil {
		if exists {
			return false, nil
		}
		return false, e
	}
	if exists {
		return false, nil
	}
	// The persisted proof remains usable after the one-shot runtime is gone.
	_ = modem.ImportProfileInventoryProof()
	return apnSettings.seedFromRecovery(modem.WithSavedProfile1ForEditor)
}
func (s *APNStore) seedFromRecovery(load apnSeedLoader) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	configPath := filepath.Join(s.directory, "config.json")
	if _, e := os.Lstat(configPath); e == nil {
		_, e = s.load()
		return false, e
	} else if !errors.Is(e, os.ErrNotExist) {
		return false, errors.New("APN_PRIVATE_CONFIG_INVALID")
	}
	var next apnFile
	if e := load(func(source modem.ConfiguredAPNProfile1, username, password string) error {
		if source.Source != "gobinet-wds-saved-profile" || source.Current || source.Status != "available" || !source.Profile2Known || !source.Profile2Absent || source.ObservedAt.IsZero() {
			return errors.New("APN_SEED_SOURCE_UNVERIFIED")
		}
		next = apnDefaults()
		next.Revision = 1
		next.Profiles[0] = apnFileProfile{ID: "profile1", Enabled: true, APN: source.APN, Auth: source.Auth, Username: username, Password: password}
		at := time.Now().UTC()
		next.SavedAt = &at
		return validateAPNProfiles(next.Profiles)
	}); e != nil {
		return false, e
	}
	if next.Revision != 1 {
		return false, errors.New("APN_SEED_SOURCE_EMPTY")
	}
	if e := os.MkdirAll(s.directory, 0700); e != nil {
		return false, errors.New("APN_CONFIG_WRITE_FAILED")
	}
	for current := filepath.Clean(s.directory); ; current = filepath.Dir(current) {
		info, e := os.Lstat(current)
		if e != nil || info.Mode()&os.ModeSymlink != 0 {
			return false, errors.New("APN_PRIVATE_DIRECTORY_INVALID")
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	root, e := os.OpenRoot(s.directory)
	if e != nil {
		return false, e
	}
	defer root.Close()
	directory, e := root.Stat(".")
	if e != nil || !directory.IsDir() || (runtime.GOOS == "linux" && directory.Mode().Perm() != 0700) {
		return false, errors.New("APN_PRIVATE_DIRECTORY_INVALID")
	}
	raw, e := json.Marshal(next)
	if e != nil {
		return false, errors.New("APN_CONFIG_WRITE_FAILED")
	}
	defer clear(raw)
	nonce := make([]byte, 8)
	if _, e = rand.Read(nonce); e != nil {
		return false, e
	}
	temporary := "seed-" + hex.EncodeToString(nonce) + ".tmp"
	file, e := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return false, errors.New("APN_CONFIG_WRITE_FAILED")
	}
	defer root.Remove(temporary)
	if e = file.Chmod(0600); e == nil {
		_, e = file.Write(raw)
	}
	if e == nil {
		e = file.Sync()
	}
	closed := file.Close()
	if e != nil || closed != nil {
		return false, errors.New("APN_CONFIG_WRITE_FAILED")
	}
	if e = root.Link(temporary, "config.json"); e != nil {
		if errors.Is(e, os.ErrExist) {
			return false, nil
		}
		return false, errors.New("APN_SEED_PUBLISH_FAILED")
	}
	if e = root.Remove(temporary); e != nil {
		return true, errors.New("APN_SEED_CLEANUP_FAILED")
	}
	if runtime.GOOS == "linux" {
		dir, e := root.Open(".")
		if e != nil {
			return true, e
		}
		e = dir.Sync()
		dir.Close()
		if e != nil {
			return true, e
		}
	}
	readback, e := s.load()
	if e != nil {
		return true, e
	}
	if readback.Revision == 1 && !reflect.DeepEqual(next, readback) {
		return true, errors.New("APN_SEED_READBACK_FAILED")
	}
	return true, nil
}
