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
	"regexp"
	"time"
)

const maximumProfileRecoveryBytes = 16 * 1024

var profileRecoveryName = regexp.MustCompile(`^profiles-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{16}\.json$`)

type privateProfileFrame struct {
	Index  int    `json:"index"`
	Status string `json:"status"`
	Frame  []byte `json:"frame,omitempty"`
}
type privateProfileBundle struct {
	SchemaVersion int                   `json:"schemaVersion"`
	ObservedAt    time.Time             `json:"observedAt"`
	DriverVersion string                `json:"driverVersion"`
	ModuleSHA256  string                `json:"moduleSHA256"`
	Profiles      []privateProfileFrame `json:"profiles"`
}

// Save publishes a complete file atomically beneath the fixed root-only
// recovery directory. A partial capture may be saved, but its summary remains
// incomplete and cannot be converted into a native restoration payload.
func (r ProfileRecovery) Save() (ProfileRecoverySummary, error) {
	if e := requireProfileRecoveryRoot(); e != nil {
		return ProfileRecoverySummary{}, e
	}
	return saveProfileRecoveryAt(ProfileRecoveryDirectory, r, time.Now().UTC(), rand.Reader, verifyProfileRecoveryOwner)
}

func rejectProfileRecoverySymlinks(directory string) error {
	for current := filepath.Clean(directory); ; current = filepath.Dir(current) {
		info, e := os.Lstat(current)
		if e != nil {
			return errors.New("profile recovery directory unavailable")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("profile recovery directory contains symlink")
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}
func profileRecoveryRoot(directory string, create bool, owner func(os.FileInfo, os.FileMode) error) (*os.Root, error) {
	if create {
		if e := createProfileRecoveryDirectories(directory); e != nil {
			return nil, errors.New("profile recovery directory creation failed")
		}
	}
	if e := rejectProfileRecoverySymlinks(directory); e != nil {
		return nil, e
	}
	root, e := os.OpenRoot(directory)
	if e != nil {
		return nil, errors.New("profile recovery directory open failed")
	}
	info, e := root.Stat(".")
	if e != nil || !info.IsDir() {
		root.Close()
		return nil, errors.New("invalid profile recovery directory")
	}
	if e = owner(info, 0700); e != nil {
		root.Close()
		return nil, e
	}
	return root, nil
}
func createProfileRecoveryDirectories(directory string) error {
	info, e := os.Lstat(directory)
	if e == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("invalid profile recovery directory")
		}
		return rejectProfileRecoverySymlinks(directory)
	}
	if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	parent := filepath.Dir(directory)
	if parent == directory {
		return errors.New("profile recovery parent unavailable")
	}
	if e = createProfileRecoveryDirectories(parent); e != nil {
		return e
	}
	root, e := os.OpenRoot(parent)
	if e != nil {
		return e
	}
	defer root.Close()
	if e = root.Mkdir(filepath.Base(directory), 0700); e != nil && !errors.Is(e, os.ErrExist) {
		return e
	}
	return rejectProfileRecoverySymlinks(directory)
}
func (r ProfileRecovery) bundle() (privateProfileBundle, error) {
	if r.observedAt.IsZero() {
		return privateProfileBundle{}, errors.New("profile capture timestamp missing")
	}
	out := privateProfileBundle{SchemaVersion: 1, ObservedAt: r.observedAt, DriverVersion: GobiNetDriverVersion, ModuleSHA256: GobiNetModuleSHA256, Profiles: []privateProfileFrame{}}
	available := 0
	for i, p := range r.profiles {
		status := p.status
		if status == "" {
			status = "not-requested"
		}
		if p.index != i+1 && p.index != 0 {
			return privateProfileBundle{}, errors.New("profile capture order invalid")
		}
		if status == "available" {
			if _, e := parseSavedAPNProfile(p.raw, i+1); e != nil {
				return privateProfileBundle{}, errors.New("profile capture frame invalid")
			}
			available++
		} else {
			if status != "failed" && status != "timeout" && status != "not-requested" {
				return privateProfileBundle{}, errors.New("profile capture state invalid")
			}
			if len(p.raw) > 0 {
				if status != "failed" {
					return privateProfileBundle{}, errors.New("unexpected failed profile payload")
				}
				if _, e := parseFailedSavedProfile(p.raw, i+1); e != nil {
					return privateProfileBundle{}, e
				}
			}
		}
		out.Profiles = append(out.Profiles, privateProfileFrame{Index: i + 1, Status: status, Frame: p.raw})
	}
	if available == 0 {
		return privateProfileBundle{}, errors.New("no valid saved profiles captured")
	}
	return out, nil
}
func saveProfileRecoveryAt(directory string, r ProfileRecovery, now time.Time, random io.Reader, owner func(os.FileInfo, os.FileMode) error) (ProfileRecoverySummary, error) {
	if r.observedAt.After(now.Add(time.Second)) || now.Sub(r.observedAt) > 2*time.Minute {
		return ProfileRecoverySummary{}, errors.New("profile capture is not recent")
	}
	bundle, e := r.bundle()
	if e != nil {
		return ProfileRecoverySummary{}, e
	}
	raw, e := json.Marshal(bundle)
	if e != nil || len(raw) > maximumProfileRecoveryBytes {
		return ProfileRecoverySummary{}, errors.New("profile backup size invalid")
	}
	root, e := profileRecoveryRoot(directory, true, owner)
	if e != nil {
		return ProfileRecoverySummary{}, e
	}
	defer root.Close()
	nonce := make([]byte, 8)
	if _, e = io.ReadFull(random, nonce); e != nil {
		return ProfileRecoverySummary{}, errors.New("profile backup identifier unavailable")
	}
	name := "profiles-" + r.observedAt.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(nonce) + ".json"
	temporary := name + ".tmp"
	file, e := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return ProfileRecoverySummary{}, errors.New("profile backup creation failed")
	}
	defer root.Remove(temporary)
	if e = file.Chmod(0600); e == nil {
		var info os.FileInfo
		info, e = file.Stat()
		if e == nil {
			e = owner(info, 0600)
		}
	}
	if e == nil {
		_, e = file.Write(raw)
	}
	if e == nil {
		e = file.Sync()
	}
	closeError := file.Close()
	if e != nil || closeError != nil {
		return ProfileRecoverySummary{}, errors.New("profile backup write failed")
	}
	// Link is atomic and refuses to overwrite any existing recovery capture.
	if e = root.Link(temporary, name); e != nil {
		return ProfileRecoverySummary{}, errors.New("profile backup publish failed")
	}
	if e = root.Remove(temporary); e != nil {
		return ProfileRecoverySummary{}, errors.New("profile backup temporary cleanup failed")
	}
	if e = syncProfileRecoveryDirectory(root); e != nil {
		return ProfileRecoverySummary{}, errors.New("profile backup sync failed")
	}
	summary := r.Summary()
	summary.BackupFile = name
	return summary, nil
}

