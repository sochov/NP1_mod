//go:build linux

package modem

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"time"
)

// ReadGobiNetAPN is an explicit, standalone WDS read probe. It neither joins an
// existing client's FD nor starts/stops/binds/configures any data call. A new
// unbound WDS CID may legitimately return out-of-call; callers retain unknown.
func ReadGobiNetAPN(ctx context.Context) (APNObservation, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if !gobiQueryMu.TryLock() {
		return APNObservation{}, errors.New("modem observation already in progress")
	}
	defer gobiQueryMu.Unlock()
	if e := ctx.Err(); e != nil {
		return APNObservation{}, e
	}
	if e := verifyGobiNet(); e != nil {
		return APNObservation{}, e
	}
	if e := inspectQMIOwnership(); e != nil {
		return APNObservation{}, e
	}
	fd, e := unix.Open(qmiDevice, unix.O_RDWR|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOCTTY, 0)
	if e != nil {
		return APNObservation{}, e
	}
	defer unix.Close(fd)
	if e = inspectQMIOwnership(); e != nil {
		return APNObservation{}, e
	}
	if e = ctx.Err(); e != nil {
		return APNObservation{}, e
	}
	if e = unix.IoctlSetInt(fd, 0x8be1, 1); e != nil {
		return APNObservation{}, fmt.Errorf("allocate independent WDS CID: %w", e)
	}
	if e = ctx.Err(); e != nil {
		return APNObservation{}, e
	}
	frame, e := exchangeQMI(ctx, fd, 4, 0x002d)
	if e != nil {
		return APNObservation{}, e
	}
	return ParseAPNCurrentSettings(frame)
}
