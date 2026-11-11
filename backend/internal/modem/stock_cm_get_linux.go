//go:build linux

package modem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

func readStockCMProbeDescriptors(ctx context.Context, cmPID uint32, ownFD int) ([]int, error) {
	pids, err := os.ReadDir("/proc")
	if err != nil || len(pids) > 4096 {
		return nil, errors.New("STOCK_CM_GET_PROC_UNAVAILABLE")
	}
	rows := []stockCMDescriptor{}
	total := 0
	for _, item := range pids {
		if ctx.Err() != nil {
			return nil, errors.New("STOCK_CM_GET_INSPECTION_TIMEOUT")
		}
		n, parseErr := strconv.ParseUint(item.Name(), 10, 32)
		if parseErr != nil || n == 0 {
			continue
		}
		directory := filepath.Join("/proc", item.Name(), "fd")
		fds, readErr := os.ReadDir(directory)
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil || len(fds) > 4096 {
			return nil, errors.New("STOCK_CM_GET_FD_INVENTORY_UNAVAILABLE")
		}
		total += len(fds)
		if total > 65536 {
			return nil, errors.New("STOCK_CM_GET_FD_INVENTORY_OVERSIZE")
		}
		for _, item := range fds {
			if ctx.Err() != nil {
				return nil, errors.New("STOCK_CM_GET_INSPECTION_TIMEOUT")
			}
			target, linkErr := os.Readlink(filepath.Join(directory, item.Name()))
			if errors.Is(linkErr, os.ErrNotExist) {
				continue
			}
			if linkErr != nil {
				return nil, errors.New("STOCK_CM_GET_FD_LINK_UNAVAILABLE")
			}
			if target != qmiDevice {
				continue
			}
			fd, parseErr := strconv.Atoi(item.Name())
			if parseErr != nil {
				return nil, errors.New("STOCK_CM_GET_FD_NUMBER_INVALID")
			}
			rows = append(rows, stockCMDescriptor{uint32(n), fd})
		}
	}
	return stockCMDescriptorSet(cmPID, uint32(os.Getpid()), ownFD, rows)
}

func captureStockCMProbeLease(ctx context.Context) (stockCMLease, error) {
	var out stockCMLease
	proof, err := InspectStockCMOwnership(ctx)
	if err != nil {
		return out, err
	}
	if !proof.Verified || !proof.OwnerVerified || proof.Status != "available" {
		return out, errors.New("STOCK_CM_GET_PROVENANCE_UNVERIFIED")
	}
	pids, err := os.ReadDir("/proc")
	if err != nil || len(pids) > 4096 {
		return out, errors.New("STOCK_CM_GET_PROC_UNAVAILABLE")
	}
	owners, cms := 0, 0
	for _, item := range pids {
		if ctx.Err() != nil {
			return out, errors.New("STOCK_CM_GET_INSPECTION_TIMEOUT")
		}
		n, parseErr := strconv.ParseUint(item.Name(), 10, 32)
		if parseErr != nil || n == 0 {
			continue
		}
		exe, readErr := os.Readlink(filepath.Join("/proc", item.Name(), "exe"))
		if readErr != nil {
			continue
		}
		if exe != stockCMExecutable && exe != stockCMOwnerExecutable {
			continue
		}
		identity, identityErr := readStockCMProcess(uint32(n), exe)
		if identityErr != nil {
			return out, identityErr
		}
		if exe == stockCMExecutable {
			out.cm = identity
			cms++
		} else {
			out.owner = identity
			owners++
		}
	}
	if owners != 1 || cms != 1 {
		return stockCMLease{}, errors.New("STOCK_CM_GET_PROCESS_NOT_UNIQUE")
	}
	if err = verifiedStockCMProcess(out.cm, out.owner); err != nil {
		return stockCMLease{}, err
	}
	out.descriptors, err = readStockCMProbeDescriptors(ctx, out.cm.pid, -1)
	if err != nil {
		return stockCMLease{}, err
	}
	if err = verifyStockCMProbeLease(ctx, out, -1); err != nil {
		return stockCMLease{}, err
	}
	return out, nil
}

func verifyStockCMProbeLease(ctx context.Context, expected stockCMLease, ownFD int) error {
	owner, err := readStockCMProcess(expected.owner.pid, stockCMOwnerExecutable)
	if err != nil {
		return err
	}
	cm, err := readStockCMProcess(expected.cm.pid, stockCMExecutable)
	if err != nil {
		return err
	}
	if err = verifiedStockCMProcess(cm, owner); err != nil {
		return err
	}
	fds, err := readStockCMProbeDescriptors(ctx, cm.pid, ownFD)
	if err != nil {
		return err
	}
	if !stockCMLeaseMatches(expected, stockCMLease{owner: owner, cm: cm, descriptors: fds}) {
		return errors.New("STOCK_CM_GET_PROVENANCE_CHANGED")
	}
	return nil
}

