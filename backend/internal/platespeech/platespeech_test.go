package platespeech

import (
	"context"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

func TestTokensReadPlatesAloud(t *testing.T) {
	cases := []struct {
		r    plate.Reading
		want []string
	}{
		{plate.Reading{Region: "世田谷", Class: "810", Kana: "さ", Number: "1919"}, []string{"世田谷", "はち", "いち", "ぜろ", "さ", "19", "19"}},
		{plate.Reading{Region: "熊谷", Class: "30A", Kana: "わ", Number: "1205"}, []string{"くまがや", "さん", "ぜろ", "えー", "わ", "12", "まるごー"}},
		{plate.Reading{Number: "8"}, []string{"はち"}},
		{plate.Reading{Number: "77"}, []string{"77"}},
		{plate.Reading{Number: "100"}, []string{"いち", "まるまる"}},
		{plate.Reading{Kana: "を", Number: "12*4"}, []string{"を"}}, // wildcards are never read
	}
	for _, c := range cases {
		if got := Tokens(c.r); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Tokens(%+v) = %v, want %v", c.r, got, c.want)
		}
	}
	if s := Sentence(cases[0].r); s != "世田谷、はち、いち、ぜろ、さ、19、19。" {
		t.Errorf("sentence %q", s)
	}
}

func TestVocabularyCoversEveryEngineOutput(t *testing.T) {
	vocab := map[string]bool{}
	for _, v := range Vocabulary() {
		vocab[v] = true
	}
	if len(Vocabulary()) != len(vocab) || len(vocab) < 290 || len(vocab) > 320 {
		t.Fatalf("vocabulary size %d", len(vocab))
	}
	readings := []plate.Reading{{Region: "相模", Class: "3YX", Kana: "れ", Number: "9"}, {Region: "豊田", Class: "500", Kana: "は", Number: "1000"}, {Class: "1", Number: "305"}}
	for _, region := range Regions {
		readings = append(readings, plate.Reading{Region: region})
	}
	for _, k := range PlateKana {
		readings = append(readings, plate.Reading{Kana: string(k)})
	}
	for _, r := range readings {
		for _, tok := range Tokens(r) {
			if !vocab[tok] {
				t.Errorf("token %q of %+v not in vocabulary", tok, r)
			}
		}
	}
}

func TestPlanUsesMessageUploadAndExactSentence(t *testing.T) {
	voice := plate.DefaultVoice()
	exact := plate.Rule{ID: "r1", Speak: true, Message: "やりますねぇ！", Region: "世田谷", Class: "810", Kana: "さ", Number: "1919"}
	reading := plate.Reading{Region: "世田谷", Class: "810", Kana: "さ", Number: "1919"}
	if got := Plan(exact, reading, voice); !reflect.DeepEqual(got, []Part{{Text: "やりますねぇ！番号を検知しました。世田谷、はち、いち、ぜろ、さ、19、19。"}}) {
		t.Fatalf("exact plan %+v", got)
	}
	wild := plate.Rule{ID: "r2", Speak: true, Message: "やりますねぇ！", Region: "横浜"}
	got := Plan(wild, plate.Reading{Region: "横浜", Kana: "さ", Number: "8"}, voice)
	want := []Part{{Text: "やりますねぇ！番号を検知しました。"}, {Text: "横浜"}, {Text: "さ"}, {Text: "はち"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("wildcard plan %+v", got)
	}
	wild.MessageAudio = "0123456789abcdef0123"
	got = Plan(wild, plate.Reading{Region: "横浜"}, voice)
	if !reflect.DeepEqual(got, []Part{{Upload: wild.MessageAudio}, {Text: "番号を検知しました。"}, {Text: "横浜"}}) {
		t.Fatalf("upload plan %+v", got)
	}
}

func TestParseWAV(t *testing.T) {
	pcm := []byte{1, 0, 2, 0, 3, 0}
	wav := EncodeWAV(pcm)
	// A LIST chunk before data, like the API's ffmpeg output.
	withList := append(append(append([]byte{}, wav[:36]...), []byte("LIST\x04\x00\x00\x00INFO")...), wav[36:]...)
	for _, b := range [][]byte{wav, withList} {
		got, e := ParseWAV(b)
		if e != nil || !reflect.DeepEqual(got, pcm) {
			t.Fatalf("parse %v %v", got, e)
		}
	}
	bad := append([]byte{}, wav...)
	binary.LittleEndian.PutUint32(bad[24:], 44100)
	if _, e := ParseWAV(bad); e == nil {
		t.Fatal("44.1 kHz accepted")
	}
	if _, e := ParseWAV([]byte("RIFF....WAVE")); e == nil {
		t.Fatal("empty accepted")
	}
}

func TestClientAgainstFakeEndpoint(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		got = r.URL.Path + " " + r.URL.Query().Get("speaker") + " " + r.PostForm.Get("text")
		if r.PostForm.Has("speaker") || r.URL.Query().Has("key") || r.URL.Query().Has("text") {
			t.Error("speaker must be a query parameter; key and text must stay in the POST body")
		}
		switch {
		case r.PostForm.Get("key") != "k":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(403)
			w.Write([]byte(`{"errorMessage": "invalidApiKey"}`))
		case r.URL.Path == "/v2/api/":
			w.Write([]byte(`{"points": 123, "resetInHours": 1.5}`))
		default:
			w.Header().Set("Content-Type", "audio/x-wav")
			w.Write(EncodeWAV([]byte{5, 0}))
		}
	}))
	defer srv.Close()
	c := NewClient()
	ep, e := ValidateEndpoint(srv.URL + "/")
	if e != nil {
		t.Fatal(e)
	}
	pcm, e := c.Synthesize(context.Background(), API{Endpoint: ep, Key: "k"}, "やりますねぇ！", 1)
	if e != nil || !reflect.DeepEqual(pcm, []byte{5, 0}) || got != "/v2/voicevox/audio/ 1 やりますねぇ！" {
		t.Fatalf("synth %v %v %q", pcm, e, got)
	}
	var ae *APIError
	if _, e = c.Synthesize(context.Background(), API{Endpoint: ep, Key: "x"}, "a", 1); !errors.As(e, &ae) || !ae.Permanent() {
		t.Fatalf("bad key %v", e)
	}
	if p, e := c.Points(context.Background(), API{Endpoint: ep, Key: "k"}); e != nil || p.Points != 123 {
		t.Fatalf("points %+v %v", p, e)
	}
	for _, bad := range []string{"ftp://x", "https://u:p@x", "https://x/?a=1", "x"} {
		if _, e := ValidateEndpoint(bad); e == nil {
			t.Errorf("endpoint %q accepted", bad)
		}
	}
}

