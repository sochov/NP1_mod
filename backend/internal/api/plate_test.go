package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/config"
	"github.com/np1-control-center/ncc/internal/plate"
)

// fakeSensor serves the sensor protocol from memory. It is a test double for
// the protocol only; it does not claim any recognition capability.
type fakeSensor struct {
	instance   string
	detections []plate.Detection
	frames     map[string][]byte
	frameHits  int
}

func (f *fakeSensor) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(PlateSensorStatus{State: "model-missing", Reason: "fixture", Instance: f.instance})
	})
	mux.HandleFunc("GET /detections", func(w http.ResponseWriter, r *http.Request) {
		next := uint64(0)
		for _, d := range f.detections {
			if d.Seq > next {
				next = d.Seq
			}
		}
		json.NewEncoder(w).Encode(plateBatch{Instance: f.instance, Next: next, Detections: f.detections})
	})
	mux.HandleFunc("GET /frames/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.frameHits++
		b, ok := f.frames[r.PathValue("id")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	})
	return mux
}

func sensorClient(t *testing.T, h http.Handler) *http.Client {
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	target, _ := url.Parse(s.URL)
	return &http.Client{Transport: rewriteTransport{target}}
}

type rewriteTransport struct{ target *url.URL }

func (t rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host = t.target.Scheme, t.target.Host
	return http.DefaultTransport.RoundTrip(r)
}

type recordingSpeaker struct{ said []string }

func (s *recordingSpeaker) Available() (bool, string) { return true, "" }
func (s *recordingSpeaker) Speak(_ context.Context, text string) error {
	s.said = append(s.said, text)
	return nil
}

func plateFixture(t *testing.T, sensor *fakeSensor) (*Server, *httptest.Server, *http.Client, string) {
	t.Helper()
	salt := []byte("test-salt-16bytes")
	c := config.Config{Username: "owner", Salt: salt, PasswordHash: config.Hash("fictional-test-password", salt), StateDir: t.TempDir()}
	srv := New(c)
	srv.plates.sensor = sensorClient(t, sensor.handler())
	ts := httptest.NewTLSServer(srv.Handler())
	t.Cleanup(ts.Close)
	client := ts.Client()
	client.Jar, _ = cookiejar.New(nil)
	return srv, ts, client, login(t, client, ts)
}

func putPlateSettings(t *testing.T, client *http.Client, ts *httptest.Server, csrf string, expected int, s plate.Settings) *http.Response {
	t.Helper()
	return request(t, client, ts, "PUT", "/plate/settings", csrf, map[string]any{"expectedRevision": expected, "settings": s})
}

func TestPlateSettingsRevisionAndValidation(t *testing.T) {
	_, ts, client, csrf := plateFixture(t, &fakeSensor{instance: "a"})
	s := plate.DefaultSettings()
	s.Enabled = true
	s.Rules = []plate.Rule{{ID: "r1", Name: "対象", Enabled: true, Number: "１２３４"}}
	res := putPlateSettings(t, client, ts, csrf, 1, s)
	var saved plate.Settings
	json.NewDecoder(res.Body).Decode(&saved)
	res.Body.Close()
	if res.StatusCode != 200 || saved.Revision != 2 || saved.Rules[0].Number != "1234" {
		t.Fatalf("save %d %+v", res.StatusCode, saved)
	}
	if res = putPlateSettings(t, client, ts, csrf, 1, s); res.StatusCode != 409 {
		t.Fatalf("stale revision %d", res.StatusCode)
	}
	res.Body.Close()
	s.Rules[0].Number = "12345"
	if res = putPlateSettings(t, client, ts, csrf, 2, s); res.StatusCode != 400 {
		t.Fatalf("invalid rule %d", res.StatusCode)
	}
	res.Body.Close()
	if res = putPlateSettings(t, client, ts, "", 2, s); res.StatusCode == 200 {
		t.Fatal("write without CSRF accepted")
	}
	res.Body.Close()
}

