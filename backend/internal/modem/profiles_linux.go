//go:build linux

package modem

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// ReadGobiNetProfiles queries only the two stock 3GPP saved profiles using one
// new WDS(1) CID. It does not bind/start/stop/change the active data connection.
// Run in a root helper with an external timeout for kernel open/ioctl/close.
func ReadGobiNetProfiles(ctx context.Context) (ProfileRecovery, error) {
	return readByStockCMOwnership(ctx, stockCMProcessPresent, func(ctx context.Context) (ProfileRecovery, error) {
		capture, _, err := ReadStockCMProfiles(ctx)
		return capture, err
	}, readExclusiveGobiNetProfiles)
}
func readExclusiveGobiNetProfiles(ctx context.Context) (ProfileRecovery, error) {
	var out ProfileRecovery
	e := withIndependentWDS(ctx, func(ctx context.Context, fd int) error {
		var e error
		out, e = collectProfiles(ctx, func(ctx context.Context, transaction, message uint16) ([]byte, error) {
			return exchangeQMI(ctx, fd, transaction, message)
		})
		return e
	})
	return out, e
}
func ReadGobiNetProfileInventory(ctx context.Context) (ProfileInventory, error) {
	return readByStockCMOwnership(ctx, stockCMProcessPresent, ReadStockCMInventory, readExclusiveGobiNetProfileInventory)
}
func readExclusiveGobiNetProfileInventory(ctx context.Context) (ProfileInventory, error) {
	var out ProfileInventory
	e := withIndependentWDS(ctx, func(ctx context.Context, fd int) error {
		frame, e := exchangeQMI(ctx, fd, 7, 0x002a)
		if e != nil {
			return e
		}
		out, e = parseProfileInventory(frame)
		return e
	})
	return out, e
}
func withIndependentWDS(ctx context.Context, action func(context.Context, int) error) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if !gobiQueryMu.TryLock() {
		return errors.New("modem observation already in progress")
	}
	defer gobiQueryMu.Unlock()
	if e := ctx.Err(); e != nil {
		return e
	}
	if e := verifyGobiNet(); e != nil {
		return e
	}
	if e := inspectQMIOwnership(); e != nil {
		return e
	}
	fd, e := unix.Open(qmiDevice, unix.O_RDWR|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOCTTY, 0)
	if e != nil {
		return e
	}
	defer unix.Close(fd)
	if e = inspectQMIOwnership(); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	if e = unix.IoctlSetInt(fd, 0x8be1, 1); e != nil {
		return fmt.Errorf("allocate independent WDS CID: %w", e)
	}
	return action(ctx, fd)
}
func requireProfileRecoveryRoot() error {
	if os.Geteuid() != 0 {
		return errors.New("profile recovery requires root")
	}
	return nil
}
func verifyProfileRecoveryOwner(info os.FileInfo, mode os.FileMode) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || info.Mode().Perm() != mode || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return errors.New("profile recovery owner or permissions invalid")
	}
	return nil
}
func syncProfileRecoveryDirectory(root *os.Root) error {
	dir, e := root.Open(".")
	if e != nil {
		return e
	}
	defer dir.Close()
	return dir.Sync()
}