type fakeSynth struct {
	mu    sync.Mutex
	texts []string
}

func (f *fakeSynth) Synthesize(_ context.Context, api API, text string, style int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.texts = append(f.texts, text)
	if api.Key == "bad" {
		return nil, &APIError{Code: "invalidApiKey", Status: 403}
	}
	return []byte{byte(len(text)), 0}, nil
}
func (f *fakeSynth) Styles(context.Context, API) ([]Style, error) {
	return []Style{{ID: 1, Speaker: "ずんだもん", Name: "あまあま"}}, nil
}
func (f *fakeSynth) Points(context.Context, API) (Points, error) { return Points{Points: 9}, nil }

type fakePlayer struct {
	mu     sync.Mutex
	played [][]byte
}

func (p *fakePlayer) Available() error { return nil }
func (p *fakePlayer) Play(_ context.Context, pcm []byte, _ int) error {
	p.mu.Lock()
	p.played = append(p.played, pcm)
	p.mu.Unlock()
	return nil
}

func ptr(s string) *string { return &s }

func TestManagerGeneratesRulesFirstAndPlays(t *testing.T) {
	dir := t.TempDir()
	synth, player := &fakeSynth{}, &fakePlayer{}
	m := NewManager(dir, synth, player)
	m.pace = 0
	if ep, key := m.APIConfig(); ep != DefaultAPIBase || key {
		t.Fatalf("defaults %q %v", ep, key)
	}
	s := plate.DefaultSettings()
	s.Rules = []plate.Rule{{ID: "r1", Name: "テスト", Enabled: true, Speak: true, Message: "やりますねぇ！", Region: "相模"}, {ID: "r2", Name: "無音", Enabled: true, Region: "品川"}}
	m.Configure(s)
	if ok, why := m.Available(); ok || !strings.Contains(why, "APIキー") {
		t.Fatalf("available without key: %v %q", ok, why)
	}
	m.generate(context.Background())
	if st := m.Status(); st.Job.State != "no-key" || st.Rules["r1"].Ready {
		t.Fatalf("status without key %+v", st)
	}
	if e := m.SetAPI(DefaultAPIBase, ptr("bad")); e != nil {
		t.Fatal(e)
	}
	m.generate(context.Background())
	if st := m.Status(); st.Job.State != "error" || !strings.Contains(st.Job.Error, "無効") {
		t.Fatalf("bad key %+v", st.Job)
	}
	if info, e := os.Stat(filepath.Join(dir, "api.json")); e != nil || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
		t.Fatalf("api file %v %v", info, e)
	}
	if e := m.SetAPI("not a url", nil); e == nil {
		t.Fatal("invalid endpoint accepted")
	}
	synth.texts = nil
	m.SetAPI("https://example.invalid/", ptr("good"))
	if ep, key := m.APIConfig(); ep != "https://example.invalid" || !key {
		t.Fatalf("config %q %v", ep, key)
	}
	// The endpoint changes, the key stays when not given.
	m.SetAPI(DefaultAPIBase, nil)
	if reloaded := NewManager(dir, synth, player); reloaded.api.Key != "good" || reloaded.api.Endpoint != DefaultAPIBase {
		t.Fatalf("reloaded %+v", reloaded.api)
	}
	m.generate(context.Background())
	if synth.texts[0] != "やりますねぇ！番号を検知しました。" {
		t.Fatalf("rule lead not first: %v", synth.texts[:3])
	}
	if n := len(synth.texts); n != len(Vocabulary())+1 {
		t.Fatalf("synthesized %d", n)
	}
	m.generate(context.Background())
	st := m.Status()
	if st.Job.State != "ready" || !st.Rules["r1"].Ready || st.Credit != "VOICEVOX:ずんだもん" {
		t.Fatalf("ready status %+v", st)
	}
	if _, ok := st.Rules["r2"]; ok {
		t.Fatal("non-speaking rule listed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)
	before := len(synth.texts)
	if e := m.Preview(ctx, s.Rules[0]); e != nil {
		t.Fatal(e)
	}
	synth.mu.Lock()
	after := len(synth.texts)
	synth.mu.Unlock()
	if after != before {
		t.Fatal("preview synthesized instead of using stored clips")
	}
	player.mu.Lock()
	played := len(player.played)
	player.mu.Unlock()
	if played != 1 {
		t.Fatalf("played %d", played)
	}

	// Changing the message removes the old clip and generates the new one.
	s.Rules[0].Message = "変更後のメッセージ"
	m.Configure(s)
	deadline := time.Now().Add(3 * time.Second)
	for !m.Status().Rules["r1"].Ready && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !m.Status().Rules["r1"].Ready {
		t.Fatal("new message not generated")
	}
	if _, e := os.Stat(m.clipPath(1, "やりますねぇ！番号を検知しました。")); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("old clip kept: %v", e)
	}
}

