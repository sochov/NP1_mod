package modem

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

const recoveryLatestFile = "latest.json"
const recoveryInventoryFile = "inventory.json"
const fixedInventoryRuntimeFile = "/run/np1-apn-profile-inventory/summary.json"

type recoveryLatest struct {
	SchemaVersion int       `json:"schemaVersion"`
	Filename      string    `json:"filename"`
	ObservedAt    time.Time `json:"observedAt"`
}
type recoveryInventory struct {
	SchemaVersion int              `json:"schemaVersion"`
	DriverVersion string           `json:"driverVersion"`
	ModuleSHA256  string           `json:"moduleSHA256"`
	Inventory     ProfileInventory `json:"inventory"`
}
type ConfiguredAPNProfile1 struct {
	Status             string    `json:"status"`
	Source             string    `json:"source"`
	Current            bool      `json:"current"`
	ObservedAt         time.Time `json:"observedAt"`
	APN                string    `json:"apn"`
	Auth               int       `json:"auth"`
	UsernameConfigured bool      `json:"usernameConfigured"`
	PasswordConfigured bool      `json:"passwordConfigured"`
	Profile2Absent     bool      `json:"profile2Absent"`
	Profile2Known      bool      `json:"profile2Known"`
}

// RebuildProfileRecoveryIndex selects only validated, immutable captures beneath
// the fixed root directory. It is an explicit maintenance action, never a GET.
func RebuildProfileRecoveryIndex() error {
	if e := requireProfileRecoveryRoot(); e != nil {
		return e
	}
	root, e := profileRecoveryRoot(ProfileRecoveryDirectory, false, verifyProfileRecoveryOwner)
	if e != nil {
		return e
	}
	defer root.Close()
	dir, e := root.Open(".")
	if e != nil {
		return e
	}
	entries, e := dir.ReadDir(513)
	dir.Close()
	if e != nil && e != io.EOF {
		return e
	}
	if len(entries) > 512 {
		return errors.New("profile recovery directory exceeds index limit")
	}
	var chosen recoveryLatest
	for _, entry := range entries {
		if !profileRecoveryName.MatchString(entry.Name()) {
			continue
		}
		capture, e := loadProfileRecoveryAt(ProfileRecoveryDirectory, entry.Name(), time.Now().UTC(), verifyProfileRecoveryOwner)
		if e != nil {
			return errors.New("profile recovery capture validation failed")
		}
		if chosen.Filename == "" || capture.observedAt.After(chosen.ObservedAt) {
			chosen = recoveryLatest{1, entry.Name(), capture.observedAt}
		}
	}
	if chosen.Filename == "" {
		return errors.New("profile recovery capture unavailable")
	}
	return writeRecoveryMetadata(root, recoveryLatestFile, chosen)
}
func readRecoveryMetadata(root *os.Root, name string, out any) error {
	info, e := root.Lstat(name)
	if e != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return errors.New("profile metadata unavailable")
	}
	if e = verifyProfileRecoveryOwner(info, 0600); e != nil {
		return e
	}
	file, e := root.Open(name)
	if e != nil {
		return e
	}
	defer file.Close()
	opened, e := file.Stat()
	if e != nil || !os.SameFile(info, opened) {
		return errors.New("profile metadata changed while opening")
	}
	if e = verifyProfileRecoveryOwner(opened, 0600); e != nil {
		return e
	}
	raw, e := io.ReadAll(io.LimitReader(file, 4097))
	if e != nil || len(raw) > 4096 {
		return errors.New("profile metadata read failed")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil || decoder.Decode(new(any)) != io.EOF {
		return errors.New("profile metadata JSON invalid")
	}
	return nil
}
func writeRecoveryMetadata(root *os.Root, name string, value any) error {
	if info, e := root.Lstat(name); e == nil {
		if !info.Mode().IsRegular() {
			return errors.New("profile metadata target invalid")
		}
		if e = verifyProfileRecoveryOwner(info, 0600); e != nil {
			return e
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	raw, e := json.Marshal(value)
	if e != nil || len(raw) > 4096 {
		return errors.New("profile metadata exceeds limit")
	}
	nonce := make([]byte, 8)
	if _, e = io.ReadFull(rand.Reader, nonce); e != nil {
		return e
	}
	temporary := name + "-" + hex.EncodeToString(nonce) + ".tmp"
	file, e := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	defer root.Remove(temporary)
	if _, e = file.Write(raw); e == nil {
		e = file.Sync()
	}
	closed := file.Close()
	if e != nil {
		return e
	}
	if closed != nil {
		return closed
	}
	if e = root.Rename(temporary, name); e != nil {
		return e
	}
	return syncProfileRecoveryDirectory(root)
}
func latestIndexedRecovery() (ProfileRecovery, error) {
	root, e := profileRecoveryRoot(ProfileRecoveryDirectory, false, verifyProfileRecoveryOwner)
	if e != nil {
		return ProfileRecovery{}, e
	}
	defer root.Close()
	var index recoveryLatest
	if e = readRecoveryMetadata(root, recoveryLatestFile, &index); e != nil {
		return ProfileRecovery{}, e
	}
	if index.SchemaVersion != 1 || !profileRecoveryName.MatchString(index.Filename) {
		return ProfileRecovery{}, errors.New("profile recovery index invalid")
	}
	capture, e := loadProfileRecoveryAt(ProfileRecoveryDirectory, index.Filename, time.Now().UTC(), verifyProfileRecoveryOwner)
	if e != nil {
		return ProfileRecovery{}, e
	}
	if !capture.observedAt.Equal(index.ObservedAt) {
		return ProfileRecovery{}, errors.New("profile recovery index timestamp mismatch")
	}
	return capture, nil
}
func validateProfile1Inventory(inventory ProfileInventory, capturedAt, now time.Time) error {
	if inventory.Source != "gobinet-wds-saved-profile-indices" || inventory.Current || inventory.ProfileType != "3gpp" || len(inventory.Indices) != 1 || inventory.Indices[0] != 1 || !inventory.Profile1Present || inventory.Profile2Present || inventory.ObservedAt.Before(capturedAt) || inventory.ObservedAt.After(capturedAt.Add(10*time.Minute)) || inventory.ObservedAt.After(now.Add(time.Second)) {
		return errors.New("profile1-only inventory proof unavailable")
	}
	return nil
}

// ImportProfileInventoryProof consumes only our root-owned fixed runtime output.
// It stores indices only; names and credentials are never in this metadata.
func ImportProfileInventoryProof() error {
	if e := requireProfileRecoveryRoot(); e != nil {
		return e
	}
	capture, e := latestIndexedRecovery()
	if e != nil {
		return e
	}
	runtimeRoot, e := profileRecoveryRoot(filepath.Dir(fixedInventoryRuntimeFile), false, verifyProfileRecoveryOwner)
	if e != nil {
		return e
	}
	defer runtimeRoot.Close()
	var inventory ProfileInventory
	if e = readRecoveryMetadata(runtimeRoot, filepath.Base(fixedInventoryRuntimeFile), &inventory); e != nil {
		return e
	}
	if e = validateProfile1Inventory(inventory, capture.observedAt, time.Now().UTC()); e != nil {
		return e
	}
	root, e := profileRecoveryRoot(ProfileRecoveryDirectory, false, verifyProfileRecoveryOwner)
	if e != nil {
		return e
	}
	defer root.Close()
	return writeRecoveryMetadata(root, recoveryInventoryFile, recoveryInventory{1, GobiNetDriverVersion, GobiNetModuleSHA256, inventory})
}
func indexedProfile1Inventory(capturedAt time.Time) error {
	root, e := profileRecoveryRoot(ProfileRecoveryDirectory, false, verifyProfileRecoveryOwner)
	if e != nil {
		return e
	}
	defer root.Close()
	var proof recoveryInventory
	if e = readRecoveryMetadata(root, recoveryInventoryFile, &proof); e != nil {
		return e
	}
	if proof.SchemaVersion != 1 || proof.DriverVersion != GobiNetDriverVersion || proof.ModuleSHA256 != GobiNetModuleSHA256 {
		return errors.New("profile inventory provenance invalid")
	}
	return validateProfile1Inventory(proof.Inventory, capturedAt, time.Now().UTC())
}
func configuredProfile1(capture ProfileRecovery) (ConfiguredAPNProfile1, error) {
	p := capture.profiles[0]
	if !p.nativeFieldsComplete() {
		return ConfiguredAPNProfile1{}, errors.New("saved profile1 fields incomplete")
	}
	status := "available"
	if time.Since(capture.observedAt) > 24*time.Hour {
		status = "stale"
	}
	return ConfiguredAPNProfile1{Status: status, Source: "gobinet-wds-saved-profile", Current: false, ObservedAt: capture.observedAt, APN: *p.apn, Auth: int(*p.authentication), UsernameConfigured: *p.username != "", PasswordConfigured: *p.password != ""}, nil
}

// ReadConfiguredAPNProfile1 reads the verified local index only. It never issues
// a modem query, imports editor data, or claims the saved APN is a current PDP.
func ReadConfiguredAPNProfile1() (ConfiguredAPNProfile1, error) {
	if e := requireProfileRecoveryRoot(); e != nil {
		return ConfiguredAPNProfile1{}, e
	}
	capture, e := latestIndexedRecovery()
	if e != nil {
		return ConfiguredAPNProfile1{}, e
	}
	view, e := configuredProfile1(capture)
	if e != nil {
		return view, e
	}
	if indexedProfile1Inventory(capture.observedAt) == nil {
		view.Profile2Absent, view.Profile2Known = true, true
	}
	return view, nil
}

// WithSavedProfile1ForEditor passes secrets only inside a root process to its
// private store writer. No credential is returned in a public struct or JSON.
func WithSavedProfile1ForEditor(consume func(ConfiguredAPNProfile1, string, string) error) error {
	if e := requireProfileRecoveryRoot(); e != nil {
		return e
	}
	capture, e := latestIndexedRecovery()
	if e != nil {
		return e
	}
	view, e := configuredProfile1(capture)
	if e != nil {
		return e
	}
	if view.Status != "available" {
		return errors.New("saved profile1 capture is stale")
	}
	if e = indexedProfile1Inventory(capture.observedAt); e != nil {
		return e
	}
	view.Profile2Absent, view.Profile2Known = true, true
	return consume(view, *capture.profiles[0].username, *capture.profiles[0].password)
}
