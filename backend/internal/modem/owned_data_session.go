package modem

import (
	"context"
	"errors"
	"time"
)

// The session probe deliberately never configures a Linux interface or route.
// A valid modem response is session proof only, not usable Internet/AP egress.
type OwnedDataSessionResult struct {
	ObservedAt        time.Time `json:"observedAt"`
	Source            string    `json:"source"`
	Status            string    `json:"status"`
	StartAttempted    bool      `json:"startAttempted"`
	StartAcknowledged bool      `json:"startAcknowledged"`
	SettingsVerified  bool      `json:"settingsVerified"`
	DNSCount          int       `json:"dnsCount"`
	StopAcknowledged  bool      `json:"stopAcknowledged"`
	CleanupVerified   bool      `json:"cleanupVerified"`
	BaselinePreserved bool      `json:"baselinePreserved"`
	ProfilesPreserved bool      `json:"profilesPreserved"`
	LinuxConfigured   bool      `json:"linuxConfigured"`
	InternetVerified  bool      `json:"internetVerified"`
	settings          ownedCurrentSettings
}

type ownedSessionTransport struct {
	// written is true even if the response was lost or a later guard failed.
	exchange func(context.Context, ownedDataOperation, ownedPacketHandle) ([]byte, bool, error)
	guard    func(context.Context) error
	// Cleanup may continue when a stock CM appears: it verifies our original
	// open FD and binding, but never stops a foreign CID or packet handle.
	cleanupGuard func(context.Context) error
	pause        func(context.Context, time.Duration) error
	// A root-only durable phase, never a public data-call control interface.
	publish func(string, OwnedDataSessionResult, ownedPacketHandle) error
}

func newOwnedDataSessionResult() OwnedDataSessionResult {
	return OwnedDataSessionResult{ObservedAt: time.Now().UTC(), Source: "gobinet-owned-wds-session-probe", Status: "unknown", CleanupVerified: true}
}

