package platespeech

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/led"
	"github.com/np1-control-center/ncc/internal/plate"
)

func TestPreparedSpeechIsLocalAndWaitsForTheSpeaker(t *testing.T) {
	ctx := context.Background()
	synth, player := &fakeSynth{}, &fakePlayer{}
	m := NewManager(t.TempDir(), synth, player)
	m.pace = 0
	if err := m.SetAPI(DefaultAPIBase, ptr("test-key")); err != nil {
		t.Fatal(err)
	}
	texts := []string{"歩行者用信号", "27秒です。"}
	if err := m.Prepare(ctx, 3, texts); err != nil {
		t.Fatal(err)
	}
	if err := m.SetAPI(DefaultAPIBase, ptr("")); err != nil {
		t.Fatal(err)
	}
	allow := func(time.Duration) bool { return true }
	// A plate announcement holds the speaker: the signal waits, then plays.
	m.playback.Lock()
	release := time.AfterFunc(100*time.Millisecond, m.playback.Unlock)
	defer release.Stop()
	if err := m.PlayPrepared(ctx, 3, 60, texts, allow); err != nil {
		t.Fatalf("prepared speech did not wait for the speaker: %v", err)
	}
	// It gives up after slotWait; allow decides validity after the wait.
	m.slotWait = 50 * time.Millisecond
	m.playback.Lock()
	err := m.PlayPrepared(ctx, 3, 60, texts, allow)
	m.playback.Unlock()
	if err == nil {
		t.Fatal("prepared speech played while the speaker stayed busy")
	}
	if err := m.PlayPrepared(ctx, 3, 60, texts, func(time.Duration) bool { return false }); err == nil {
		t.Fatal("an expired announcement played")
	}
	if len(player.played) != 1 || len(synth.texts) != 2 {
		t.Fatalf("playback or synthesis repeated: plays=%d synth=%d", len(player.played), len(synth.texts))
	}
}

// failingPlayer refuses the first fails plays, as PulseAudio does while the
// stock audio services restart.
type failingPlayer struct {
	mu    sync.Mutex
	fails int
	calls int
}

func (p *failingPlayer) Available() error { return nil }
func (p *failingPlayer) Play(context.Context, []byte, int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.calls <= p.fails {
		return errors.New("connection refused")
	}
	return nil
}

func TestPlaybackRetriesAFailedStart(t *testing.T) {
	player := &failingPlayer{fails: playRetries}
	m := NewManager(t.TempDir(), &fakeSynth{}, player)
	if err := m.playPCM(context.Background(), make([]byte, bytesPerSec), 50); err != nil || player.calls != playRetries+1 {
		t.Fatalf("retry: err=%v calls=%d", err, player.calls)
	}
	player = &failingPlayer{fails: playRetries + 1}
	m = NewManager(t.TempDir(), &fakeSynth{}, player)
	if err := m.playPCM(context.Background(), make([]byte, bytesPerSec), 50); err == nil || player.calls != playRetries+1 {
		t.Fatalf("persistent failure: err=%v calls=%d", err, player.calls)
	}
}

func TestDebugAnnouncementUsesOnlyCachedClips(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	synth, player := &fakeSynth{}, &fakePlayer{}
	m := NewManager(t.TempDir(), synth, player)
	m.pace = 0
	if err := m.SetAPI(DefaultAPIBase, ptr("k")); err != nil {
		t.Fatal(err)
	}
	go m.Run(ctx)
	reading := plate.Reading{Region: "相模", Class: "503", Kana: "あ", Number: "1234"}
	announce := func() error {
		t.Helper()
		result := make(chan error, 1)
		if !m.AnnounceReading(ctx, reading, led.Notification{}, func(e error) { result <- e }) {
			t.Fatal("debug announcement dropped")
		}
		select {
		case e := <-result:
			return e
		case <-time.After(5 * time.Second):
			t.Fatal("debug announcement never finished")
			return nil
		}
	}
	// Nothing is cached yet (no settings saved): nothing may be synthesized.
	if err := announce(); err == nil || len(player.played) != 0 {
		t.Fatalf("debug speech without cache: err=%v plays=%d", err, len(player.played))
	}
	synth.mu.Lock()
	synth.texts = nil
	synth.mu.Unlock()
	m.Configure(plate.DefaultSettings())
	deadline := time.Now().Add(10 * time.Second)
	for m.Status().Job.State != "ready" {
		if time.Now().After(deadline) {
			t.Fatalf("vocabulary not prepared: %+v", m.Status().Job)
		}
		time.Sleep(10 * time.Millisecond)
	}
	synth.mu.Lock()
	cached := len(synth.texts)
	synth.mu.Unlock()
	if err := announce(); err != nil || len(player.played) != 1 {
		t.Fatalf("debug speech from cache: err=%v plays=%d", err, len(player.played))
	}
	synth.mu.Lock()
	defer synth.mu.Unlock()
	if len(synth.texts) != cached {
		t.Fatalf("debug speech called the API: %v", synth.texts[cached:])
	}
	for _, text := range synth.texts {
		if text == "相模、ご、まる、さん、あ、いち、に、さん、よん。" {
			t.Fatal("a detected plate was sent to the API")
		}
	}
}

