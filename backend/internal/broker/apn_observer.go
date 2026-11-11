package broker

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"time"

	"github.com/np1-control-center/ncc/internal/modem"
)

type apnObserver struct {
	active  func(context.Context) (bool, error)
	service func(context.Context, string) error
	mark    func(time.Time) error
	remove  func() error
	fresh   func(context.Context, time.Time) error
	now     func() time.Time
}

func apnService(parent context.Context, action string) error {
	if action != "start" && action != "stop" {
		return errors.New("APN_OBSERVER_ACTION_INVALID")
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/systemctl", action, "np1-control-modem.service")
	cmd.WaitDelay = time.Second
	if cmd.Run() != nil {
		return errors.New("APN_OBSERVER_SERVICE_FAILED")
	}
	return nil
}

func realAPNObserver() apnObserver {
	return apnObserver{
		active: func(parent context.Context) (bool, error) {
			ctx, cancel := context.WithTimeout(parent, 3*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "/bin/systemctl", "is-active", "--quiet", "np1-control-modem.service")
			cmd.WaitDelay = time.Second
			e := cmd.Run()
			if e == nil {
				return true, nil
			}
			var status *exec.ExitError
			if ctx.Err() == nil && errors.As(e, &status) && status.ExitCode() == 3 {
				return false, nil
			}
			return false, errors.New("APN_OBSERVER_STATE_UNVERIFIED")
		},
		service: apnService,
		mark:    saveAPNPauseMarker,
		remove:  func() error { return os.Remove(observerPausedPath) },
		fresh:   waitAPNObserverFresh,
		now:     time.Now,
	}
}

func measuredAPNObserverSince(state modem.WorkerState, since, now time.Time) bool {
	return state.Status == "available" && state.Signal != nil && state.Signal.Queries["signal"] == "available" &&
		(state.Signal.RSRP != nil || state.Signal.RSSI != nil) && !state.Signal.ObservedAt.Before(since) &&
		!state.Signal.ObservedAt.After(now.Add(time.Second)) && now.Sub(state.Signal.ObservedAt) <= 10*time.Second
}

func waitAPNObserverFresh(parent context.Context, since time.Time) error {
	ctx, cancel := context.WithTimeout(parent, 25*time.Second)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		now := time.Now().UTC()
		state, e := modem.ReadWorkerState(modem.WorkerStatePath, now)
		if e == nil && measuredAPNObserverSince(state, since, now) {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("APN_OBSERVER_SIGNAL_UNVERIFIED")
		case <-ticker.C:
		}
	}
}

func resumeAPNObserver(parent context.Context, observer apnObserver) error {
	// Freshness is anchored before start. An available pre-stop file cannot
	// satisfy this check even if systemctl start succeeds without a new sample.
	since := observer.now().UTC()
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	if observer.service(ctx, "start") != nil {
		return errors.New("APN_OBSERVER_RESUME_FAILED")
	}
	if observer.fresh(ctx, since) != nil {
		return errors.New("APN_OBSERVER_SIGNAL_UNVERIFIED")
	}
	if observer.remove() != nil {
		return errors.New("APN_OBSERVER_MARKER_FAILED")
	}
	return nil
}

func withAPNObserverPaused(ctx context.Context, observer apnObserver, operation func(context.Context) error) (err error) {
	active, e := observer.active(ctx)
	if e != nil {
		return e
	}
	if !active {
		return operation(ctx)
	}
	if observer.mark(observer.now().UTC()) != nil {
		return errors.New("APN_OBSERVER_MARKER_FAILED")
	}
	defer func() {
		// Parent cancellation must still allow cleanup. A hard parent watchdog
		// uses the persisted marker to retry this only after the child is reaped.
		if e := resumeAPNObserver(context.Background(), observer); e != nil {
			err = e
		}
	}()
	if observer.service(ctx, "stop") != nil {
		return errors.New("APN_OBSERVER_PAUSE_FAILED")
	}
	return operation(ctx)
}

func resumeMarkedAPNObserver(ctx context.Context, observer apnObserver) error {
	var marker apnPauseMarker
	if e := readAPNRuntimeJSON(observerPausedPath, &marker, 1024); errors.Is(e, os.ErrNotExist) {
		return nil
	} else if e != nil {
		return errors.New("APN_OBSERVER_MARKER_INVALID")
	}
	if marker.Version != 1 || !marker.Resume || marker.StartedAt.IsZero() || marker.StartedAt.After(observer.now().Add(time.Minute)) {
		return errors.New("APN_OBSERVER_MARKER_INVALID")
	}
	return resumeAPNObserver(ctx, observer)
}
