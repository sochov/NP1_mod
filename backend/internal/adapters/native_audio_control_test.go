package adapters

import (
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/np1-control-center/ncc/internal/model"
)

func TestNativeAudioHTTPStrictSchemaAndNoMasterWrite(t *testing.T) {
	for _, id := range []string{"mute", "volume-correction"} {
		for _, body := range []string{
			`{}`, `{"value":1}`, `{"expectedValue":0}`, `{"expectedValue":0,"value":3}`,
			`{"expectedValue":-1,"value":0}`, `{"expectedValue":0,"value":"1"}`,
			`{"expectedValue":0,"value":1.0}`, `{"expectedValue":0,"value":1e0}`,
			`{"expectedValue":0,"value":true}`, `{"expectedValue":null,"value":1}`,
			`{"expectedValue":0,"value":1,"volume":0}`, `{"expectedValue":0,"value":1} {}`,
			`{"expectedValue":0,"value":1,"value":0}`, `{"expectedValue":0,"expectedValue":1,"value":1}`,
		} {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("PUT", "/audio-settings/"+id, strings.NewReader(body))
			r.SetPathValue("id", id)
			called := false
			handleNativeAudio(w, r, func(*nativeAudioPut) (model.Observation, error) { called = true; return model.Observation{}, nil })
			if w.Code != 400 || called {
				t.Fatalf("invalid audio body accepted: id%s code%d %s", id, w.Code, body)
			}
		}
	}
	for _, id := range []string{"master-volume", "volume", "unknown", "../mute"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("PUT", "/audio-settings/unknown", strings.NewReader(`{"expectedValue":0,"value":1}`))
		r.SetPathValue("id", id)
		handleNativeAudio(w, r, func(*nativeAudioPut) (model.Observation, error) {
			t.Fatal("unproven write reached owner")
			return model.Observation{}, nil
		})
		if w.Code != 404 {
			t.Fatal("unknown audio setting not rejected")
		}
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/audio-settings/mute", strings.NewReader(`{"expectedValue":0,"value":2}`))
	r.SetPathValue("id", "mute")
	if _, err := parseNativeAudioPut(w, r, "mute"); err == nil {
		t.Fatal("mute accepted correction enum")
	}
}

func TestNativeAudioHTTPReadWriteAndConflict(t *testing.T) {
	state := nativeAudioValue{17, 0, 2}
	for _, method := range []string{"GET", "PUT"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/audio-settings/mute", strings.NewReader(`{"expectedValue":0,"value":1}`))
		r.SetPathValue("id", "mute")
		handleNativeAudio(w, r, func(p *nativeAudioPut) (model.Observation, error) {
			if method == "GET" {
				if p != nil {
					t.Fatal("read became write")
				}
			} else {
				if p == nil || p.id != "mute" || p.ExpectedValue != 0 || p.Value != 1 {
					t.Fatal("write contract changed")
				}
				state.mute = 1
			}
			return nativeAudioObservation(state, true, ""), nil
		})
		if w.Code != 200 {
			t.Fatal("verified audio response rejected")
		}
	}
	for _, failure := range []struct {
		err    error
		status int
	}{{errNativeAudioConflict, 409}, {errors.New("private vendor stderr"), 503}} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/audio-settings", nil)
		handleNativeAudio(w, r, func(*nativeAudioPut) (model.Observation, error) { return model.Observation{}, failure.err })
		if w.Code != failure.status || strings.Contains(w.Body.String(), "vendor stderr") {
			t.Fatal("audio error leaked or mislabeled")
		}
	}
}

func TestNativeAudioResultNeedsCorrelatedHelperCleanupAndKnownValues(t *testing.T) {
	valid := "audio-volume=17\naudio-mute=0\naudio-correction=2\ncleanup=matched; own-receiver-removed\n"
	actual, err := parseNativeAudioOutput([]byte(valid))
	if err != nil || actual != (nativeAudioValue{17, 0, 2}) {
		t.Fatal("valid audio result rejected", err)
	}
	for _, text := range []string{"", strings.ReplaceAll(valid, "cleanup=matched; own-receiver-removed", ""), valid + "audio-mute=1\n", valid + "cleanup=matched; own-receiver-removed\n", strings.ReplaceAll(valid, "audio-mute=0", "audio-mute=2"), strings.ReplaceAll(valid, "audio-correction=2", "audio-correction=3"), strings.ReplaceAll(valid, "audio-volume=17", "audio-volume=-1"), strings.ReplaceAll(valid, "audio-volume=17", "audio-volume=01"), strings.ReplaceAll(valid, "audio-volume=17", "audio-volume=2147483648")} {
		if _, err := parseNativeAudioOutput([]byte(text)); err == nil {
			t.Fatal("incomplete or unknown audio result accepted")
		}
	}
	if nativeAudioObservation(nativeAudioValue{17, 2, 0}, true, "").Status != "unknown" {
		t.Fatal("unknown mute became usable")
	}
	for _, id := range []string{"master-volume", "volume", "format"} {
		if _, _, _, known := nativeAudioSpec(id); known {
			t.Fatal("unproven setting in allowlist")
		}
	}
}

func TestNativeAudioFailureEvidenceSurvivesSuccessfulRead(t *testing.T) {
	root := t.TempDir()
	failed, err := newNativeAudioOperation(root, true)
	if err != nil {
		t.Fatal(err)
	}
	failed.enter("apply")
	failed.failure("HELPER_EXEC_FAILED")
	failed.finish(errors.New("private error"))
	path := filepath.Join(root, "last-native-audio-change-failure.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	read, err := newNativeAudioOperation(root, false)
	if err != nil {
		t.Fatal(err)
	}
	read.enter("read-current")
	read.finish(nil)
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatal("successful GET erased original failure")
	}
}