// Right after setup, a restore or a voice change the vocabulary is still being
// prepared. A rule match then says its message alone: the detected plate is
// never fetched from the API, piece by piece or otherwise.
func TestRuleMatchSpeaksTheMessageWhileThePlateIsNotCached(t *testing.T) {
	ctx := context.Background()
	synth, player := &fakeSynth{}, &fakePlayer{}
	m := NewManager(t.TempDir(), synth, player)
	m.pace = 0
	if err := m.SetAPI(DefaultAPIBase, ptr("k")); err != nil {
		t.Fatal(err)
	}
	rule := plate.Rule{ID: "r", Speak: true, Message: "注意車両です。", Region: "相模"}
	reading := plate.Reading{Region: "相模", Class: "503", Kana: "あ", Number: "1234"}
	plan := Plan(rule, reading, m.voice)
	lead := plan[0].Text
	if err := m.playNotification(ctx, plan, 60, led.Notification{}, false); err != nil {
		t.Fatalf("message not spoken while the plate is prepared: %v", err)
	}
	if len(synth.texts) != 1 || synth.texts[0] != lead {
		t.Fatalf("sent to the API: %q", synth.texts)
	}
	if len(player.played) != 1 {
		t.Fatalf("plays %d", len(player.played))
	}
	short := len(player.played[0])
	for _, token := range Tokens(reading) {
		if err := writeAtomic(m.clipPath(m.voice.Style, token), []byte{1, 0}, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.playNotification(ctx, plan, 60, led.Notification{}, false); err != nil {
		t.Fatal(err)
	}
	if len(synth.texts) != 1 || len(player.played) != 2 || len(player.played[1]) <= short {
		t.Fatalf("cached plate not read: api %q plays %d", synth.texts, len(player.played))
	}
	// Without a message there is nothing to say until the plate is cached.
	bare := NewManager(t.TempDir(), synth, player)
	bare.SetAPI(DefaultAPIBase, ptr("k"))
	if err := bare.playNotification(ctx, []Part{{Text: "相模", Vocab: true}}, 60, led.Notification{}, false); !errors.Is(err, ErrNotCached) {
		t.Fatalf("uncached plate alone: %v", err)
	}
	if len(synth.texts) != 1 {
		t.Fatalf("sent to the API: %q", synth.texts)
	}
}

// The owner's own rule may be previewed before the vocabulary is ready.
func TestRulePreviewMaySynthesizeTheOwnersPlate(t *testing.T) {
	synth := &fakeSynth{}
	m := NewManager(t.TempDir(), synth, &fakePlayer{})
	m.SetAPI(DefaultAPIBase, ptr("k"))
	rule := plate.Rule{ID: "r", Speak: true, Message: "注意車両です。", Region: "相模"}
	if _, err := m.PreviewAudio(context.Background(), rule); err != nil {
		t.Fatal(err)
	}
	if len(synth.texts) != 2 || synth.texts[1] != "相模" {
		t.Fatalf("preview synthesized %q", synth.texts)
	}
}

func TestVocabCacheReportsAndRegenerates(t *testing.T) {
	synth := &fakeSynth{}
	m := NewManager(t.TempDir(), synth, &fakePlayer{})
	m.pace = 0
	m.SetAPI(DefaultAPIBase, ptr("k"))
	s := plate.DefaultSettings()
	s.Rules = []plate.Rule{{ID: "r1", Enabled: true, Speak: true, Message: "注意", Region: "相模"}}
	m.Configure(s)
	if c, job := m.VocabCache(); c.Ready != 0 || c.Total != len(Vocabulary())+1 || len(c.Missing) != 20 || job.State != "idle" {
		t.Fatalf("before %+v %+v", c, job)
	}
	m.generate(context.Background())
	m.generate(context.Background())
	if c, job := m.VocabCache(); c.Ready != c.Total || c.Bytes == 0 || len(c.Missing) != 0 || job.State != "ready" {
		t.Fatalf("after %+v %+v", c, job)
	}
	if err := m.RegenerateVocab(); err != nil {
		t.Fatal(err)
	}
	if c, _ := m.VocabCache(); c.Ready != 0 || c.Bytes != 0 {
		t.Fatalf("not deleted %+v", c)
	}
	select {
	case <-m.wake:
	default:
		t.Fatal("generation not woken")
	}
	sent := len(synth.texts)
	m.generate(context.Background())
	if c, _ := m.VocabCache(); c.Ready != c.Total || len(synth.texts) != sent+c.Total {
		t.Fatalf("not regenerated %+v", c)
	}
}
