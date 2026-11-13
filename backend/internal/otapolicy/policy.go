// Package otapolicy manages the stock Update service's documented-by-analysis
// no_ota_download presence flag. It never starts/stops Update or writes payloads.
//
// The stock directory holding the flag is world-writable, so the Update user could
// unlink a plain file. A flag created here is therefore also marked immutable, and
// Enforce recreates it when it disappears while the owner's choice is "stopped".
package otapolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"time"
)

var ErrConflict = errors.New("OTA_POLICY_CONFLICT")
var ErrUnmanaged = errors.New("OTA_POLICY_UNMANAGED")

type Status struct {
	Disabled bool   `json:"disabled"`
	Managed  bool   `json:"managed"`
	Token    string `json:"token"`
	Writable bool   `json:"writable"`
	Scope    string `json:"scope"`
	// Protected: the flag carries the immutable attribute, so no process can delete it.
	Protected bool `json:"protected"`
	// Heals counts how often Enforce had to recreate a missing flag.
	Heals    int        `json:"heals"`
	LastHeal *time.Time `json:"lastHeal,omitempty"`
}
type snapshot struct {
	Exists        bool
	Data          []byte
	Mode          uint32
	UID, GID      uint32
	Device, Inode uint64
	Modified      int64
}
type ledger struct {
	Version  int
	Original snapshot
	Current  snapshot
	Managed  bool
	// Added later; older ledgers simply lack them.
	Heals    int        `json:",omitempty"`
	LastHeal *time.Time `json:",omitempty"`
}
type store struct {
	flag, state string
	verify      func() bool
	// Device flags remain root-owned; host fixtures can use their own identity.
	uid, gid uint32
	// The immutable attribute; host fixtures substitute an in-memory record.
	protect, unprotect func(string) error
	protected          func(string) bool
	now                func() time.Time
}

func (s store) disabledFlag() snapshot {
	return snapshot{Exists: true, Data: []byte{}, Mode: 0644, UID: s.uid, GID: s.gid}
}

var deviceStore = store{
	flag:      "/pio_data/SYSTEM/Update/no_ota_download",
	state:     "/overlay/owner-apps/np1-control/private/ota-policy/ledger.json",
	verify:    firmwareMatches,
	protect:   func(path string) error { return setImmutable(path, true) },
	unprotect: func(path string) error { return setImmutable(path, false) },
	protected: isImmutable,
	now:       time.Now,
}

// FirmwareMatches reports whether Update and libAplLib are the analysed versions.
// A mismatch after installation means the firmware changed underneath NCC.
func FirmwareMatches() bool { return firmwareMatches() }

