package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/config"
	"github.com/np1-control-center/ncc/internal/plate"
	"github.com/np1-control-center/ncc/internal/platespeech"
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

// recordingSpeaker stands in for the device speaker; it reports playback
// as done immediately so the record state can be checked.
type recordingSpeaker struct {
	said  []string
	rules []plate.Rule
}

func (s *recordingSpeaker) Available() (bool, string) { return true, "" }
func (s *recordingSpeaker) Announce(_ context.Context, rule plate.Rule, reading plate.Reading, done func(error)) bool {
	s.said = append(s.said, rule.Name+"|"+rule.Message+"|"+reading.Display())
	s.rules = append(s.rules, rule)
	done(nil)
	return true
}

func rawRequest(t *testing.T, client *http.Client, server *httptest.Server, method, path, csrf, contentType string, body []byte) *http.Response {
	t.Helper()
	req, e := http.NewRequest(method, server.URL+"/api/v1"+path, bytes.NewReader(body))
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Origin", server.URL)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Request-ID", "test-upload")
	req.Header.Set("X-CSRF-Token", csrf)
	res, e := client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	return res
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

func TestPlateEmptySettingsRulesRemainJSONArray(t *testing.T) {
	_, ts, client, csrf := plateFixture(t, &fakeSensor{instance: "empty"})
	for phase := 0; phase < 2; phase++ {
		if phase == 1 {
			response := putPlateSettings(t, client, ts, csrf, 1, plate.DefaultSettings())
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatal(response.StatusCode)
			}
		}
		response := request(t, client, ts, "GET", "/plate/settings", csrf, nil)
		var value map[string]json.RawMessage
		err := json.NewDecoder(response.Body).Decode(&value)
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK || string(value["rules"]) != "[]" {
			t.Fatalf("empty rules must be an array, phase %d: %s, %v", phase, value["rules"], err)
		}
	}
}

func TestLEDRuleNotifiesWithoutSpeechOrAPIKey(t *testing.T) {
	srv, _, _, _ := plateFixture(t, &fakeSensor{instance: "led"})
	r := plate.Rule{ID: "r1", Name: "LED only", Enabled: true, Number: "42"}
	r.LED.Mode = "solid"
	r.LED.Effect = "flow"
	r.LED.Brightness = 180
	r.LED.RGB = [3]int{0, 100, 255}
	srv.plates.settings.Rules = []plate.Rule{r}
	speaker := &recordingSpeaker{}
	srv.plates.speaker = speaker
	outcome := plate.Outcome{Record: plate.Record{ID: "synthetic", Reading: plate.Reading{Number: "42"}}, NewMatches: []plate.Match{{RuleID: r.ID, RuleName: r.Name}}}
	srv.plates.speak(context.Background(), outcome)
	if len(speaker.said) != 1 {
		t.Fatal("LED-only match was skipped")
	}
}

func TestLEDOnlyRuleDoesNotSuppressAnotherRulesSpeech(t *testing.T) {
	srv, _, _, _ := plateFixture(t, &fakeSensor{instance: "led-multiple"})
	a := plate.Rule{ID: "light", Name: "light", Enabled: true, Number: "42"}
	a.LED.Mode = "rainbow"
	b := plate.Rule{ID: "voice", Name: "voice", Enabled: true, Speak: true, Number: "42"}
	srv.plates.settings.Rules = []plate.Rule{a, b}
	speaker := &recordingSpeaker{}
	srv.plates.speaker = speaker
	srv.plates.speak(context.Background(), plate.Outcome{Record: plate.Record{ID: "synthetic", Reading: plate.Reading{Number: "42"}}, NewMatches: []plate.Match{{RuleID: "light"}, {RuleID: "voice", Speak: true}}})
	if len(speaker.rules) != 1 || !speaker.rules[0].Speak || speaker.rules[0].ID != "voice" || speaker.rules[0].LED.Mode != "rainbow" {
		t.Fatal("LED-only match suppressed or duplicated speech")
	}
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
	s.Rules = []plate.Rule{{ID: "r1", Name: "対象", Enabled: true, Number: "1234", SaveImage: true, Speak: true, Message: "やりますねぇ！"}}
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
	if len(said) != 1 || said[0] != "対象|やりますねぇ！|12-34" {
		t.Fatalf("speech %v", said)
	}
	// The second reading is an hour later, which falls on the next JST day when the test runs
	// between 23:00 and 24:00 JST; ask for both days instead of the default range.
	jst := time.FixedZone("JST", 9*60*60)
	res = request(t, client, ts, "GET", "/plate/records?matched=true&from="+now.In(jst).Format("2006-01-02")+"&to="+now.Add(time.Hour).In(jst).Format("2006-01-02"), "", nil)
	json.NewDecoder(res.Body).Decode(&body)
	res.Body.Close()
	spoken := false
	for _, r := range body.Records {
		spoken = spoken || r.Record.Speech == plate.SpeechSpoken
	}
	if !spoken {
		t.Fatalf("record not marked spoken after playback: %+v", body.Records)
	}
}

