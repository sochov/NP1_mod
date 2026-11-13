package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestFullBackupCarriesOwnerSettingsAndRestoresThem(t *testing.T) {
	srv, ts, client, csrf := plateFixture(t, &fakeSensor{instance: "backup"})
	owner := map[string]json.RawMessage{"ap/config.json": json.RawMessage(`{"ssid":"NP1-TEST","passphrase":"fictional-passphrase"}`), "uplink/settings.json": json.RawMessage(`{"mode":"auto"}`)}
	var restored []map[string]json.RawMessage
	var dryRuns int
	srv.client = sensorClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /backup/owner":
			json.NewEncoder(w).Encode(map[string]any{"files": owner})
		case "POST /backup/owner":
			var body struct {
				DryRun bool                       `json:"dryRun"`
				Files  map[string]json.RawMessage `json:"files"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if body.DryRun {
				dryRuns++
			} else {
				restored = append(restored, body.Files)
			}
			json.NewEncoder(w).Encode(map[string]any{"changed": []string{"ap/config.json"}, "restarts": []string{"np1-control-ap.service"}})
		case "GET /wifi/networks":
			w.Write([]byte(`{"networks":[{"ssid":"stock-saved"}]}`))
		case "GET /device/ota-policy":
			w.Write([]byte(`{"disabled":true}`))
		default:
			t.Error("unexpected broker call", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	dir := srv.cfg.StateDir
	os.MkdirAll(filepath.Join(dir, "plate", "speech", "uploads"), 0o700)
	os.WriteFile(filepath.Join(dir, "plate", "speech", "api.json"), []byte(`{"url":"https://voicevox.invalid","apiKey":"fictional-key"}`), 0o600)
	os.WriteFile(filepath.Join(dir, "plate", "speech", "uploads", "abc-1.pcm"), []byte{1, 2, 3, 4}, 0o600)

	r := request(t, client, ts, "POST", "/backup/export", csrf, nil)
	var backup map[string]any
	json.NewDecoder(r.Body).Decode(&backup)
	r.Body.Close()
	if r.StatusCode != 200 || backup["schemaVersion"] != float64(2) {
		t.Fatalf("export %d %v", r.StatusCode, backup["schemaVersion"])
	}
	if backup["owner"].(map[string]any)["ap/config.json"] == nil || backup["server"].(map[string]any)["plate/speech/api.json"] == nil || backup["uploads"].(map[string]any)["abc-1.pcm"] == nil {
		t.Fatalf("backup is missing settings: %v", backup)
	}
	if backup["recordedOnly"].(map[string]any)["stockWifi"] == nil {
		t.Fatal("stock Wi-Fi list is not recorded")
	}

	// Change the VOICEVOX file, then restore.
	os.WriteFile(filepath.Join(dir, "plate", "speech", "api.json"), []byte(`{"url":"https://other.invalid"}`), 0o600)
	os.Remove(filepath.Join(dir, "plate", "speech", "uploads", "abc-1.pcm"))
	settings := request(t, client, ts, "GET", "/settings", "", nil)
	var current Settings
	json.NewDecoder(settings.Body).Decode(&current)
	settings.Body.Close()
	backup["expectedRevision"], backup["dryRun"] = current.Revision, true
	r = request(t, client, ts, "POST", "/backup/restore", csrf, backup)
	var plan restorePlan
	json.NewDecoder(r.Body).Decode(&plan)
	r.Body.Close()
	if r.StatusCode != 200 || len(plan.ServerChanged) != 1 || plan.ServerChanged[0] != "plate/speech/api.json" || len(restored) != 0 {
		t.Fatalf("dry run %d %+v", r.StatusCode, plan)
	}
	backup["dryRun"] = false
	r = request(t, client, ts, "POST", "/backup/restore", csrf, backup)
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("restore %d", r.StatusCode)
	}
	var voice map[string]string
	b, _ := os.ReadFile(filepath.Join(dir, "plate", "speech", "api.json"))
	if json.Unmarshal(b, &voice) != nil || voice["url"] != "https://voicevox.invalid" || voice["apiKey"] != "fictional-key" {
		t.Fatalf("VOICEVOX settings not restored: %s", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "plate", "speech", "uploads", "abc-1.pcm")); len(b) != 4 {
		t.Fatal("uploaded sound not restored")
	}
	if len(restored) != 1 || restored[0]["ap/config.json"] == nil || dryRuns != 2 {
		t.Fatalf("owner files not passed to the broker: %v %d", restored, dryRuns)
	}
}

func TestFullBackupRejectsUnknownServerFiles(t *testing.T) {
	if _, e := validateServerBackup(map[string]json.RawMessage{"../config.json": json.RawMessage(`{}`)}, nil); e == nil {
		t.Fatal("accepted a path outside the list")
	}
	if _, e := validateServerBackup(nil, map[string]string{"../x.pcm": "AA=="}); e == nil {
		t.Fatal("accepted an unsafe upload name")
	}
}
