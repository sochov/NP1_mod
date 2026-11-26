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
