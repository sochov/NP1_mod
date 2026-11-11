package adapters

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/config"
)

const APNDirectory = "/overlay/owner-apps/np1-control/private/apn"

type APNProfileView struct {
	ID                 string `json:"id"`
	Enabled            bool   `json:"enabled"`
	APN                string `json:"apn"`
	Auth               int    `json:"auth"`
	UsernameConfigured bool   `json:"usernameConfigured"`
	PasswordConfigured bool   `json:"passwordConfigured"`
}
type APNSettingsView struct {
	Revision        int              `json:"revision"`
	Profiles        []APNProfileView `json:"profiles"`
	SavedAt         *time.Time       `json:"savedAt"`
	AppliedRevision *int             `json:"appliedRevision"`
	AppliedAt       *time.Time       `json:"appliedAt"`
}
type APNProfileChange struct {
	ID            string  `json:"id"`
	Enabled       bool    `json:"enabled"`
	APN           string  `json:"apn"`
	Auth          int     `json:"auth"`
	Username      *string `json:"username,omitempty"`
	Password      *string `json:"password,omitempty"`
	ClearUsername bool    `json:"clearUsername,omitempty"`
	ClearPassword bool    `json:"clearPassword,omitempty"`
}

// APNSecretProfile is for the local root apply helper only. Never return it from an API.
// Secret JSON tags are deliberately absent: serializeAPNFile is the only credential encoder.
type APNSecretProfile struct {
	ID       string `json:"id"`
	Enabled  bool   `json:"enabled"`
	APN      string `json:"apn"`
	Auth     int    `json:"auth"`
	Username string `json:"-"`
	Password string `json:"-"`
}
type apnFileProfile struct {
	ID       string `json:"id"`
	Enabled  bool   `json:"enabled"`
	APN      string `json:"apn"`
	Auth     int    `json:"auth"`
	Username string `json:"username"`
	Password string `json:"password"`
}
type apnFile struct {
	Revision        int              `json:"revision"`
	Profiles        []apnFileProfile `json:"profiles"`
	SavedAt         *time.Time       `json:"savedAt"`
	AppliedRevision *int             `json:"appliedRevision"`
	AppliedAt       *time.Time       `json:"appliedAt"`
}
type APNStore struct {
	directory string
	mu        sync.Mutex
}

func NewAPNStore(directory string) *APNStore { return &APNStore{directory: directory} }

var apnSettings = NewAPNStore(APNDirectory)

func APNSettings() (APNSettingsView, error) { return apnSettings.Read() }
func SaveAPNSettings(expected int, changes []APNProfileChange) (APNSettingsView, error) {
	return apnSettings.Save(expected, changes)
}
func APNApplyProfiles(expected int) ([]APNSecretProfile, error) {
	return apnSettings.ForApply(expected)
}
func MarkAPNApplied(expected int) (APNSettingsView, error) { return apnSettings.MarkApplied(expected) }

