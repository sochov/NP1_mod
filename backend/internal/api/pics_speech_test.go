package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/pics"
	"github.com/np1-control-center/ncc/internal/platespeech"
)

// picsSynth and picsPlayer stand in for the VOICEVOX API and the speaker.
type picsSynth struct {
	mu    sync.Mutex
	texts []string
}

func (s *picsSynth) Synthesize(_ context.Context, _ platespeech.API, text string, _ int) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.texts = append(s.texts, text)
	return []byte{1, 0, 2, 0}, nil
}
func (s *picsSynth) Styles(context.Context, platespeech.API) ([]platespeech.Style, error) {
	return nil, nil
}
func (s *picsSynth) Points(context.Context, platespeech.API) (platespeech.Points, error) {
	return platespeech.Points{}, nil
}

type picsPlayer struct {
	mu     sync.Mutex
	played int
}

func (p *picsPlayer) Available() error { return nil }
func (p *picsPlayer) Play(context.Context, []byte, int) error {
	p.mu.Lock()
	p.played++
	p.mu.Unlock()
	return nil
}
func (p *picsPlayer) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.played
}

func picsFrame(lamps ...byte) []byte {
	return append([]byte{0, 0, 2, 255, 0, 0, 0, 0, 0, 1}, lamps...)
}

func preparedPICS(t *testing.T) (*picsService, *picsPlayer, string) {
	t.Helper()
	dir := t.TempDir()
	player := &picsPlayer{}
	voice := platespeech.NewManager(filepath.Join(dir, "speech"), &picsSynth{}, player)
	key := "test-key"
	if err := voice.SetAPI(platespeech.DefaultAPIBase, &key); err != nil {
		t.Fatal(err)
	}
	p := newPICSService(dir, voice)
	p.settings = picsSettings{Enabled: true, Speech: true, Style: 1, Volume: 60}
	if err := voice.Prepare(context.Background(), 1, []string{pics.GreenText}); err != nil {
		t.Fatal(err)
	}
	p.speechPrepared = true
	return p, player, dir
}