// Separate, explicit diagnostic entry points. The production getters and their
// foreign-FD rejection are unchanged. There is no caller-selected QMI message,
// CID binding, data-call control, APN write, WDA or CM lifecycle operation.
func withStockCMReadOnlyCID(parent context.Context, service uint32, action func(context.Context, func(context.Context, uint16, uint16) ([]byte, error)) error) error {
	if service != 1 && service != 3 {
		return errors.New("STOCK_CM_GET_SERVICE_NOT_ALLOWED")
	}
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	if !gobiQueryMu.TryLock() {
		return errors.New("STOCK_CM_GET_ALREADY_IN_PROGRESS")
	}
	defer gobiQueryMu.Unlock()
	if verifyGobiNet() != nil {
		return errors.New("STOCK_CM_GET_MODEM_UNVERIFIED")
	}
	lease, err := captureStockCMProbeLease(ctx)
	if err != nil {
		return err
	}
	fd, err := unix.Open(qmiDevice, unix.O_RDWR|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOCTTY, 0)
	if err != nil {
		return errors.New("STOCK_CM_GET_OPEN_FAILED")
	}
	defer func() {
		if fd >= 0 {
			unix.Close(fd)
		}
	}()
	if err = verifyStockCMProbeLease(ctx, lease, fd); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return errors.New("STOCK_CM_GET_TIMEOUT")
	}
	if unix.IoctlSetInt(fd, 0x8be1, int(service)) != nil {
		return errors.New("STOCK_CM_GET_CID_ALLOCATION_FAILED")
	}
	if err = verifyStockCMProbeLease(ctx, lease, fd); err != nil {
		return err
	}
	exchange := stockCMGuardedExchange{service: service, verify: func() error { return verifyStockCMProbeLease(ctx, lease, fd) }, exchange: func(ctx context.Context, transaction, message uint16) ([]byte, error) {
		return exchangeQMI(ctx, fd, transaction, message)
	}}
	actionError := action(ctx, exchange.run)
	closeError := unix.Close(fd)
	fd = -1
	if closeError != nil {
		return errors.New("STOCK_CM_GET_CLOSE_UNVERIFIED")
	}
	if err = verifyStockCMProbeLease(ctx, lease, -1); err != nil {
		return err
	}
	if exchange.failure != nil {
		return exchange.failure
	}
	return actionError
}

func ReadStockCMNAS(parent context.Context) (GobiNetSignal, error) {
	var out GobiNetSignal
	err := withStockCMReadOnlyCID(parent, 3, func(ctx context.Context, exchange func(context.Context, uint16, uint16) ([]byte, error)) error {
		var err error
		out, err = collectNAS(ctx, exchange)
		return err
	})
	if err != nil {
		return GobiNetSignal{}, err
	}
	out.Source = "gobinet-nas-independent-cid-stock-cm-readonly"
	return out, nil
}

func ReadStockCMSignal(parent context.Context) (GobiNetSignal, error) {
	var out GobiNetSignal
	err := withStockCMReadOnlyCID(parent, 3, func(ctx context.Context, exchange func(context.Context, uint16, uint16) ([]byte, error)) error {
		var err error
		out, err = collectSignalOnly(ctx, exchange)
		return err
	})
	if err != nil {
		return GobiNetSignal{}, err
	}
	out.Source = "gobinet-nas-independent-cid-stock-cm-readonly"
	return out, nil
}

func ReadStockCMProfiles(parent context.Context) (ProfileRecovery, ProfileInventory, error) {
	var recovery ProfileRecovery
	var inventory ProfileInventory
	err := withStockCMReadOnlyCID(parent, 1, func(ctx context.Context, exchange func(context.Context, uint16, uint16) ([]byte, error)) error {
		var err error
		recovery, err = collectProfiles(ctx, exchange)
		if err != nil {
			return err
		}
		frame, err := exchange(ctx, 7, 0x2a)
		if err != nil {
			return err
		}
		inventory, err = parseProfileInventory(frame)
		if err != nil {
			return err
		}
		recovery, err = recovery.withSameCIDInventory(frame)
		return err
	})
	if err != nil {
		return ProfileRecovery{}, ProfileInventory{}, err
	}
	return recovery, inventory, nil
}

func ReadStockCMInventory(parent context.Context) (ProfileInventory, error) {
	var out ProfileInventory
	err := withStockCMReadOnlyCID(parent, 1, func(ctx context.Context, exchange func(context.Context, uint16, uint16) ([]byte, error)) error {
		frame, err := exchange(ctx, 7, 0x2a)
		if err != nil {
			return err
		}
		out, err = parseProfileInventory(frame)
		return err
	})
	if err != nil {
		return ProfileInventory{}, err
	}
	return out, nil
}
