package platespeech

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func (m *Manager) preparedPath(style int, text string) string {
	return filepath.Join(m.dir, "prepared", strconv.Itoa(style), hashName(text))
}

func (m *Manager) Prepared(style int, text string) bool {
	info, err := os.Stat(m.preparedPath(style, text))
	return err == nil && info.Size() > 0
}

func (m *Manager) HardwareAvailable() error { return m.player.Available() }

func (m *Manager) CachedStyles() []Style {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Style{}, m.styles...)
}

// Prepare fills the local cache in priority order. Callers supply only fixed,
// non-observational vocabulary and run a single preparation worker.
func (m *Manager) Prepare(ctx context.Context, style int, texts []string) error {
	endpoint, key := m.APIConfigValue()
	for _, text := range texts {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if m.Prepared(style, text) {
			continue
		}
		if key == "" {
			return errors.New("VOICEVOX APIキーが未設定です")
		}
		request, cancel := context.WithTimeout(ctx, 12*time.Second)
		pcm, err := m.synth.Synthesize(request, API{Endpoint: endpoint, Key: key}, text, style)
		cancel()
		if err != nil {
			return errors.New(describe(err))
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := writeAtomic(m.preparedPath(style, text), pcm, 0o600); err != nil {
			return errors.New("音声を保存できません")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.pace):
		}
	}
	return nil
}

// PlayPrepared uses the same player as plate announcements and waits for the
// speaker when it is busy. allow runs with the playback slot held, after
// file I/O, and decides whether the announcement is still valid.
func (m *Manager) PlayPrepared(ctx context.Context, style, volume int, texts []string, allow func(time.Duration) bool) error {
	var pcm []byte
	for _, text := range texts {
		clip, err := os.ReadFile(m.preparedPath(style, text))
		if err != nil || len(clip) == 0 || len(clip) > MaxClipBytes {
			return errors.New("VOICEVOXの音声を準備中です")
		}
		pcm = append(pcm, clip...)
	}
	if !m.acquirePlayback(ctx, m.slotWait) {
		return errors.New("本体スピーカーは再生中です")
	}
	defer m.playback.Unlock()
	if ctx.Err() != nil || len(pcm) == 0 || !allow(time.Duration(len(pcm))*time.Second/bytesPerSec) {
		return errors.New("読み上げの有効期限が切れました")
	}
	return m.playPCM(ctx, pcm, volume)
}

func (m *Manager) acquirePlayback(ctx context.Context, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for !m.playback.TryLock() {
		if ctx.Err() != nil || time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(20 * time.Millisecond):
		}
	}
	return true
}