func ownedPause(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func runOwnedDataSession(ctx context.Context, t ownedSessionTransport, hold time.Duration) (out OwnedDataSessionResult, resultErr error) {
	out = newOwnedDataSessionResult()
	if hold < 0 || hold > 10*time.Second || t.exchange == nil || t.guard == nil || t.cleanupGuard == nil {
		return out, errors.New("OWNED_DATA_SESSION_CONTRACT_INVALID")
	}
	if t.pause == nil {
		t.pause = ownedPause
	}
	if t.publish == nil {
		t.publish = func(string, OwnedDataSessionResult, ownedPacketHandle) error { return nil }
	}
	var handle ownedPacketHandle
	// Cleanup uses its own bounded context, never the cancelled operation
	// context. No wildcard handle, autoconnect policy or foreign CM is touched.
	defer func() {
		if handle.value != 0 {
			out.CleanupVerified = false
			cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if e := t.cleanupGuard(cleanup); e != nil {
				if resultErr == nil {
					resultErr = errors.New("OWNED_DATA_CLEANUP_IDENTITY_CHANGED")
				}
				out.Status = "cleanup-unverified"
				out.ObservedAt = time.Now().UTC()
				return
			}
			frame, _, stopErr := t.exchange(cleanup, ownedStop, handle)
			if _, e := parseOwnedDataResponse(ownedStop, frame); stopErr == nil && e == nil {
				out.StopAcknowledged = true
			}
			// Stop may be asynchronous. Only this same CID's explicit
			// disconnected status proves that the owned session is gone.
			for attempt := 0; attempt < 8; attempt++ {
				if e := t.cleanupGuard(cleanup); e != nil {
					break
				}
				frame, _, e := t.exchange(cleanup, ownedStatusAfter, handle)
				status, parseErr := parseOwnedConnectionStatus(ownedStatusAfter, frame)
				if e == nil && parseErr == nil && status == 1 {
					out.CleanupVerified = true
					break
				}
				if t.pause(cleanup, 100*time.Millisecond) != nil {
					break
				}
			}
			if !out.CleanupVerified {
				resultErr = errors.New("OWNED_DATA_CLEANUP_UNVERIFIED")
			} else if !out.StopAcknowledged && resultErr == nil {
				resultErr = errors.New("OWNED_DATA_STOP_NOT_ACKNOWLEDGED")
			}
		}
		verify, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out.BaselinePreserved = t.guard(verify) == nil
		if !out.BaselinePreserved && resultErr == nil {
			resultErr = errors.New("OWNED_DATA_BASELINE_CHANGED")
		}
		out.ObservedAt = time.Now().UTC()
		if resultErr == nil && out.StartAcknowledged && out.SettingsVerified && out.CleanupVerified && out.BaselinePreserved {
			out.Status = "session-tested"
		} else if !out.CleanupVerified {
			out.Status = "cleanup-unverified"
		} else {
			out.Status = "failed"
		}
		if e := t.publish("stopped", out, handle); e != nil && resultErr == nil {
			resultErr = errors.New("OWNED_DATA_PHASE_SAVE_FAILED")
			out.Status = "failed"
		}
	}()
	query := func(op ownedDataOperation) ([]byte, error) {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		if e := t.guard(ctx); e != nil {
			return nil, e
		}
		frame, _, e := t.exchange(ctx, op, handle)
		if e != nil {
			return frame, e
		}
		if _, e := parseOwnedDataResponse(op, frame); e != nil {
			return nil, e
		}
		if e := t.guard(ctx); e != nil {
			return nil, e
		}
		return frame, nil
	}
	for _, op := range []ownedDataOperation{ownedBindMux, ownedSetIPv4} {
		if _, e := query(op); e != nil {
			return out, e
		}
	}
	frame, e := query(ownedStatusBefore)
	if e != nil {
		return out, e
	}
	status, e := parseOwnedConnectionStatus(ownedStatusBefore, frame)
	if e != nil || status != 1 {
		return out, errors.New("OWNED_DATA_BINDING_ALREADY_ACTIVE")
	}
	if e := ctx.Err(); e != nil {
		return out, e
	}
	if e := t.guard(ctx); e != nil {
		return out, e
	}
	// Once Start is written, cancellation must still allow the correlated
	// response to supply our handle, followed by Stop. This does not extend
	// the hold or start a new request after cancellation.
	startCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	pending := out
	pending.StartAttempted, pending.CleanupVerified = true, false
	if e := t.publish("start-pending", pending, ownedPacketHandle{}); e != nil {
		cancel()
		return out, errors.New("OWNED_DATA_PHASE_SAVE_FAILED")
	}
	frame, written, startErr := t.exchange(startCtx, ownedStart, ownedPacketHandle{})
	cancel()
	out.StartAttempted = written
	if written {
		out.CleanupVerified = false
	}
	h, parseErr := parseOwnedPacketHandle(frame)
	if parseErr == nil {
		handle = h
		out.StartAcknowledged = true
		if e := t.publish("started", out, handle); e != nil {
			return out, errors.New("OWNED_DATA_PHASE_SAVE_FAILED")
		}
	} else {
		var rejected *qmiProtocolError
		if errors.As(parseErr, &rejected) {
			out.CleanupVerified = true
		}
		if !out.CleanupVerified {
			return out, errors.New("OWNED_DATA_START_OUTCOME_UNKNOWN")
		}
		if startErr != nil {
			return out, startErr
		}
		return out, parseErr
	}
	if startErr != nil {
		return out, startErr
	}
	if e := ctx.Err(); e != nil {
		return out, e
	}
	frame, e = query(ownedCurrent)
	if e != nil {
		return out, e
	}
	out.settings, e = parseOwnedCurrentSettings(frame)
	if e != nil {
		return out, e
	}
	out.SettingsVerified = true
	out.DNSCount = len(out.settings.dns)
	if e := t.publish("settings", out, handle); e != nil {
		return out, errors.New("OWNED_DATA_PHASE_SAVE_FAILED")
	}
	for remaining := hold; remaining > 0; {
		delay := min(remaining, 200*time.Millisecond)
		if e := t.pause(ctx, delay); e != nil {
			return out, e
		}
		remaining -= delay
		if e := t.guard(ctx); e != nil {
			return out, e
		}
	}
	return out, nil
}
