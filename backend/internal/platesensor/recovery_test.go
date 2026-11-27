package platesensor

import (
	"context"
	"errors"
	"testing"
	"time"
)

type recoveryPrimary struct{ fresh, missing bool }

func (p *recoveryPrimary) Next(buf []byte) ([2]int64, bool, error) {
	if p.missing {
		return [2]int64{}, false, errors.New("front export missing")
	}
	if p.fresh {
		buf[0] = 1
	}
	return [2]int64{}, p.fresh, nil
}

func TestFrontRecoveryAcrossRepeatedOutages(t *testing.T) {
	for _, missing := range []bool{false, true} {
		p := &recoveryPrimary{fresh: true}
		ready, stopped := make(chan struct{}, 1), make(chan struct{}, 1)
		s := recoveringFront{clock: time.Now, primary: p, feeds: []frontFeed{{"front-recording", func(ctx context.Context, publish func([]byte)) error {
			publish([]byte{2})
			ready <- struct{}{}
			<-ctx.Done()
			stopped <- struct{}{}
			return ctx.Err()
		}}}}
		defer s.reset()
		buf := make([]byte, 1)
		for range 2 {
			if _, ok, err := s.Next(buf); err != nil || !ok || buf[0] != 1 {
				t.Fatal("missing primary", err)
			}
			p.fresh, p.missing = false, missing
			if _, ok, _ := s.Next(buf); ok || s.session != nil {
				t.Fatal("early failover")
			}
			s.lastPrimary = time.Now().Add(-cameraFrameWait)
			_, first, _ := s.Next(buf)
			select {
			case <-ready:
			case <-time.After(time.Second):
				t.Fatal("never opened fallback")
			}
			if !first {
				if _, ok, err := s.Next(buf); !ok || err != nil {
					t.Fatal("fallback frame rejected", err)
				}
			}
			if buf[0] != 2 {
				t.Fatal("wrong feed")
			}
			if _, ok, err := s.Next(buf); ok || err != nil {
				t.Fatal("poll between fallback frames must be idle", err)
			}
			p.fresh, p.missing = true, false
			if _, ok, err := s.Next(buf); !ok || err != nil || buf[0] != 1 {
				t.Fatal("no failback", err)
			}
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("decoder leaked")
			}
		}
	}
}

func TestFrontRecoveryRetriesStalledDecoder(t *testing.T) {
	now := time.Now()
	s := recoveringFront{primary: &recoveryPrimary{}, clock: func() time.Time { return now }, feeds: []frontFeed{{"stalled", func(ctx context.Context, p func([]byte)) error { <-ctx.Done(); return ctx.Err() }}}}
	defer s.reset()
	buf := []byte{0}
	s.Next(buf)
	now = now.Add(cameraFrameWait)
	s.Next(buf)
	first := s.session
	if first == nil {
		t.Fatal("decoder missing")
	}
	now = now.Add(12 * time.Second)
	if _, ok, _ := s.Next(buf); ok || s.session != nil {
		t.Fatal("stalled decoder not cancelled")
	}
	if _, ok, _ := s.Next(buf); ok || s.session != nil {
		t.Fatal("no retry backoff")
	}
	now = now.Add(time.Second)
	s.Next(buf)
	if s.session == nil || s.session == first {
		t.Fatal("no automatic restart")
	}
}

func TestFrontRecoveryBothFeedsUnavailableThenReturn(t *testing.T) {
	now := time.Now()
	failedFeed := func(ctx context.Context, publish func([]byte)) error { return errors.New("unavailable") }
	s := recoveringFront{primary: &recoveryPrimary{missing: true}, clock: func() time.Time { return now },
		lastPrimary: now.Add(-cameraFrameWait), feeds: []frontFeed{{"encoder", failedFeed}, {"recording", failedFeed}}}
	defer s.reset()
	buf := []byte{8}
	for i := 0; i < 5; i++ {
		if _, ok, _ := s.Next(buf); ok {
			t.Fatal("accepted unavailable input")
		}
		deadline := time.Now().Add(time.Second)
		for s.session != nil && time.Now().Before(deadline) {
			if _, ok, _ := s.Next(buf); ok {
				t.Fatal("accepted unavailable input")
			}
			time.Sleep(time.Millisecond)
		}
		if s.session != nil || !s.retryAt.After(now) || s.retryAt.Sub(now) > 16*time.Second {
			t.Fatal("lost retry schedule")
		}
		now = s.retryAt
	}
	ready := make(chan struct{})
	s.clock = time.Now
	s.lastPrimary = time.Now().Add(-cameraFrameWait)
	s.retryAt = time.Now()
	s.feeds[s.index].run = func(ctx context.Context, publish func([]byte)) error {
		publish([]byte{9})
		close(ready)
		<-ctx.Done()
		return ctx.Err()
	}
	_, ok, _ := s.Next(buf)
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("failed to reopen")
	}
	if !ok {
		_, ok, _ = s.Next(buf)
	}
	if !ok || buf[0] != 9 {
		t.Fatal("failed to resume with primary still missing")
	}
}

func TestFrontRecoveryDiscardsUndeliveredOldFrame(t *testing.T) {
	now := time.Now()
	r := &recoverySession{latest: []byte{4}, received: now.Add(-cameraFrameWait), pending: true, done: make(chan error)}
	s := recoveringFront{primary: &recoveryPrimary{}, clock: func() time.Time { return now }, lastPrimary: now.Add(-cameraFrameWait), session: r}
	buf := []byte{0}
	if _, ok, _ := s.Next(buf); ok || buf[0] != 0 {
		t.Fatal("old backup frame was processed")
	}
}