// LoadProfileRecovery accepts only an immutable capture filename, never a path.
// It reads a root-owned 0600 regular file and revalidates every QMI frame.
func LoadProfileRecovery(filename string) (ProfileRecovery, error) {
	if e := requireProfileRecoveryRoot(); e != nil {
		return ProfileRecovery{}, e
	}
	return loadProfileRecoveryAt(ProfileRecoveryDirectory, filename, time.Now().UTC(), verifyProfileRecoveryOwner)
}
func loadProfileRecoveryAt(directory, filename string, now time.Time, owner func(os.FileInfo, os.FileMode) error) (ProfileRecovery, error) {
	if !profileRecoveryName.MatchString(filename) {
		return ProfileRecovery{}, errors.New("profile recovery filename invalid")
	}
	root, e := profileRecoveryRoot(directory, false, owner)
	if e != nil {
		return ProfileRecovery{}, e
	}
	defer root.Close()
	info, e := root.Lstat(filename)
	if e != nil || !info.Mode().IsRegular() || info.Size() > maximumProfileRecoveryBytes {
		return ProfileRecovery{}, errors.New("invalid profile recovery file")
	}
	if e = owner(info, 0600); e != nil {
		return ProfileRecovery{}, e
	}
	file, e := root.Open(filename)
	if e != nil {
		return ProfileRecovery{}, errors.New("profile recovery file open failed")
	}
	defer file.Close()
	opened, e := file.Stat()
	if e != nil || !os.SameFile(info, opened) {
		return ProfileRecovery{}, errors.New("profile recovery file changed while opening")
	}
	if e = owner(opened, 0600); e != nil {
		return ProfileRecovery{}, e
	}
	raw, e := io.ReadAll(io.LimitReader(file, maximumProfileRecoveryBytes+1))
	if e != nil || len(raw) > maximumProfileRecoveryBytes {
		return ProfileRecovery{}, errors.New("profile recovery file read failed")
	}
	return decodeProfileRecovery(raw, now)
}
func decodeProfileRecovery(raw []byte, now time.Time) (ProfileRecovery, error) {
	if len(raw) > maximumProfileRecoveryBytes {
		return ProfileRecovery{}, errors.New("profile recovery exceeds size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var bundle privateProfileBundle
	if e := decoder.Decode(&bundle); e != nil {
		return ProfileRecovery{}, errors.New("profile recovery JSON invalid")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return ProfileRecovery{}, errors.New("profile recovery trailing data")
	}
	if bundle.SchemaVersion != 1 || bundle.DriverVersion != GobiNetDriverVersion || bundle.ModuleSHA256 != GobiNetModuleSHA256 || bundle.ObservedAt.IsZero() || bundle.ObservedAt.After(now.Add(time.Second)) || len(bundle.Profiles) != 2 {
		return ProfileRecovery{}, errors.New("profile recovery provenance invalid")
	}
	out := ProfileRecovery{observedAt: bundle.ObservedAt}
	available := 0
	for i, frame := range bundle.Profiles {
		if frame.Index != i+1 {
			return ProfileRecovery{}, errors.New("profile recovery index invalid")
		}
		entry := savedAPNProfile{index: i + 1, status: frame.Status}
		if frame.Status == "available" {
			var e error
			entry, e = parseSavedAPNProfile(frame.Frame, i+1)
			if e != nil {
				return ProfileRecovery{}, errors.New("profile recovery QMI frame invalid")
			}
			available++
		} else {
			if frame.Status != "failed" && frame.Status != "timeout" && frame.Status != "not-requested" {
				return ProfileRecovery{}, errors.New("profile recovery query state invalid")
			}
			if len(frame.Frame) > 0 {
				if frame.Status != "failed" {
					return ProfileRecovery{}, errors.New("unexpected failed profile frame")
				}
				var e error
				entry, e = parseFailedSavedProfile(frame.Frame, i+1)
				if e != nil {
					return ProfileRecovery{}, e
				}
			}
		}
		out.profiles[i] = entry
	}
	if available == 0 {
		return ProfileRecovery{}, errors.New("profile recovery has no valid captures")
	}
	return out, nil
}

// WriteNativeProfiles emits credentials only to an explicitly supplied private
// FD writer in a root process. It is not an API observation or apply operation.
// A complete conversion is only an input check, not proof of restoration.
func (r ProfileRecovery) WriteNativeProfiles(writer io.Writer) error {
	if e := requireProfileRecoveryRoot(); e != nil {
		return e
	}
	return r.writeNativeProfiles(writer)
}
func (r ProfileRecovery) writeNativeProfiles(writer io.Writer) error {
	if !r.Summary().NativeFieldsComplete {
		return errors.New("saved native profile fields incomplete or unsupported")
	}
	// Private FD wire shared with tools/native: eight-byte header followed by
	// two 154-byte records. No credentials are placed in argv or environment.
	payload := make([]byte, 316)
	copy(payload, "NCCAPN1\n")
	for i, profile := range r.profiles {
		record := payload[8+i*154 : 8+(i+1)*154]
		record[0], record[1], record[2], record[3] = byte(profile.index), *profile.authentication, 0, 0
		copy(record[4:54], *profile.apn)
		copy(record[54:104], *profile.username)
		copy(record[104:154], *profile.password)
	}
	n, e := writer.Write(payload)
	clear(payload)
	if e == nil && n != 316 {
		return io.ErrShortWrite
	}
	return e
}
