package modem

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type ownedSessionFixture struct {
	ops                 []ownedDataOperation
	guardErr            error
	cleanupErr          error
	currentBad          bool
	activeBefore        bool
	startUnknown        bool
	stopUnknown         bool
	cancelOnStart       context.CancelFunc
	guardFailAfterStart bool
}

func (f *ownedSessionFixture) transport() ownedSessionTransport {
	return ownedSessionTransport{
		guard:        func(context.Context) error { return f.guardErr },
		cleanupGuard: func(context.Context) error { return f.cleanupErr },
		pause:        func(ctx context.Context, d time.Duration) error { return ctx.Err() },
		exchange: func(ctx context.Context, op ownedDataOperation, h ownedPacketHandle) ([]byte, bool, error) {
			if ctx.Err() != nil {
				return nil, false, ctx.Err()
			}
			f.ops = append(f.ops, op)
			switch op {
			case ownedStart:
				if f.cancelOnStart != nil {
					f.cancelOnStart()
				}
				if f.guardFailAfterStart {
					f.guardErr = errors.New("stock CM appeared")
				}
				if f.startUnknown {
					return nil, true, context.DeadlineExceeded
				}
				return ownedResponseFixture(op, map[byte][]byte{1: {0x78, 0x56, 0x34, 0x12}}), true, nil
			case ownedCurrent:
				if f.currentBad {
					return nil, true, errors.New("invalid current settings")
				}
				return ownedCurrentFixture(), true, nil
			case ownedStatusBefore:
				v := byte(1)
				if f.activeBefore {
					v = 2
				}
				return ownedResponseFixture(op, map[byte][]byte{1: {v}}), true, nil
			case ownedStop:
				if h.value != 0x12345678 {
					panic("Stop did not use the sole correlated own handle")
				}
				if f.stopUnknown {
					return nil, true, context.DeadlineExceeded
				}
			case ownedStatusAfter:
				v := byte(1)
				if f.stopUnknown {
					v = 2
				}
				return ownedResponseFixture(op, map[byte][]byte{1: {v}}), true, nil
			}
			return ownedResponseFixture(op, nil), true, nil
		},
	}
}
func ownedCount(ops []ownedDataOperation, want ownedDataOperation) int {
	n := 0
	for _, op := range ops {
		if op == want {
			n++
		}
	}
	return n
}

func TestOwnedSessionStartsAndStopsOnlyItsHandle(t *testing.T) {
	f := &ownedSessionFixture{}
	r, e := runOwnedDataSession(context.Background(), f.transport(), time.Second)
	if e != nil || r.Status != "session-tested" || !r.SettingsVerified || !r.CleanupVerified || !r.StopAcknowledged || !r.BaselinePreserved {
		t.Fatalf("successful lifecycle rejected: %+v %v", r, e)
	}
	if r.LinuxConfigured || r.InternetVerified || ownedCount(f.ops, ownedStart) != 1 || ownedCount(f.ops, ownedStop) != 1 {
		t.Fatal("unexpected egress assertion or duplicate lifecycle")
	}
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "192.") || strings.Contains(string(b), "305419896") {
		t.Fatal("private settings/handle escaped")
	}
}

func TestOwnedSessionCancellationWaitsForHandleThenCleansUp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &ownedSessionFixture{cancelOnStart: cancel}
	r, e := runOwnedDataSession(ctx, f.transport(), time.Second)
	if !errors.Is(e, context.Canceled) || !r.StartAcknowledged || !r.CleanupVerified || ownedCount(f.ops, ownedStop) != 1 || ownedCount(f.ops, ownedCurrent) != 0 {
		t.Fatalf("cancelled Start did not drain/clean own handle: %+v %v", r, e)
	}
}

func TestOwnedSessionLostStartNeverStopsWildcardOrRestarts(t *testing.T) {
	f := &ownedSessionFixture{startUnknown: true}
	r, e := runOwnedDataSession(context.Background(), f.transport(), 0)
	if e == nil || r.Status != "cleanup-unverified" || r.CleanupVerified || ownedCount(f.ops, ownedStart) != 1 || ownedCount(f.ops, ownedStop) != 0 {
		t.Fatalf("unknown Start falsely recovered: %+v %v", r, e)
	}
}

func TestOwnedSessionExistingCallIsNeverStopped(t *testing.T) {
	f := &ownedSessionFixture{activeBefore: true}
	r, e := runOwnedDataSession(context.Background(), f.transport(), 0)
	if e == nil || r.StartAttempted || !r.CleanupVerified || ownedCount(f.ops, ownedStart) != 0 || ownedCount(f.ops, ownedStop) != 0 {
		t.Fatal("preexisting binding was controlled")
	}
}

func TestOwnedSessionGuardChangeAndBadSettingsStillStopOwnCall(t *testing.T) {
	for _, f := range []*ownedSessionFixture{{currentBad: true}, {guardFailAfterStart: true}} {
		r, e := runOwnedDataSession(context.Background(), f.transport(), time.Second)
		if e == nil || !r.CleanupVerified || ownedCount(f.ops, ownedStop) != 1 || ownedCount(f.ops, ownedStart) != 1 {
			t.Fatalf("failure leaked own call: %+v %v", r, e)
		}
	}
}

func TestOwnedSessionFailedCleanupNeverClaimsSuccess(t *testing.T) {
	for _, f := range []*ownedSessionFixture{{stopUnknown: true}, {cleanupErr: errors.New("own binding gone")}} {
		r, e := runOwnedDataSession(context.Background(), f.transport(), 0)
		if e == nil || r.CleanupVerified || r.Status != "cleanup-unverified" {
			t.Fatalf("cleanup uncertainty hidden: %+v %v", r, e)
		}
	}
}

func TestOwnedSessionJournalFailurePreventsStartOrCleansKnownHandle(t *testing.T) {
	for _, failPhase := range []string{"start-pending", "started", "settings"} {
		f := &ownedSessionFixture{}
		transport := f.transport()
		transport.publish = func(phase string, _ OwnedDataSessionResult, _ ownedPacketHandle) error {
			if phase == failPhase {
				return errors.New("disk unavailable")
			}
			return nil
		}
		r, e := runOwnedDataSession(context.Background(), transport, 0)
		if e == nil || !r.CleanupVerified {
			t.Fatalf("journal error failed cleanup: %s %+v %v", failPhase, r, e)
		}
		if failPhase == "start-pending" {
			if ownedCount(f.ops, ownedStart) != 0 {
				t.Fatal("Start transmitted before durable journal")
			}
		} else if ownedCount(f.ops, ownedStop) != 1 {
			t.Fatal("known handle leaked after journal failure")
		}
	}
}
