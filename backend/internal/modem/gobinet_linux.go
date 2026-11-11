//go:build linux

package modem

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const qmiDevice = "/dev/qcqmi0"
const pinnedGobiModule = "/lib/modules/4.14.206-perf/kernel/drivers/net/usb/GobiNet/GobiNet.ko"

var gobiQueryMu sync.Mutex

// inspectQMIOwnership is deliberately conservative: this first verified ABI
// implementation refuses concurrent users, even though the driver isolates CIDs.
// It never reads any other process's device FD or duplicates that FD.
func inspectQMIOwnership() error {
	pids, e := os.ReadDir("/proc")
	if e != nil {
		return e
	}
	self := strconv.Itoa(os.Getpid())
	for _, pid := range pids {
		if _, e := strconv.Atoi(pid.Name()); e != nil || pid.Name() == self {
			continue
		}
		directory := filepath.Join("/proc", pid.Name(), "fd")
		fds, e := os.ReadDir(directory)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return errors.New("cannot verify QMI descriptor ownership")
		}
		for _, fd := range fds {
			target, e := os.Readlink(filepath.Join(directory, fd.Name()))
			if e != nil {
				if errors.Is(e, os.ErrNotExist) {
					continue
				}
				return errors.New("cannot inspect QMI descriptor")
			}
			if target == qmiDevice {
				return errors.New("QMI channel already opened by another process")
			}
		}
	}
	return nil
}
func verifyGobiNet() error {
	version, e := os.ReadFile("/sys/module/GobiNet/version")
	if e != nil || strings.TrimSpace(string(version)) != GobiNetDriverVersion {
		return errors.New("unverified GobiNet driver version")
	}
	srcversion, e := os.ReadFile("/sys/module/GobiNet/srcversion")
	if e != nil || strings.TrimSpace(string(srcversion)) != "533BB7E5866E52F63B9ACCB" {
		return errors.New("unverified GobiNet module build")
	}
	module, e := os.Open(pinnedGobiModule)
	if e != nil {
		return e
	}
	sum := sha256.New()
	n, e := io.Copy(sum, io.LimitReader(module, 2*1024*1024))
	module.Close()
	if e != nil || n != 1025145 || hex.EncodeToString(sum.Sum(nil)) != GobiNetModuleSHA256 {
		return errors.New("GobiNet module hash mismatch")
	}
	var st unix.Stat_t
	if e = unix.Lstat(qmiDevice, &st); e != nil {
		return e
	}
	if st.Mode&unix.S_IFMT != unix.S_IFCHR {
		return errors.New("QMI path is not a character device")
	}
	dev, e := os.ReadFile("/sys/class/GobiQMI/qcqmi0/dev")
	if e != nil || strings.TrimSpace(string(dev)) != fmt.Sprintf("%d:%d", unix.Major(uint64(st.Rdev)), unix.Minor(uint64(st.Rdev))) {
		return errors.New("QMI character device does not match GobiNet sysfs")
	}
	device, e := filepath.EvalSymlinks("/sys/class/GobiQMI/qcqmi0/device")
	if e != nil {
		return e
	}
	for device != "/" {
		vendor, err := os.ReadFile(filepath.Join(device, "idVendor"))
		if err == nil {
			product, e := os.ReadFile(filepath.Join(device, "idProduct"))
			if e != nil || strings.TrimSpace(string(vendor)) != "2c7c" || strings.TrimSpace(string(product)) != "0125" {
				return errors.New("unverified QMI USB modem")
			}
			return nil
		}
		device = filepath.Dir(device)
	}
	return errors.New("QMI USB ancestry unavailable")
}

// ReadGobiNetSignal keeps the verified signal-only path. The background observer
// must not issue optional status queries until their device test is approved.
func ReadGobiNetSignal(ctx context.Context) (GobiNetSignal, error) { return readGobiNet(ctx, false) }