func firmwareMatches() bool {
	for path, want := range map[string]string{
		"/usr/local/bin/Update":       "567f4a5a28f1989facd1f0d56ddd9a5e5391e7fbbf171dd6fa1166998b95f779",
		"/usr/local/lib/libAplLib.so": "83bcfe30838001b382e58771fd28eabf17e43e6e72568ee5ad80656d223eae00",
	} {
		f, e := os.Open(path)
		if e != nil {
			return false
		}
		info, e := f.Stat()
		if e != nil || !info.Mode().IsRegular() || info.Size() > 64<<20 {
			f.Close()
			return false
		}
		h := sha256.New()
		_, e = io.Copy(h, io.LimitReader(f, 64<<20))
		f.Close()
		if e != nil || hex.EncodeToString(h.Sum(nil)) != want {
			return false
		}
	}
	return true
}
func noSymlinkParents(path string) error {
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		info, e := os.Lstat(p)
		if e != nil {
			return e
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe policy directory")
		}
		if filepath.Dir(p) == p {
			return nil
		}
	}
}
func capture(path string) (snapshot, error) {
	if e := noSymlinkParents(path); e != nil {
		return snapshot{}, e
	}
	info, e := os.Lstat(path)
	if os.IsNotExist(e) {
		return snapshot{}, nil
	}
	if e != nil {
		return snapshot{}, e
	}
	if !info.Mode().IsRegular() || info.Size() > 4096 {
		return snapshot{}, errors.New("unsupported policy flag")
	}
	f, e := os.Open(path)
	if e != nil {
		return snapshot{}, e
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(info, opened) {
		return snapshot{}, ErrConflict
	}
	data, e := io.ReadAll(io.LimitReader(f, 4097))
	if e != nil || len(data) > 4096 {
		return snapshot{}, errors.New("policy flag read failed")
	}
	result := snapshot{Exists: true, Data: data, Mode: uint32(info.Mode().Perm()), Modified: info.ModTime().UnixNano()}
	identify(info, &result)
	return result, nil
}
func token(value snapshot) string {
	b, _ := json.Marshal(value)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func equal(a, b snapshot) bool { return reflect.DeepEqual(a, b) }
func (s store) load() (ledger, error) {
	if e := noSymlinkParents(s.state); e != nil {
		return ledger{}, e
	}
	info, e := os.Lstat(s.state)
	if os.IsNotExist(e) {
		return ledger{}, nil
	}
	if e != nil {
		return ledger{}, e
	}
	if !info.Mode().IsRegular() || info.Size() > 24576 {
		return ledger{}, ErrUnmanaged
	}
	b, e := os.ReadFile(s.state)
	if e != nil {
		return ledger{}, e
	}
	var l ledger
	if json.Unmarshal(b, &l) != nil || l.Version != 1 {
		return ledger{}, ErrUnmanaged
	}
	return l, nil
}
func (s store) save(l ledger) error {
	b, e := json.Marshal(l)
	if e != nil {
		return e
	}
	dir := filepath.Dir(s.state)
	f, e := os.CreateTemp(dir, ".ledger-")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	return os.Rename(name, s.state)
}
func (s store) prepare() error {
	if !s.verify() {
		return errors.New("unsupported Update firmware")
	}
	// Only our private directory may be created; stock directories must exist.
	if e := noSymlinkParents(s.flag); e != nil {
		return e
	}
	parent := filepath.Dir(filepath.Dir(s.state))
	if e := noSymlinkParents(filepath.Join(parent, "check")); e != nil {
		return e
	}
	if e := os.MkdirAll(filepath.Dir(s.state), 0700); e != nil {
		return e
	}
	return noSymlinkParents(s.state)
}
func (s store) status() (Status, error) {
	if !s.verify() {
		return Status{}, errors.New("unsupported Update firmware")
	}
	current, e := capture(s.flag)
	if e != nil {
		return Status{}, e
	}
	l, e := s.load()
	if e != nil && !os.IsNotExist(e) {
		return Status{}, e
	}
	managed := l.Version == 1 && l.Managed && equal(current, l.Current)
	return Status{Disabled: current.Exists, Managed: managed, Writable: managed, Token: token(current), Scope: "本体OTAの新しい更新データ取得。進行中の更新は中断しません", Protected: current.Exists && s.protected != nil && s.protected(s.flag), Heals: l.Heals, LastHeal: l.LastHeal}, nil
}
func (s store) writeFlag(value snapshot) error {
	if !value.Exists {
		// An immutable file cannot be unlinked; lift the attribute first.
		if s.unprotect != nil {
			if e := s.unprotect(s.flag); e != nil {
				return e
			}
		}
		return os.Remove(s.flag)
	}
	f, e := os.OpenFile(s.flag, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(value.Data)
	if e == nil {
		e = f.Chmod(os.FileMode(value.Mode))
	}
	if e == nil {
		e = setOwner(f, value.UID, value.GID)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	return e
}
func (s store) change(l ledger, target snapshot) (Status, error) {
	before, e := capture(s.flag)
	if e != nil {
		return Status{}, e
	}
	if !equal(before, l.Current) {
		return Status{}, ErrConflict
	}
	if before.Exists == target.Exists {
		if !before.Exists || (reflect.DeepEqual(before.Data, target.Data) && before.Mode == target.Mode && before.UID == target.UID && before.GID == target.GID) {
			l.Current = before
			if e = s.save(l); e != nil {
				return Status{}, e
			}
			return s.status()
		}
		return Status{}, ErrConflict
	}
	if e = s.writeFlag(target); e != nil {
		return Status{}, e
	}
	after, e := capture(s.flag)
	if e != nil {
		return Status{}, e
	}
	if after.Exists != target.Exists || (after.Exists && (!reflect.DeepEqual(after.Data, target.Data) || after.Mode != target.Mode || after.UID != target.UID || after.GID != target.GID)) {
		return Status{}, errors.New("policy read-back failed")
	}
	// Only a flag this package created is protected. A failure leaves the flag in
	// effect; Status reports it unprotected and Enforce tries again.
	if after.Exists && s.protect != nil {
		_ = s.protect(s.flag)
	}
	l.Current = after
	if e = s.save(l); e != nil {
		return Status{}, e
	}
	return s.status()
}
func (s store) run(action string, expected string, disabled bool) (Status, error) {
	switch action {
	case "status", "install", "set", "disable", "enable", "rollback", "uninstall", "enforce":
	default:
		return Status{}, errors.New("unknown OTA policy action")
	}
	if action == "status" {
		return s.status()
	}
	if e := s.prepare(); e != nil {
		return Status{}, e
	}
	unlock, e := lockPolicy(filepath.Join(filepath.Dir(s.state), "operation.lock"))
	if e != nil {
		return Status{}, e
	}
	defer unlock()
	l, e := s.load()
	if e != nil {
		return Status{}, e
	}
	if action == "install" {
		if l.Version == 1 && l.Managed {
			current, e := capture(s.flag)
			if e != nil {
				return Status{}, e
			}
			if !equal(current, l.Current) {
				return Status{}, ErrConflict
			}
			return s.status()
		} // Preserve a deliberate Web re-enable across upgrades.
		before, e := capture(s.flag)
		if e != nil {
			return Status{}, e
		}
		l = ledger{Version: 1, Original: before, Current: before, Managed: true}
		if e = s.save(l); e != nil {
			return Status{}, e
		}
		if before.Exists {
			return s.status()
		}
		return s.change(l, s.disabledFlag())
	}
	if l.Version != 1 || !l.Managed {
		return Status{}, ErrUnmanaged
	}
	if action == "enforce" {
		return s.enforce(l)
	}
	now, e := capture(s.flag)
	if e != nil {
		return Status{}, e
	}
	if !equal(now, l.Current) {
		return Status{}, ErrConflict
	}
	switch action {
	case "set":
		if expected == "" || expected != token(now) {
			return Status{}, ErrConflict
		}
		if disabled {
			if now.Exists {
				return s.status()
			}
			return s.change(l, s.disabledFlag())
		}
		return s.change(l, snapshot{})
	case "disable":
		if now.Exists {
			return s.status()
		}
		return s.change(l, s.disabledFlag())
	case "enable":
		return s.change(l, snapshot{})
	case "rollback", "uninstall":
		result, e := s.change(l, l.Original)
		if e != nil {
			return Status{}, e
		}
		l.Current, e = capture(s.flag)
		if e != nil {
			return Status{}, e
		}
		l.Managed = false
		if e = s.save(l); e != nil {
			return Status{}, e
		}
		result.Managed = false
		result.Writable = false
		return result, nil
	default:
		return Status{}, errors.New("unknown OTA policy action")
	}
}

// enforce keeps the owner's choice in effect. While the choice is "stopped" (the
// recorded flag exists) a missing flag is recreated and an unprotected flag that this
// package created is protected again. A flag replaced by someone else still stops
// downloads, so it is left alone. An explicit re-enable (no recorded flag) is never
// overridden.
func (s store) enforce(l ledger) (Status, error) {
	if !l.Current.Exists {
		return s.status()
	}
	now, e := capture(s.flag)
	if e != nil {
		return Status{}, e
	}
	if !now.Exists {
		l.Current = now
		l.Heals++
		at := time.Now().UTC()
		if s.now != nil {
			at = s.now().UTC()
		}
		l.LastHeal = &at
		if e = s.save(l); e != nil {
			return Status{}, e
		}
		return s.change(l, s.disabledFlag())
	}
	if equal(now, l.Current) && !l.Original.Exists && s.protected != nil && !s.protected(s.flag) && s.protect != nil {
		_ = s.protect(s.flag)
	}
	return s.status()
}
func Run(action string) (Status, error) { return deviceStore.run(action, "", false) }

// Enforce is called periodically by the OTA guard.
func Enforce() (Status, error) { return deviceStore.run("enforce", "", false) }
func Set(expectedToken string, disabled bool) (Status, error) {
	return deviceStore.run("set", expectedToken, disabled)
}
