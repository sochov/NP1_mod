package audio

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeOutput struct {
	mu     sync.Mutex
	frames [][]byte
	closed chan struct{}
	ctx    context.Context
	block  bool
}

func (p *fakeOutput) Write(b []byte) (int, error) {
	if p.block {
		<-p.ctx.Done()
		return 0, p.ctx.Err()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.frames = append(p.frames, append([]byte(nil), b...))
	return len(b), nil
}
func (p *fakeOutput) Close() error { close(p.closed); return nil }
func testManager(t *testing.T, block bool) (*Manager, **fakeOutput) {
	t.Helper()
	var output *fakeOutput
	m := New(func(ctx context.Context) (Process, error) {
		output = &fakeOutput{closed: make(chan struct{}), ctx: ctx, block: block}
		return output, nil
	})
	t.Cleanup(m.Close)
	return m, &output
}
func requireProblem(t *testing.T, e error, status int) {
	t.Helper()
	var p *Problem
	if !errors.As(e, &p) || p.Status != status {
		t.Fatalf("wanted %d, got %v", status, e)
	}
}
func TestStreamSessionValidationAndRestoration(t *testing.T) {
	m, out := testManager(t, false)
	s, e := m.Start()
	if e != nil {
		t.Fatal(e)
	}
	if len(s.ID) != 48 || s.SampleRate != 24000 {
		t.Fatal(s)
	}
	_, e = m.Start()
	requireProblem(t, e, 409)
	requireProblem(t, m.Frame(s.ID, 0, []byte{0, 0}), 400)
	requireProblem(t, m.Frame(s.ID, 1, []byte{0}), 400)
	requireProblem(t, m.Frame(s.ID, 1, make([]byte, 9602)), 400)
	requireProblem(t, m.Frame("other", 1, []byte{0, 0}), 410)
	if e = m.Frame(s.ID, 1, []byte{0, 0, 1, 0}); e != nil {
		t.Fatal(e)
	}
	requireProblem(t, m.Frame(s.ID, 1, []byte{0, 0}), 409)
	requireProblem(t, m.Frame(s.ID, 2, []byte{0, 0}), 429)
	if e = m.Stop(s.ID); e != nil {
		t.Fatal(e)
	}
	select {
	case <-(*out).closed:
	case <-time.After(time.Second):
		t.Fatal("output not closed")
	}
	requireProblem(t, m.Frame(s.ID, 2, []byte{0, 0}), 410)
	if _, e = m.Start(); e != nil {
		t.Fatal(e)
	}
}
func TestIdleExpiryCancelsBlockedOutput(t *testing.T) {
	m, out := testManager(t, true)
	now := time.Now()
	m.now = func() time.Time { return now }
	s, e := m.Start()
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Frame(s.ID, 1, make([]byte, 9600)); e != nil {
		t.Fatal(e)
	}
	m.mu.Lock()
	now = now.Add(3 * time.Second)
	m.mu.Unlock()
	select {
	case <-(*out).closed:
	case <-time.After(time.Second):
		t.Fatal("blocked writer survived idle timeout")
	}
	requireProblem(t, m.Frame(s.ID, 2, []byte{0, 0}), 410)
}
func TestFailedProcessDoesNotClaimSession(t *testing.T) {
	m := New(func(context.Context) (Process, error) { return nil, errors.New("unavailable") })
	_, e := m.Start()
	requireProblem(t, e, 503)
	if m.current != nil {
		t.Fatal("failed output reserved session")
	}
}