// rateLimited returns 429 for the first n calls.
type rateLimited struct {
	fakeSynth
	left int
}

func (r *rateLimited) Synthesize(ctx context.Context, api API, text string, style int) ([]byte, error) {
	if r.left > 0 {
		r.left--
		return nil, &APIError{Code: "http-429", Status: 429}
	}
	return r.fakeSynth.Synthesize(ctx, api, text, style)
}

func TestRateLimitWaitsAndSavedRulesJumpTheQueue(t *testing.T) {
	synth := &rateLimited{left: 1}
	m := NewManager(t.TempDir(), synth, &fakePlayer{})
	m.pace = 0
	m.SetAPI(DefaultAPIBase, ptr("k"))
	m.Configure(plate.DefaultSettings())
	if wait := m.generate(context.Background()); wait < time.Minute || m.Status().Job.State != "waiting" || len(m.failures) != 0 {
		t.Fatalf("429 handling wait=%v job=%+v failures=%v", wait, m.Status().Job, m.failures)
	}
	// Saving a rule while vocabulary is being generated restarts the job
	// with the rule's message first.
	s := plate.DefaultSettings()
	s.Rules = []plate.Rule{{ID: "r1", Name: "x", Enabled: true, Speak: true, Message: "やりますねぇ！", Region: "相模"}}
	m.Configure(s)
	m.generate(context.Background())
	if synth.texts[0] != "やりますねぇ！番号を検知しました。" {
		t.Fatalf("first synthesized %q", synth.texts[0])
	}
}

// flaky fails one text once with a non-rate-limit API error.
type flaky struct {
	fakeSynth
	text string
	done bool
}

func (f *flaky) Synthesize(ctx context.Context, api API, text string, style int) ([]byte, error) {
	if text == f.text && !f.done {
		f.done = true
		return nil, &APIError{Code: "serverError", Status: 500}
	}
	return f.fakeSynth.Synthesize(ctx, api, text, style)
}

func TestTransientFailureIsRetried(t *testing.T) {
	synth := &flaky{text: "26"}
	m := NewManager(t.TempDir(), synth, &fakePlayer{})
	m.pace = 0
	m.SetAPI(DefaultAPIBase, ptr("k"))
	m.Configure(plate.DefaultSettings())
	m.generate(context.Background()) // everything but "26"
	if wait := m.generate(context.Background()); wait != 30*time.Minute || m.Status().Job.Ready != len(Vocabulary())-1 {
		t.Fatalf("after failure wait=%v job=%+v", wait, m.Status().Job)
	}
	m.generate(context.Background()) // retry
	m.generate(context.Background())
	if st := m.Status().Job; st.State != "ready" || st.Ready != len(Vocabulary()) {
		t.Fatalf("not retried: %+v", st)
	}
}

func TestUploadsAndMissingClipFailure(t *testing.T) {
	m := NewManager(t.TempDir(), &fakeSynth{}, &fakePlayer{})
	id, e := m.SaveUpload([]byte{9, 0, 9, 0})
	if e != nil || !plate.AudioIDPattern.MatchString(id) || !m.HasUpload(id) || m.HasUpload("../api") {
		t.Fatalf("upload %q %v", id, e)
	}
	if e := m.play(context.Background(), []Part{{Upload: "ffffffffffffffffffff"}}, 50); e == nil {
		t.Fatal("missing upload played")
	}
	if e := m.play(context.Background(), []Part{{Text: "横浜"}}, 50); e == nil || !strings.Contains(e.Error(), "APIキー") {
		t.Fatalf("missing clip without key: %v", e)
	}
}