// ReadGobiNetStatus is an opt-in probe that adds the fixed serving-system and
// RF-band queries on the same independent NAS FD. It is not used implicitly.
func ReadGobiNetStatus(ctx context.Context) (GobiNetSignal, error) { return readGobiNet(ctx, true) }

// readGobiNet allocates a new NAS(3) CID and closes only its own FD.
// No CTL/WDS/WDA/reset/configuration messages are exposed.
// Run as a short-lived helper with an external timeout: driver open/ioctl/close
// can wait inside the kernel and cannot be made context-cancellable by Go.
func readGobiNet(ctx context.Context, withStatus bool) (GobiNetSignal, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if !gobiQueryMu.TryLock() {
		return GobiNetSignal{}, errors.New("NAS observation already in progress")
	}
	defer gobiQueryMu.Unlock()
	if e := ctx.Err(); e != nil {
		return GobiNetSignal{}, e
	}
	if e := verifyGobiNet(); e != nil {
		return GobiNetSignal{}, e
	}
	if e := inspectQMIOwnership(); e != nil {
		return GobiNetSignal{}, e
	}
	fd, e := unix.Open(qmiDevice, unix.O_RDWR|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOCTTY, 0)
	if e != nil {
		return GobiNetSignal{}, e
	}
	defer unix.Close(fd)
	if e = inspectQMIOwnership(); e != nil {
		return GobiNetSignal{}, e
	}
	if e = ctx.Err(); e != nil {
		return GobiNetSignal{}, e
	}
	if e = unix.IoctlSetInt(fd, 0x8be1, 3); e != nil {
		return GobiNetSignal{}, fmt.Errorf("allocate independent NAS CID: %w", e)
	}
	if e = ctx.Err(); e != nil {
		return GobiNetSignal{}, e
	}
	exchange := func(ctx context.Context, transaction, message uint16) ([]byte, error) {
		return exchangeNAS(ctx, fd, transaction, message)
	}
	if !withStatus {
		return collectSignalOnly(ctx, exchange)
	}
	return collectNAS(ctx, exchange)
}

func exchangeNAS(ctx context.Context, fd int, transaction, message uint16) ([]byte, error) {
	// IDs are paired as well as allowlisted. No caller-controlled QMI request
	// reaches the kernel, and each reply has a distinct fixed transaction ID.
	if !(transaction == 1 && message == 0x004f || transaction == 2 && message == 0x0024 || transaction == 3 && message == 0x0031) {
		return nil, errors.New("NAS query not allowlisted")
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	request := []byte{0, byte(transaction), byte(transaction >> 8), byte(message), byte(message >> 8), 0, 0}
	n, e := unix.Write(fd, request)
	if e != nil {
		return nil, e
	}
	if n != len(request) {
		return nil, errors.New("short QMI request write")
	}
	until := time.Now().Add(2 * time.Second)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(until) {
		until = deadline
	}
	buffer := make([]byte, maximumQMIFrame)
	for count := 0; count < 32; {
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		remaining := time.Until(until)
		if remaining <= 0 {
			return nil, context.DeadlineExceeded
		}
		wait := min(remaining, 100*time.Millisecond)
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		ready, err := unix.Poll(poll, max(1, int(wait.Milliseconds())))
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return nil, err
		}
		if ready == 0 {
			continue
		}
		if poll[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			return nil, errors.New("QMI device disconnected")
		}
		if poll[0].Revents&unix.POLLIN == 0 {
			continue
		}
		n, e = unix.Read(fd, buffer)
		if e == unix.EAGAIN || e == unix.EINTR {
			continue
		}
		if e != nil {
			return nil, e
		}
		if n <= 0 {
			return nil, io.ErrUnexpectedEOF
		}
		count++
		_, e := parseNASResponse(buffer[:n], transaction, message)
		if errors.Is(e, errUnrelatedQMI) {
			continue
		}
		return buffer[:n], e
	}
	return nil, errors.New("too many unrelated NAS frames")
}