func TestPlateSpeechUploadsAndVoicevoxSettings(t *testing.T) {
	srv, ts, client, csrf := plateFixture(t, &fakeSensor{instance: "a"})
	// Defaults: endpoint set, key empty, speech unavailable until configured.
	res := request(t, client, ts, "GET", "/voicevox", "", nil)
	var cfg struct {
		Endpoint, DefaultEndpoint string
		KeyConfigured             bool
	}
	json.NewDecoder(res.Body).Decode(&cfg)
	res.Body.Close()
	if cfg.Endpoint != "https://deprecatedapis.tts.quest" || cfg.KeyConfigured {
		t.Fatalf("defaults %+v", cfg)
	}
	res = request(t, client, ts, "GET", "/plate/speech", "", nil)
	var speech struct {
		Available bool
		Reason    string
	}
	json.NewDecoder(res.Body).Decode(&speech)
	res.Body.Close()
	if speech.Available || speech.Reason == "" {
		t.Fatalf("speech without key %+v", speech)
	}
	key := "fictional-key"
	if res = request(t, client, ts, "PUT", "/voicevox", csrf, map[string]any{"endpoint": "https://voice.example.invalid/", "key": key}); res.StatusCode != 200 {
		t.Fatalf("put voicevox %d", res.StatusCode)
	}
	json.NewDecoder(res.Body).Decode(&cfg)
	res.Body.Close()
	if cfg.Endpoint != "https://voice.example.invalid" || !cfg.KeyConfigured {
		t.Fatalf("saved %+v", cfg)
	}
	// The key is never echoed back.
	res = request(t, client, ts, "GET", "/voicevox", "", nil)
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if strings.Contains(string(raw), key) {
		t.Fatal("key returned to client")
	}
	if res = request(t, client, ts, "PUT", "/voicevox", csrf, map[string]any{"endpoint": "ftp://x"}); res.StatusCode != 400 {
		t.Fatalf("bad endpoint %d", res.StatusCode)
	}
	res.Body.Close()
	if res = request(t, client, ts, "PUT", "/voicevox", "", map[string]any{"endpoint": "https://x"}); res.StatusCode == 200 {
		t.Fatal("voicevox write without CSRF accepted")
	}
	res.Body.Close()

	// Upload: only 24 kHz mono 16-bit WAV is accepted.
	wav := platespeech.EncodeWAV(make([]byte, 4800))
	up := rawRequest(t, client, ts, "POST", "/plate/speech/uploads", csrf, "audio/wav", wav)
	var uploaded struct {
		ID         string
		DurationMs int
	}
	upBody, _ := io.ReadAll(up.Body)
	up.Body.Close()
	json.Unmarshal(upBody, &uploaded)
	if up.StatusCode != 200 || !plate.AudioIDPattern.MatchString(uploaded.ID) || uploaded.DurationMs != 100 {
		t.Fatalf("upload %d %+v %s", up.StatusCode, uploaded, upBody)
	}
	if bad := rawRequest(t, client, ts, "POST", "/plate/speech/uploads", csrf, "audio/wav", []byte("not audio")); bad.StatusCode != 400 {
		t.Fatalf("invalid audio %d", bad.StatusCode)
	}
	get := request(t, client, ts, "GET", "/plate/speech/uploads/"+uploaded.ID, "", nil)
	back, _ := io.ReadAll(get.Body)
	get.Body.Close()
	if get.StatusCode != 200 || len(back) != len(wav) {
		t.Fatalf("download %d %d", get.StatusCode, len(back))
	}

	s := plate.DefaultSettings()
	s.Rules = []plate.Rule{{ID: "r1", Name: "録音", Enabled: true, Region: "相模", Speak: true, MessageAudio: "ffffffffffffffffffff"}}
	if res = putPlateSettings(t, client, ts, csrf, 1, s); res.StatusCode != 400 {
		t.Fatalf("unknown upload accepted %d", res.StatusCode)
	}
	res.Body.Close()
	s.Rules[0].MessageAudio = uploaded.ID
	if res = putPlateSettings(t, client, ts, csrf, 1, s); res.StatusCode != 200 {
		t.Fatalf("upload rule %d", res.StatusCode)
	}
	res.Body.Close()
	if st := srv.plates.voice.Status(); !st.KeyConfigured {
		t.Fatalf("status %+v", st)
	}
	if res = request(t, client, ts, "POST", "/plate/speech/preview", csrf, map[string]any{"ruleId": "missing"}); res.StatusCode != 404 {
		t.Fatalf("preview of unsaved rule %d", res.StatusCode)
	}
	res.Body.Close()
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
