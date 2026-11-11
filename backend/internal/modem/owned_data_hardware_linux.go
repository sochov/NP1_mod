//go:build linux

package modem

import (
	"context"
	"errors"
	"net"
	"os"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// readOwnedQMAPInfo performs the pinned driver's GET-only RMNET_INFO ioctl.
// 0x89f1 (link-state mutation) and all WDA operations are intentionally absent.
func readOwnedQMAPInfo() (ownedQMAPInfo, error) {
	var out ownedQMAPInfo
	if runtime.GOARCH != "arm64" && runtime.GOARCH != "amd64" {
		return out, errors.New("OWNED_DATA_IOCTL_ABI_UNSUPPORTED")
	}
	fd, e := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if e != nil {
		return out, errors.New("OWNED_DATA_QMAP_SOCKET_UNAVAILABLE")
	}
	defer unix.Close(fd)
	var raw [ownedRMNetInfoSize]byte
	var request struct {
		name    [16]byte
		data    unsafe.Pointer
		padding [16]byte
	}
	copy(request.name[:], "usb0")
	request.data = unsafe.Pointer(&raw[0])
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), 0x89f3, uintptr(unsafe.Pointer(&request)))
	runtime.KeepAlive(request)
	runtime.KeepAlive(raw)
	if errno != 0 {
		return out, errors.New("OWNED_DATA_QMAP_GET_FAILED")
	}
	return parseOwnedRMNetInfo(raw[:])
}

func ownedHardwareSnapshot() (ownedQMAPInfo, [3]int, error) {
	var indexes [3]int
	bindings, e := VerifyCellularInterfaces()
	if e != nil || len(bindings) != 3 || bindings[0] != "usb0" || bindings[1] != "usb0.1" || bindings[2] != "usb0.2" {
		return ownedQMAPInfo{}, indexes, errors.New("OWNED_DATA_INTERFACES_UNVERIFIED")
	}
	for i, name := range bindings {
		in, e := net.InterfaceByName(name)
		if e != nil || in.Index <= 0 {
			return ownedQMAPInfo{}, indexes, errors.New("OWNED_DATA_INTERFACE_ID_UNAVAILABLE")
		}
		indexes[i] = in.Index
	}
	for path, want := range map[string]string{"/sys/class/net/usb0/qmap_mode": "2", "/sys/class/net/usb0/qmap_size": "4096", "/sys/module/GobiNet/parameters/qmap_mode": "2"} {
		raw, e := os.ReadFile(path)
		if e != nil || strings.TrimSpace(string(raw)) != want {
			return ownedQMAPInfo{}, indexes, errors.New("OWNED_DATA_QMAP_SYSFS_UNVERIFIED")
		}
	}
	qmap, e := readOwnedQMAPInfo()
	return qmap, indexes, e
}

func InspectOwnedCellularPreflight(parent context.Context) (OwnedCellularPreflight, error) {
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	out := newOwnedPreflight()
	proof, e := InspectStockCMOwnership(ctx)
	if e != nil || proof.Status != "no-cm" || !proof.OwnerVerified || proof.CMProcesses != 0 {
		return out, errors.New("OWNED_DATA_STOCK_CM_NOT_ABSENT")
	}
	out.CMAbsent, out.DriverVerified = true, true
	first, indexes, e := ownedHardwareSnapshot()
	if e != nil {
		return out, e
	}
	if e := inspectQMIOwnership(); e != nil {
		return out, errors.New("OWNED_DATA_QMI_OWNERSHIP_UNVERIFIED")
	}
	second, after, e := ownedHardwareSnapshot()
	if e != nil || first != second || indexes != after || ctx.Err() != nil {
		return out, errors.New("OWNED_DATA_HARDWARE_CHANGED")
	}
	proof, e = InspectStockCMOwnership(ctx)
	if e != nil || proof.Status != "no-cm" || !proof.OwnerVerified || proof.CMProcesses != 0 {
		return out, errors.New("OWNED_DATA_STOCK_CM_CHANGED")
	}
	out.Status, out.QMAPVerified, out.QMAPVersion, out.BindingCount = "available", true, first.version, 2
	out.ObservedAt = time.Now().UTC()
	return out, nil
}
