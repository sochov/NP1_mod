package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestTemporaryLEDTestNeverWritesTheSavedPreset(t *testing.T) {
	srv, ts, client, csrf := plateFixture(t, &fakeSensor{instance: "led-test"})
	var paths []string
	srv.client = sensorClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Method != "POST" {
			t.Error("test must not save LED settings")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/led/notifications" {
			var body struct {
				DurationMS int `json:"durationMs"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if body.DurationMS != 5000 {
				t.Error("test duration was lost")
			}
			json.NewEncoder(w).Encode(map[string]string{"id": strings.Repeat("a", 64)})
		} else if r.URL.Path == "/led/notifications/stop" {
			w.Write([]byte(`{"accepted":true}`))
		} else {
			t.Error("unexpected broker write", r.URL.Path)
			w.WriteHeader(400)
		}
	}))
	body := map[string]any{"settings": map[string]any{"mode": "rainbow", "brightness": 180}, "durationMs": 5000}
	r := request(t, client, ts, "POST", "/device/led/test", csrf, body)
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal(r.StatusCode)
	}
	r = request(t, client, ts, "POST", "/device/led/test/stop", csrf, struct{}{})
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal(r.StatusCode)
	}
	body["durationMs"] = 30001
	r = request(t, client, ts, "POST", "/device/led/test", csrf, body)
	r.Body.Close()
	if r.StatusCode != 400 {
		t.Fatal("unbounded test duration accepted")
	}
	if len(paths) != 2 {
		t.Fatal("invalid test reached broker")
	}
}