func TestPlatePollRecordsImageAndUnsupportedSpeech(t *testing.T) {
	now := time.Now().UTC()
	sensor := &fakeSensor{instance: "boot-1", frames: map[string][]byte{"f1": {0xFF, 0xD8, 0xFF, 0xD9}}, detections: []plate.Detection{
		{Seq: 1, At: now, Frame: "f1", Reading: plate.Reading{Region: "品川", Class: "300", Kana: "あ", Number: "1234", Confidence: 0.9}, Location: &plate.Location{At: now, Lat: 35.6, Lon: 139.7}},
		{Seq: 2, At: now, Frame: "f2", Reading: plate.Reading{Region: "練馬", Number: "77", Confidence: 0.9}},
	}}
	srv, ts, client, csrf := plateFixture(t, sensor)
	s := plate.DefaultSettings()
	s.Enabled = true
	s.MinSightings = 1
	s.Rules = []plate.Rule{{ID: "r1", Name: "対象", Enabled: true, Number: "1234", SaveImage: true, Speak: true}}
	putPlateSettings(t, client, ts, csrf, 1, s).Body.Close()

	if n := srv.plates.poll(context.Background()); n != 2 {
		t.Fatalf("ingested %d", n)
	}
	// The same batch again must not duplicate records (cursor), and a sensor
	// restart (new instance) must reset the cursor without losing data.
	if n := srv.plates.poll(context.Background()); n != 0 {
		t.Fatalf("re-ingested %d", n)
	}
	// A restarted server resumes the cursor instead of re-ingesting.
	restarted := newPlateService(srv.cfg.StateDir, srv.plates.sensor)
	if n := restarted.poll(context.Background()); n != 0 {
		t.Fatalf("restart re-ingested %d detections", n)
	}
	if sensor.frameHits != 1 {
		t.Fatalf("frames fetched %d (only the matched record saves an image)", sensor.frameHits)
	}
	res := request(t, client, ts, "GET", "/plate/records", "", nil)
	var body struct {
		Records []struct {
			Record         plate.Record `json:"record"`
			ImageAvailable bool         `json:"imageAvailable"`
		} `json:"records"`
	}
	json.NewDecoder(res.Body).Decode(&body)
	res.Body.Close()
	if len(body.Records) != 2 {
		t.Fatalf("records %+v", body)
	}
	var matched plate.Record
	for _, r := range body.Records {
		if len(r.Record.Matches) > 0 {
			matched = r.Record
			if !r.ImageAvailable {
				t.Fatal("matched image not available")
			}
		}
	}
	if matched.Speech != plate.SpeechUnsupported {
		t.Fatalf("speech = %q; unsupported speech must never be reported as spoken", matched.Speech)
	}
	if matched.Location == nil || matched.Location.Lat != 35.6 {
		t.Fatalf("location lost: %+v", matched.Location)
	}
	img := request(t, client, ts, "GET", "/plate/records/"+matched.ID+"/image", "", nil)
	if img.StatusCode != 200 || img.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("image %d %s", img.StatusCode, img.Header.Get("Content-Type"))
	}
	img.Body.Close()
	if res := request(t, client, ts, "GET", "/plate/records/..%2fsettings/image", "", nil); res.StatusCode == 200 {
		t.Fatal("traversal served")
	}
	if res := request(t, client, ts, "GET", "/plate/records?bogus=1", "", nil); res.StatusCode != 400 {
		t.Fatalf("unknown query %d", res.StatusCode)
	}

	srv.plates.speaker = &recordingSpeaker{}
	sensor.instance = "boot-2"
	sensor.detections = []plate.Detection{{Seq: 1, At: now.Add(time.Hour), Reading: plate.Reading{Number: "1234", Confidence: 0.9}}}
	if n := srv.plates.poll(context.Background()); n != 1 {
		t.Fatalf("after restart ingested %d", n)
	}
	said := srv.plates.speaker.(*recordingSpeaker).said
	if len(said) != 1 || !strings.Contains(said[0], "対象") {
		t.Fatalf("speech %v", said)
	}
}

func TestPlateStatusWhenSensorMissing(t *testing.T) {
	srv, ts, client, _ := plateFixture(t, &fakeSensor{})
	srv.plates.sensor = unixClient(t.TempDir() + "/missing.sock")
	srv.plates.refreshStatus(context.Background())
	res := request(t, client, ts, "GET", "/plate/status", "", nil)
	var status struct {
		Sensor PlateSensorStatus `json:"sensor"`
		Speech struct {
			Available bool `json:"available"`
		} `json:"speech"`
	}
	json.NewDecoder(res.Body).Decode(&status)
	res.Body.Close()
	if status.Sensor.State != "unavailable" || status.Speech.Available {
		t.Fatalf("status %+v", status)
	}
}
