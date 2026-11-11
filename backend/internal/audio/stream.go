// Package audio plays short, bounded browser PCM streams through PulseAudio.
// It never opens the microphone, changes the system volume, or stores audio.
package audio

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os/exec"
	"sync"
	"time"
)

const SampleRate = 24000
const MaxChunkBytes = 9600
const IdleTimeout = 2 * time.Second

type Process interface {
	Write([]byte) (int, error)
	// Close must release a blocked Write. The manager calls it once, including on idle expiry.
	Close() error
}
type StartProcess func(context.Context) (Process, error)
type Session struct {
	ID            string `json:"id"`
	SampleRate    int    `json:"sampleRate"`
	Channels      int    `json:"channels"`
	ChunkSamples  int    `json:"chunkSamples"`
	IdleTimeoutMs int    `json:"idleTimeoutMs"`
}
type Problem struct {
	Status int
	Code   string
}

func (p *Problem) Error() string            { return p.Code }
func failure(status int, code string) error { return &Problem{status, code} }

type live struct {
	id                string
	sequence          uint64
	updated, accepted time.Time
	queue             chan []byte
	cancel            context.CancelFunc
	done              chan struct{}
	stopping          bool
}
type Manager struct {
	mu      sync.Mutex
	current *live
	start   StartProcess
	now     func() time.Time
}

func New(start StartProcess) *Manager {
	if start == nil {
		start = startPulse
	}
	return &Manager{start: start, now: time.Now}
}
func (m *Manager) Start() (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current != nil && !m.current.stopping && !m.now().Before(m.current.updated) && m.now().Sub(m.current.updated) < IdleTimeout {
		return Session{}, failure(409, "AUDIO_IN_USE")
	}
	if m.current != nil {
		previous := m.current
		previous.stopping = true
		previous.cancel()
		m.mu.Unlock()
		e := waitStopped(previous)
		m.mu.Lock()
		if e != nil {
			return Session{}, e
		}
		// Another Start may win while the previous process is being joined.
		if m.current != nil {
			return Session{}, failure(409, "AUDIO_IN_USE")
		}
	}
	var token [24]byte
	if _, e := rand.Read(token[:]); e != nil {
		return Session{}, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	p, e := m.start(ctx)
	if e != nil {
		cancel()
		return Session{}, failure(503, "AUDIO_OUTPUT_UNAVAILABLE")
	}
	s := &live{id: hex.EncodeToString(token[:]), updated: m.now(), queue: make(chan []byte, 3), cancel: cancel, done: make(chan struct{})}
	m.current = s
	go m.run(ctx, s, p)
	return Session{s.id, SampleRate, 1, MaxChunkBytes / 2, int(IdleTimeout / time.Millisecond)}, nil
}
func (m *Manager) Frame(id string, sequence uint64, pcm []byte) error {
	if sequence == 0 || sequence > 1<<53-1 || len(pcm) == 0 || len(pcm) > MaxChunkBytes || len(pcm)%2 != 0 {
		return failure(400, "INVALID_AUDIO_FRAME")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.current
	if s == nil || s.stopping || id != s.id || m.now().Before(s.updated) || m.now().Sub(s.updated) >= IdleTimeout {
		return failure(410, "AUDIO_SESSION_ENDED")
	}
	if sequence <= s.sequence {
		return failure(409, "AUDIO_SEQUENCE_CONFLICT")
	}
	if !s.accepted.IsZero() && m.now().Sub(s.accepted) < 40*time.Millisecond {
		return failure(429, "AUDIO_FRAME_RATE")
	}
	frame := append([]byte(nil), pcm...)
	select {
	case s.queue <- frame:
	default:
		// A slow output must not accumulate old sound or block an HTTP request.
		select {
		case <-s.queue:
		default:
		}
		select {
		case s.queue <- frame:
		default:
			return failure(503, "AUDIO_OUTPUT_BUSY")
		}
	}
	s.sequence = sequence
	s.updated = m.now()
	s.accepted = s.updated
	return nil
}
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	if m.current == nil || m.current.id != id {
		m.mu.Unlock()
		return failure(410, "AUDIO_SESSION_ENDED")
	}
	s := m.current
	s.stopping = true
	s.cancel()
	m.mu.Unlock()
	return waitStopped(s)
}
func (m *Manager) Close() {
	m.mu.Lock()
	s := m.current
	if s != nil {
		s.stopping = true
		s.cancel()
	}
	m.mu.Unlock()
	if s != nil {
		_ = waitStopped(s)
	}
}
func waitStopped(s *live) error {
	select {
	case <-s.done:
		return nil
	case <-time.After(time.Second):
		return failure(503, "AUDIO_STOP_NOT_CONFIRMED")
	}
}
func (m *Manager) run(ctx context.Context, s *live, p Process) {
	var closeOnce sync.Once
	closeOutput := func() { closeOnce.Do(func() { _ = p.Close() }) }
	watchdogDone := make(chan struct{})
	defer func() {
		s.cancel()
		closeOutput()
		<-watchdogDone
		m.mu.Lock()
		if m.current == s {
			m.current = nil
		}
		m.mu.Unlock()
		close(s.done)
	}()
	// The watchdog is independent of Write: a disconnected audio server may
	// block its pipe, but must still be terminated when browser frames stop.
	go func() {
		defer close(watchdogDone)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				closeOutput()
				return
			case <-ticker.C:
				m.mu.Lock()
				expired := m.now().Before(s.updated) || m.now().Sub(s.updated) >= IdleTimeout
				m.mu.Unlock()
				if expired {
					s.cancel()
					closeOutput()
					return
				}
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-s.queue:
			if ctx.Err() != nil {
				return
			}
			if n, e := p.Write(frame); e != nil || n != len(frame) {
				return
			}
		}
	}
}

type pulseProcess struct {
	pipe    io.WriteCloser
	command *exec.Cmd
	cancel  context.CancelFunc
	once    sync.Once
}

func (p *pulseProcess) Write(b []byte) (int, error) { return p.pipe.Write(b) }
func (p *pulseProcess) Close() (e error) {
	p.once.Do(func() { p.cancel(); _ = p.pipe.Close(); e = p.command.Wait() })
	return
}
func startPulse(parent context.Context) (Process, error) {
	if _, e := exec.LookPath("/usr/bin/pacat"); e != nil {
		return nil, errors.New("PulseAudio player unavailable")
	}
	ctx, cancel := context.WithCancel(parent)
	// Fixed output and stream volume; never change the system's sink volume.
	cmd := pulseCommand(ctx)
	pipe, e := cmd.StdinPipe()
	if e != nil {
		cancel()
		return nil, e
	}
	if e = cmd.Start(); e != nil {
		cancel()
		_ = pipe.Close()
		return nil, e
	}
	return &pulseProcess{pipe: pipe, command: cmd, cancel: cancel}, nil
}

func pulseCommand(ctx context.Context) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "/usr/bin/pacat", "--playback", "--raw", "--server=unix:/run/pulse/native", "--device=primary", "--client-name=NP1-Web-Audio", "--stream-name=Browser-screen-audio", "--format=s16le", "--rate=24000", "--channels=1", "--latency-msec=100", "--volume=16384")
	cmd.WaitDelay = 500 * time.Millisecond
	return cmd
}