func apnDefaults() apnFile {
	return apnFile{Profiles: []apnFileProfile{{ID: "profile1"}, {ID: "profile2"}}}
}
func apnPublic(c apnFile) APNSettingsView {
	out := APNSettingsView{c.Revision, make([]APNProfileView, 0, 2), c.SavedAt, c.AppliedRevision, c.AppliedAt}
	for _, p := range c.Profiles {
		out.Profiles = append(out.Profiles, APNProfileView{p.ID, p.Enabled, p.APN, p.Auth, p.Username != "", p.Password != ""})
	}
	return out
}
func validAPNName(value string) bool {
	if len(value) < 1 || len(value) > 49 {
		return false
	}
	for _, part := range strings.Split(value, ".") {
		if part == "" || part[0] == '-' || part[len(part)-1] == '-' {
			return false
		}
		for _, c := range []byte(part) {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// The stock library may build AT and quectel-CM command text. Accept only a safe
// ASCII token until its quoting rules have been proven; never silently escape it.
func validAPNCredential(value string) bool {
	if len(value) > 49 || strings.HasPrefix(value, "-") {
		return false
	}
	for _, c := range []byte(value) {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-@:+%=", rune(c))) {
			return false
		}
	}
	return true
}
func validateAPNProfiles(profiles []apnFileProfile) error {
	if len(profiles) != 2 || profiles[0].ID != "profile1" || profiles[1].ID != "profile2" {
		return errors.New("APN_PROFILES_INVALID")
	}
	for _, p := range profiles {
		if p.Auth < 0 || p.Auth > 3 {
			return errors.New("APN_AUTH_INVALID")
		}
		if (p.Enabled || p.APN != "") && !validAPNName(p.APN) {
			return errors.New("APN_NAME_INVALID")
		}
		if !validAPNCredential(p.Username) || !validAPNCredential(p.Password) {
			return errors.New("APN_CREDENTIAL_CHARACTERS_INVALID")
		}
	}
	return nil
}
func (s *APNStore) load() (apnFile, error) {
	path := filepath.Join(s.directory, "config.json")
	st, e := os.Lstat(path)
	if os.IsNotExist(e) {
		return apnDefaults(), nil
	}
	if e != nil {
		return apnFile{}, errors.New("APN_CONFIG_READ_FAILED")
	}
	if !st.Mode().IsRegular() || st.Size() > 8192 || (runtime.GOOS == "linux" && st.Mode().Perm()&0077 != 0) {
		return apnFile{}, errors.New("APN_PRIVATE_CONFIG_INVALID")
	}
	raw, e := os.ReadFile(path)
	if e != nil || len(raw) > 8192 {
		return apnFile{}, errors.New("APN_CONFIG_READ_FAILED")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var value apnFile
	if e = decoder.Decode(&value); e != nil {
		return apnFile{}, errors.New("APN_CONFIG_INVALID")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return apnFile{}, errors.New("APN_CONFIG_INVALID")
	}
	if value.Revision < 0 {
		return apnFile{}, errors.New("APN_CONFIG_INVALID")
	}
	if e = validateAPNProfiles(value.Profiles); e != nil {
		return apnFile{}, e
	}
	return value, nil
}
func (s *APNStore) write(value apnFile) error {
	if e := os.MkdirAll(s.directory, 0700); e != nil {
		return errors.New("APN_CONFIG_WRITE_FAILED")
	}
	st, e := os.Lstat(s.directory)
	if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("APN_PRIVATE_DIRECTORY_INVALID")
	}
	if e = os.Chmod(s.directory, 0700); e != nil {
		return errors.New("APN_PRIVATE_DIRECTORY_INVALID")
	}
	if e = config.Atomic(filepath.Join(s.directory, "config.json"), value); e != nil {
		return errors.New("APN_CONFIG_WRITE_FAILED")
	}
	return nil
}
func (s *APNStore) Read() (APNSettingsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, e := s.load()
	if e != nil {
		return APNSettingsView{}, e
	}
	return apnPublic(value), nil
}
func (s *APNStore) Save(expected int, changes []APNProfileChange) (APNSettingsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, e := s.load()
	if e != nil {
		return APNSettingsView{}, e
	}
	if expected != previous.Revision {
		return APNSettingsView{}, errors.New("REVISION_CONFLICT")
	}
	if len(changes) != 2 {
		return APNSettingsView{}, errors.New("APN_PROFILES_INVALID")
	}
	next := previous
	next.Profiles = append([]apnFileProfile(nil), previous.Profiles...)
	seen := map[string]bool{}
	for _, change := range changes {
		index := 0
		if change.ID == "profile2" {
			index = 1
		} else if change.ID != "profile1" {
			return APNSettingsView{}, errors.New("APN_PROFILE_INVALID")
		}
		if seen[change.ID] {
			return APNSettingsView{}, errors.New("APN_PROFILE_DUPLICATE")
		}
		seen[change.ID] = true
		p := &next.Profiles[index]
		p.Enabled = change.Enabled
		p.APN = change.APN
		p.Auth = change.Auth
		if change.ClearUsername {
			if change.Username != nil && *change.Username != "" {
				return APNSettingsView{}, errors.New("APN_CREDENTIAL_UPDATE_AMBIGUOUS")
			}
			p.Username = ""
		} else if change.Username != nil && *change.Username != "" {
			p.Username = *change.Username
		}
		if change.ClearPassword {
			if change.Password != nil && *change.Password != "" {
				return APNSettingsView{}, errors.New("APN_CREDENTIAL_UPDATE_AMBIGUOUS")
			}
			p.Password = ""
		} else if change.Password != nil && *change.Password != "" {
			p.Password = *change.Password
		}
	}
	if e = validateAPNProfiles(next.Profiles); e != nil {
		return APNSettingsView{}, e
	}
	next.Revision++
	at := time.Now().UTC()
	next.SavedAt = &at
	if e = s.write(next); e != nil {
		return APNSettingsView{}, e
	}
	return apnPublic(next), nil
}

// ForApply reads a revision-pinned private snapshot; it performs no network operation.
func (s *APNStore) ForApply(expected int) ([]APNSecretProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, e := s.load()
	if e != nil {
		return nil, e
	}
	if value.Revision != expected {
		return nil, errors.New("REVISION_CONFLICT")
	}
	out := make([]APNSecretProfile, 0, 2)
	enabled := false
	for _, p := range value.Profiles {
		enabled = enabled || p.Enabled
		out = append(out, APNSecretProfile{p.ID, p.Enabled, p.APN, p.Auth, p.Username, p.Password})
	}
	if !enabled {
		return nil, errors.New("APN_NO_ENABLED_PROFILE")
	}
	return out, nil
}

// MarkApplied is only called after the native helper and independent read-back
// confirm the same saved revision. Saving settings never calls this method.
func (s *APNStore) MarkApplied(expected int) (APNSettingsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, e := s.load()
	if e != nil {
		return APNSettingsView{}, e
	}
	if value.Revision != expected {
		return APNSettingsView{}, errors.New("REVISION_CONFLICT")
	}
	at := time.Now().UTC()
	value.AppliedRevision = &expected
	value.AppliedAt = &at
	if e = s.write(value); e != nil {
		return APNSettingsView{}, e
	}
	return apnPublic(value), nil
}
