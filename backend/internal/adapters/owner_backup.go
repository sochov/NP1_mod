package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"time"
)

// Owner settings backup on the root broker.
//
// The web service cannot read the owner's private settings (they hold passwords and keys and
// belong to root), so the broker exports them verbatim for a full settings backup and writes
// them back on restore. Only the files below are touched, each must be a JSON object (the
// hotspot autostart marker is the one plain-text file), and the previous copy is kept next to
// it. Services that read the files at start are restarted afterwards so the restored settings
// take effect; the broker and the web service restart last.

var ownerPrivateRoot = "/overlay/owner-apps/np1-control/private"

type ownerBackupFile struct {
	Name  string   // relative to ownerPrivateRoot
	Text  bool     // plain text instead of a JSON object
	Units []string // restarted after a restore that changed the file
}

var ownerBackupFiles = []ownerBackupFile{
	{Name: "led/config.json", Units: []string{"np1-control-led.service"}},
	{Name: "ap/config.json", Units: []string{"np1-control-ap.service"}},
	{Name: "ap/autostart", Text: true, Units: []string{"np1-control-ap.service"}},
	{Name: "apn/config.json"},
	{Name: "nas/config.json"},
	{Name: "nas/policies.json"},
	{Name: "wifi/profiles.json"},
	{Name: "uplink/settings.json", Units: []string{"np1-control-uplink.service"}},
}

const ownerBackupMaxFile = 64 * 1024

// OwnerBackupExport returns the files that exist; absent files are simply not part of it.
func OwnerBackupExport() (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	for _, f := range ownerBackupFiles {
		b, e := os.ReadFile(filepath.Join(ownerPrivateRoot, f.Name))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		if len(b) > ownerBackupMaxFile {
			return nil, errors.New("OWNER_FILE_TOO_LARGE")
		}
		if f.Text {
			b, _ = json.Marshal(string(b))
		} else if !json.Valid(b) {
			return nil, errors.New("OWNER_FILE_INVALID")
		}
		out[f.Name] = json.RawMessage(b)
	}
	return out, nil
}

func ownerBackupContent(f ownerBackupFile, raw json.RawMessage) ([]byte, error) {
	if len(raw) > ownerBackupMaxFile {
		return nil, errors.New("OWNER_FILE_TOO_LARGE")
	}
	if f.Text {
		var s string
		if json.Unmarshal(raw, &s) != nil || (s != "" && s != "enabled\n") {
			return nil, errors.New("OWNER_FILE_INVALID")
		}
		return []byte(s), nil
	}
	var object map[string]any
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return nil, errors.New("OWNER_FILE_INVALID")
	}
	return raw, nil
}

// OwnerBackupRestore validates every file first, then writes the changed ones. It returns the
// names written and the units to restart.
func OwnerBackupRestore(files map[string]json.RawMessage, dryRun bool) (changed []string, units []string, err error) {
	known := map[string]ownerBackupFile{}
	for _, f := range ownerBackupFiles {
		known[f.Name] = f
	}
	contents := map[string][]byte{}
	for name, raw := range files {
		f, ok := known[name]
		if !ok {
			return nil, nil, errors.New("OWNER_FILE_UNKNOWN")
		}
		b, e := ownerBackupContent(f, raw)
		if e != nil {
			return nil, nil, e
		}
		contents[name] = b
	}
	if name := "ap/config.json"; contents[name] != nil {
		// The hotspot configuration has its own loader with stricter checks.
		if e := os.MkdirAll(filepath.Join(ownerPrivateRoot, "ap"), 0o700); e != nil {
			return nil, nil, e
		}
		tmp, e := os.CreateTemp(filepath.Join(ownerPrivateRoot, "ap"), ".ncc-check-*.json")
		if e != nil {
			return nil, nil, e
		}
		tmp.Write(contents[name])
		tmp.Close()
		_, e = apLoadConfig(tmp.Name())
		os.Remove(tmp.Name())
		if e != nil {
			return nil, nil, errors.New("OWNER_FILE_INVALID")
		}
	}
	unitSet := map[string]bool{}
	for _, f := range ownerBackupFiles {
		b, ok := contents[f.Name]
		if !ok {
			continue
		}
		path := filepath.Join(ownerPrivateRoot, f.Name)
		previous, e := os.ReadFile(path)
		if e == nil && sameJSONOrText(previous, b) {
			continue
		}
		changed = append(changed, f.Name)
		for _, u := range f.Units {
			unitSet[u] = true
		}
		if dryRun {
			continue
		}
		if e == nil {
			if e = writeOwnerFile(path+".before-restore", previous); e != nil {
				return changed, nil, e
			}
		}
		if f.Text && len(b) == 0 {
			if e = os.Remove(path); e != nil && !os.IsNotExist(e) {
				return changed, nil, e
			}
			continue
		}
		if e = writeOwnerFile(path, b); e != nil {
			return changed, nil, e
		}
	}
	for u := range unitSet {
		units = append(units, u)
	}
	sort.Strings(units)
	return changed, units, nil
}

// sameJSONOrText compares JSON by value (exports are re-encoded compactly) and text exactly.
func sameJSONOrText(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil {
		return reflect.DeepEqual(x, y)
	}
	return bytes.Equal(bytes.TrimSpace(a), bytes.TrimSpace(b))
}

// SameJSON is shared with the web service's own backup files.
func SameJSON(a, b []byte) bool { return sameJSONOrText(a, b) }

func writeOwnerFile(path string, b []byte) error {
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

// restartAfterRestore restarts the given units, then the web service and the broker itself,
// after the response has been sent.
var restartAfterRestore = func(units []string, broker bool) {
	go func() {
		time.Sleep(time.Second)
		for _, u := range append(units, "np1-control.service") {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			exec.CommandContext(ctx, "systemctl", "restart", "--no-block", u).Run()
			cancel()
		}
		if broker {
			exec.Command("systemctl", "restart", "--no-block", "np1-control-broker.service").Run()
		}
	}()
}

func RegisterOwnerBackupHandlers(mux *http.ServeMux) {
	mux.HandleFunc("GET /backup/owner", func(w http.ResponseWriter, r *http.Request) {
		files, e := OwnerBackupExport()
		if e != nil {
			recordingJSON(w, 500, map[string]string{"code": "OWNER_BACKUP_UNAVAILABLE"})
			return
		}
		recordingJSON(w, 200, map[string]any{"files": files})
	})
	mux.HandleFunc("POST /backup/owner", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var body struct {
			DryRun bool                       `json:"dryRun"`
			Files  map[string]json.RawMessage `json:"files"`
			// RestartWeb asks for a web service restart for its own restored files.
			RestartWeb bool `json:"restartWeb"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			recordingJSON(w, 400, map[string]string{"code": "INVALID_BACKUP"})
			return
		}
		changed, units, e := OwnerBackupRestore(body.Files, body.DryRun)
		if e != nil {
			recordingJSON(w, 400, map[string]any{"code": e.Error(), "changed": changed})
			return
		}
		if changed == nil {
			changed = []string{}
		}
		if !body.DryRun && (len(changed) > 0 || body.RestartWeb) {
			restartAfterRestore(units, len(changed) > 0)
		}
		recordingJSON(w, 200, map[string]any{"changed": changed, "restarts": units, "dryRun": body.DryRun})
	})
}