func waitPICSDecision(t *testing.T, p *picsService, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		p.mu.Lock()
		got, idle := p.speechDecision, p.playing == nil
		p.mu.Unlock()
		if got == want && idle {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("speech decision %q, want %q", got, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// 2026-11-26: the crossing was heard every 2.5-9 s on the road and turned
// green 4.7 s after its last red report. The LED lit; speech must follow.
func TestPICSSpeaksGreenAfterASparseRoadGap(t *testing.T) {
	p, player, _ := preparedPICS(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Now()
	p.mu.Lock()
	p.monitor.Receive("sender", picsFrame(0x11), now.Add(-4700*time.Millisecond))
	p.monitor.Receive("sender", picsFrame(0x83), now)
	p.mu.Unlock()
	p.tickSpeech(ctx)
	waitPICSDecision(t, p, "played-green")
	if player.count() != 1 {
		t.Fatalf("plays %d", player.count())
	}
}

func TestPICSRefetchesAMissingClipInsteadOfStayingSilent(t *testing.T) {
	p, player, dir := preparedPICS(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := os.RemoveAll(filepath.Join(dir, "speech", "prepared")); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	p.mu.Lock()
	p.monitor.Receive("sender", picsFrame(0x11), now.Add(-time.Second))
	p.monitor.Receive("sender", picsFrame(0x83), now)
	p.mu.Unlock()
	p.tickSpeech(ctx)
	p.mu.Lock()
	decision, prepared := p.speechDecision, p.speechPrepared
	p.mu.Unlock()
	if decision != "clip-unavailable" || prepared {
		t.Fatalf("missing clip: decision %q prepared %v", decision, prepared)
	}
	select {
	case <-p.speechChanged:
	default:
		t.Fatal("preparation was not woken to fetch the clip again")
	}
	if err := p.voice.Prepare(ctx, 1, []string{pics.GreenText}); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.speechPrepared = true
	p.mu.Unlock()
	p.tickSpeech(ctx)
	waitPICSDecision(t, p, "played-green")
	if player.count() != 1 {
		t.Fatalf("plays %d", player.count())
	}
}

func TestPICSFreesAStalledPlaybackAndReportsAStalledLoop(t *testing.T) {
	p, _, _ := preparedPICS(t)
	if err := p.healthy(time.Now()); err != nil {
		t.Fatalf("not started yet: %v", err)
	}
	p.heartbeat.Store(time.Now().Add(-picsStallLimit - time.Second).UnixNano())
	if err := p.healthy(time.Now()); err == nil {
		t.Fatal("stalled speech loop reported healthy")
	}
	p.heartbeat.Store(time.Now().UnixNano())
	if err := p.healthy(time.Now()); err != nil {
		t.Fatal(err)
	}
	cancelled := false
	p.mu.Lock()
	p.playing, p.playingSince = &pics.Cue{Kind: "green"}, time.Now().Add(-picsPlayLimit-time.Second)
	p.playCancel = func() { cancelled = true }
	p.mu.Unlock()
	p.tickSpeech(context.Background())
	p.mu.Lock()
	defer p.mu.Unlock()
	if !cancelled || p.playing != nil || p.speechDecision != "playback-stalled" {
		t.Fatalf("stalled playback kept the speaker: cancelled=%v decision=%q", cancelled, p.speechDecision)
	}
}

func TestPICSSpeechDecisionsSurviveInAFile(t *testing.T) {
	p, _, dir := preparedPICS(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.writeSpeechLog(ctx)
	p.mu.Lock()
	p.notePICSSpeech("cache-not-ready")
	p.notePICSSpeech("cache-not-ready")
	p.notePICSSpeech("played-green")
	p.mu.Unlock()
	path := filepath.Join(dir, "pics", "speech-log.jsonl")
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, _ := os.ReadFile(path)
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) == 2 && strings.Contains(lines[0], `"decision":"cache-not-ready"`) && strings.Contains(lines[1], `"decision":"played-green"`) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("speech log %q", data)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A cue needs only its own clips. While the rest is still prepared (first
// setup, a new voice, no uplink on the road) the green message is spoken.
func TestPICSSpeaksCachedCuesBeforeTheWholeSetIsReady(t *testing.T) {
	p, player, _ := preparedPICS(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.mu.Lock()
	p.speechPrepared = false // e.g. "37秒です。" could not be fetched yet
	now := time.Now()
	p.monitor.Receive("sender", picsFrame(0x11), now.Add(-time.Second))
	p.monitor.Receive("sender", picsFrame(0x83), now)
	p.mu.Unlock()
	p.tickSpeech(ctx)
	waitPICSDecision(t, p, "played-green")
	if player.count() != 1 {
		t.Fatalf("plays %d", player.count())
	}
}

// The fixed messages are fetched once the API is set, even with PICS off.
func TestPICSPreparesMessagesWhilePICSIsOff(t *testing.T) {
	dir := t.TempDir()
	synth := &picsSynth{}
	voice := platespeech.NewManager(filepath.Join(dir, "speech"), synth, &picsPlayer{})
	p := newPICSService(dir, voice)
	p.settings = picsSettings{Enabled: false, Speech: false, Style: 1, Volume: 60}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.prepareSpeech(ctx)
	key := "test-key"
	if err := voice.SetAPI(platespeech.DefaultAPIBase, &key); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.restartPreparation() // what PUT /voicevox does
	p.mu.Unlock()
	deadline := time.Now().Add(30 * time.Second)
	for !voice.Prepared(1, pics.GreenText) {
		if time.Now().After(deadline) {
			t.Fatal("green message not prepared while PICS is off")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPICSCacheReportsAndRegenerates(t *testing.T) {
	p, _, _ := preparedPICS(t)
	c := p.speechCache()
	if c.Total != len(pics.Vocabulary()) || c.Ready != 1 || c.Bytes == 0 || len(c.Missing) != 20 || c.Missing[0] != pics.PreviewText {
		t.Fatalf("cache %+v", c)
	}
	if c.State != "waiting" {
		t.Fatalf("state %q", c.State)
	}
	if err := p.regenerateSpeech(); err != nil {
		t.Fatal(err)
	}
	if c := p.speechCache(); c.Ready != 0 || c.Bytes != 0 {
		t.Fatalf("not deleted %+v", c)
	}
	select {
	case <-p.speechChanged:
	default:
		t.Fatal("preparation not woken after delete")
	}
}
