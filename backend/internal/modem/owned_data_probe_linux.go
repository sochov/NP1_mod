//go:build linux

package modem

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type ownedDataInterface struct {
	index, mtu int
	up         bool
	addresses  string
}
type ownedDataLease struct {
	owner         stockCMProcess
	qmap          ownedQMAPInfo
	interfaces    [3]ownedDataInterface
	management    nativeAPNNetworkBaseline
	routes, rules string
}

func captureOwnedInterfaces() ([3]ownedDataInterface, error) {
	var out [3]ownedDataInterface
	for i, name := range []string{"usb0", "usb0.1", "usb0.2"} {
		in, e := net.InterfaceByName(name)
		if e != nil {
			return out, errors.New("OWNED_DATA_INTERFACE_UNAVAILABLE")
		}
		addresses, e := in.Addrs()
		if e != nil {
			return out, errors.New("OWNED_DATA_INTERFACE_ADDRESSES_UNAVAILABLE")
		}
		var encoded []string
		for _, address := range addresses {
			encoded = append(encoded, address.String())
		}
		// All children must be unused. The first prototype will not configure
		// an address or remove an address created by somebody else.
		if len(encoded) != 0 {
			return out, errors.New("OWNED_DATA_INTERFACE_ALREADY_ADDRESSED")
		}
		out[i] = ownedDataInterface{in.Index, in.MTU, in.Flags&net.FlagUp != 0, strings.Join(encoded, "\n")}
	}
	return out, nil
}

func captureOwnedDataLease(ctx context.Context) (ownedDataLease, error) {
	var out ownedDataLease
	proof, e := InspectOwnedCellularPreflight(ctx)
	if e != nil || proof.Status != "available" {
		return out, errors.New("OWNED_DATA_PREFLIGHT_NOT_AVAILABLE")
	}
	items, e := os.ReadDir("/proc")
	if e != nil || len(items) > 4096 {
		return out, errors.New("OWNED_DATA_PROC_UNAVAILABLE")
	}
	count := 0
	for _, item := range items {
		n, e := strconv.ParseUint(item.Name(), 10, 32)
		if e != nil || n == 0 {
			continue
		}
		exe, e := os.Readlink(filepath.Join("/proc", item.Name(), "exe"))
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return out, errors.New("OWNED_DATA_PROCESS_IDENTITY_UNAVAILABLE")
		}
		if exe == stockCMOwnerExecutable {
			out.owner, e = readStockCMProcess(uint32(n), exe)
			if e != nil {
				return out, e
			}
			count++
		}
	}
	if count != 1 || verifiedStockCMOwner(out.owner) != nil {
		return out, errors.New("OWNED_DATA_OWNER_UNVERIFIED")
	}
	selfNS, e := os.Readlink("/proc/self/ns/net")
	if e != nil || selfNS != out.owner.networkNamespace {
		return out, errors.New("OWNED_DATA_NETNS_UNVERIFIED")
	}
	out.qmap, _, e = ownedHardwareSnapshot()
	if e != nil {
		return out, e
	}
	out.interfaces, e = captureOwnedInterfaces()
	if e != nil {
		return out, e
	}
	out.management, e = captureNativeAPNNetwork(ctx)
	if e != nil || out.management.wifi.state != "COMPLETED" || out.management.hotspot == nil {
		return out, errors.New("OWNED_DATA_STA_AND_AP_BASELINE_REQUIRED")
	}
	out.routes, e = nativeAPNCommand(ctx, "/sbin/ip", "-4", "route", "show", "table", "main")
	if e != nil {
		return out, e
	}
	out.rules, e = nativeAPNCommand(ctx, "/sbin/ip", "-4", "rule", "show")
	if e != nil {
		return out, e
	}
	return out, nil
}

// inspectOwnedDataDescriptors includes self and permits exactly the one FD we
// opened. It never reads, duplicates or closes another process's descriptor.
func inspectOwnedDataDescriptors(ctx context.Context, ownFD int) error {
	items, e := os.ReadDir("/proc")
	if e != nil || len(items) > 4096 {
		return errors.New("OWNED_DATA_PROC_UNAVAILABLE")
	}
	count, total := 0, 0
	for _, item := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n, e := strconv.ParseUint(item.Name(), 10, 32)
		if e != nil || n == 0 {
			continue
		}
		directory := filepath.Join("/proc", item.Name(), "fd")
		fds, e := os.ReadDir(directory)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil || len(fds) > 4096 {
			return errors.New("OWNED_DATA_FD_INVENTORY_UNAVAILABLE")
		}
		total += len(fds)
		if total > 65536 {
			return errors.New("OWNED_DATA_FD_INVENTORY_OVERSIZE")
		}
		for _, f := range fds {
			target, e := os.Readlink(filepath.Join(directory, f.Name()))
			if errors.Is(e, os.ErrNotExist) {
				continue
			}
			if e != nil {
				return errors.New("OWNED_DATA_FD_LINK_UNAVAILABLE")
			}
			if target != qmiDevice {
				continue
			}
			nfd, e := strconv.Atoi(f.Name())
			if e != nil || int(n) != os.Getpid() || nfd != ownFD {
				return errors.New("OWNED_DATA_FOREIGN_QMI_OWNER")
			}
			count++
		}
	}
	if ownFD >= 0 && count != 1 || ownFD < 0 && count != 0 {
		return errors.New("OWNED_DATA_OWN_FD_UNVERIFIED")
	}
	return nil
}

func verifyOwnedDataLease(ctx context.Context, before ownedDataLease, ownFD int) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	present, e := stockCMProcessPresent(ctx)
	if e != nil || present {
		return errors.New("OWNED_DATA_STOCK_CM_APPEARED")
	}
	owner, e := readStockCMProcess(before.owner.pid, stockCMOwnerExecutable)
	if e != nil || !stockCMProcessUnchanged(before.owner, owner) {
		return errors.New("OWNED_DATA_OWNER_CHANGED")
	}
	if e := inspectOwnedDataDescriptors(ctx, ownFD); e != nil {
		return e
	}
	qmap, indexes, e := ownedHardwareSnapshot()
	if e != nil || qmap != before.qmap {
		return errors.New("OWNED_DATA_QMAP_CHANGED")
	}
	interfaces, e := captureOwnedInterfaces()
	if e != nil || interfaces != before.interfaces || indexes != [3]int{interfaces[0].index, interfaces[1].index, interfaces[2].index} {
		return errors.New("OWNED_DATA_INTERFACES_CHANGED")
	}
	stock, e := nativeAPNStockSnapshot(ctx)
	if e != nil || stock != before.management.stock {
		return errors.New("OWNED_DATA_STOCK_STATE_CHANGED")
	}
	wifi, e := nativeAPNWiFiStatus(ctx)
	if e != nil || !nativeAPNWiFiUnchanged(before.management.wifi, wifi) {
		return errors.New("OWNED_DATA_STA_CHANGED")
	}
	routes, e := nativeAPNCommand(ctx, "/sbin/ip", "-4", "route", "show", "table", "main")
	if e != nil || routes != before.routes {
		return errors.New("OWNED_DATA_MAIN_ROUTES_CHANGED")
	}
	rules, e := nativeAPNCommand(ctx, "/sbin/ip", "-4", "rule", "show")
	if e != nil || rules != before.rules {
		return errors.New("OWNED_DATA_RULES_CHANGED")
	}
	return nil
}

type ownedDataFD struct {
	fd       int
	identity unix.Stat_t
	qmap     ownedQMAPInfo
	indexes  [3]int
}

func (f ownedDataFD) verifyCleanup(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var stat unix.Stat_t
	if unix.Fstat(f.fd, &stat) != nil || stat.Dev != f.identity.Dev || stat.Ino != f.identity.Ino || stat.Rdev != f.identity.Rdev || stat.Mode != f.identity.Mode {
		return errors.New("OWNED_DATA_CID_FD_CHANGED")
	}
	qmap, indexes, e := ownedHardwareSnapshot()
	if e != nil || qmap != f.qmap || indexes != f.indexes {
		return errors.New("OWNED_DATA_CLEANUP_BINDING_CHANGED")
	}
	return nil
}

// exchange accepts only ownedDataOperation. This intentionally does not extend
// fixedQMIRequest, the production observer, or its GET-only allowlist.
func (f ownedDataFD) exchange(ctx context.Context, op ownedDataOperation, handle ownedPacketHandle) ([]byte, bool, error) {
	if e := ctx.Err(); e != nil {
		return nil, false, e
	}
	if e := f.verifyCleanup(ctx); e != nil {
		return nil, false, e
	}
	request, e := ownedDataRequest(op, handle)
	if e != nil {
		return nil, false, e
	}
	n, e := unix.Write(f.fd, request)
	written := n > 0
	if e != nil {
		return nil, written, e
	}
	if n != len(request) {
		return nil, written, errors.New("OWNED_DATA_SHORT_QMI_WRITE")
	}
	limit := 2 * time.Second
	if op == ownedStart {
		limit = 15 * time.Second
	}
	if op == ownedStop {
		limit = 4 * time.Second
	}
	until := time.Now().Add(limit)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(until) {
		until = deadline
	}
	buffer := make([]byte, maximumQMIFrame+1)
	for frames := 0; frames < 64; {
		if e := ctx.Err(); e != nil {
			return nil, true, e
		}
		remaining := time.Until(until)
		if remaining <= 0 {
			return nil, true, context.DeadlineExceeded
		}
		poll := []unix.PollFd{{Fd: int32(f.fd), Events: unix.POLLIN}}
		n, e := unix.Poll(poll, max(1, int(min(remaining, 100*time.Millisecond).Milliseconds())))
		if e == unix.EINTR {
			continue
		}
		if e != nil {
			return nil, true, e
		}
		if n == 0 {
			continue
		}
		if poll[0].Revents&(unix.POLLHUP|unix.POLLNVAL|unix.POLLERR) != 0 {
			return nil, true, errors.New("OWNED_DATA_MODEM_DISCONNECTED")
		}
		if poll[0].Revents&unix.POLLIN == 0 {
			continue
		}
		n, e = unix.Read(f.fd, buffer)
		if e == unix.EAGAIN || e == unix.EINTR {
			continue
		}
		if e != nil {
			return nil, true, e
		}
		if n <= 0 {
			return nil, true, io.ErrUnexpectedEOF
		}
		frames++
		_, e = parseOwnedDataResponse(op, buffer[:n])
		if errors.Is(e, errUnrelatedQMI) {
			continue
		}
		if e != nil {
			var protocol *qmiProtocolError
			if errors.As(e, &protocol) {
				return bytes.Clone(buffer[:n]), true, e
			}
			return nil, true, e
		}
		return bytes.Clone(buffer[:n]), true, nil
	}
	return nil, true, errors.New("OWNED_DATA_UNRELATED_FRAMES_LIMIT")
}

func collectOwnedSavedProfiles(ctx context.Context, fd int, guard func(context.Context) error) (ProfileRecovery, error) {
	exchange := func(ctx context.Context, transaction, message uint16) ([]byte, error) {
		if e := guard(ctx); e != nil {
			return nil, e
		}
		frame, e := exchangeQMI(ctx, fd, transaction, message)
		// Preserve a correlated protocol-error frame (e.g. absent profile 2),
		// but discard everything if ownership changes after the GET.
		if after := guard(ctx); after != nil {
			return nil, after
		}
		return frame, e
	}
	recovery, e := collectProfiles(ctx, exchange)
	if e != nil {
		return ProfileRecovery{}, e
	}
	frame, e := exchange(ctx, 7, 0x002a)
	if e != nil {
		return ProfileRecovery{}, e
	}
	recovery, e = recovery.withSameCIDInventory(frame)
	if e != nil {
		return ProfileRecovery{}, e
	}
	absent, _ := recovery.profileAbsence(2)
	if !recovery.profiles[0].nativeFieldsComplete() || !absent {
		return ProfileRecovery{}, errors.New("OWNED_DATA_SAVED_PROFILE_BASELINE_UNVERIFIED")
	}
	return recovery, nil
}

// ProbeOwnedCellularSession is an explicit bounded experiment, never invoked
// by the Web API/worker. It starts/stops only its own IPv4 call on mux 0x82.
// Interface addresses, link flags, routes, resolver and APN settings are not set.
func ProbeOwnedCellularSession(parent context.Context) (OwnedDataSessionResult, error) {
	out := newOwnedDataSessionResult()
	if os.Geteuid() != 0 || os.Getenv("NCC_OWNED_DATA_SESSION_TRIAL") != "1" {
		return out, errors.New("OWNED_DATA_EXPLICIT_TRIAL_REQUIRED")
	}
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	if !gobiQueryMu.TryLock() {
		return out, errors.New("OWNED_DATA_MODEM_BUSY")
	}
	defer gobiQueryMu.Unlock()
	journalRoot, journalLock, e := openOwnedDataJournal()
	if e != nil {
		return out, e
	}
	defer journalRoot.Close()
	defer journalLock.Close()
	lease, e := captureOwnedDataLease(ctx)
	if e != nil {
		return out, e
	}
	if e := verifyOwnedDataLease(ctx, lease, -1); e != nil {
		return out, e
	}
	fd, e := unix.Open(qmiDevice, unix.O_RDWR|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOCTTY, 0)
	if e != nil {
		return out, errors.New("OWNED_DATA_DEVICE_OPEN_FAILED")
	}
	defer unix.Close(fd)
	if e := verifyOwnedDataLease(ctx, lease, fd); e != nil {
		return out, e
	}
	if e := unix.IoctlSetInt(fd, 0x8be1, 1); e != nil {
		return out, errors.New("OWNED_DATA_WDS_CID_ALLOCATION_FAILED")
	}
	if e := verifyOwnedDataLease(ctx, lease, fd); e != nil {
		return out, e
	}
	transport := ownedDataFD{fd: fd, qmap: lease.qmap, indexes: [3]int{lease.interfaces[0].index, lease.interfaces[1].index, lease.interfaces[2].index}}
	if unix.Fstat(fd, &transport.identity) != nil {
		return out, errors.New("OWNED_DATA_CID_IDENTITY_UNAVAILABLE")
	}
	guard := func(ctx context.Context) error { return verifyOwnedDataLease(ctx, lease, fd) }
	original, e := collectOwnedSavedProfiles(ctx, fd, guard)
	if e != nil {
		return out, errors.New("OWNED_DATA_PROFILE_CAPTURE_FAILED")
	}
	backup, e := original.Save()
	if e != nil {
		return out, errors.New("OWNED_DATA_PROFILE_BACKUP_FAILED")
	}
	journal, e := newPrivateOwnedJournal(lease, backup.BackupFile)
	if e != nil {
		return out, e
	}
	publish := func(phase string, result OwnedDataSessionResult, handle ownedPacketHandle) error {
		journal.Phase, journal.Result, journal.PacketHandle = phase, result, handle.value
		if result.SettingsVerified {
			journal.IPv4, journal.Gateway = result.settings.address.String(), result.settings.gateway.String()
			journal.PrefixBits, journal.MTU = result.settings.prefixBits, result.settings.mtu
			journal.DNS = nil
			for _, address := range result.settings.dns {
				journal.DNS = append(journal.DNS, address.String())
			}
		}
		return writeOwnedDataJournal(journalRoot, journal, verifyProfileRecoveryOwner)
	}
	if e := publish("prepared", out, ownedPacketHandle{}); e != nil {
		return out, e
	}
	out, e = runOwnedDataSession(ctx, ownedSessionTransport{
		exchange:     transport.exchange,
		guard:        guard,
		cleanupGuard: transport.verifyCleanup,
		publish:      publish,
	}, 3*time.Second)
	if out.CleanupVerified {
		profileCtx, done := context.WithTimeout(context.Background(), 15*time.Second)
		after, readErr := collectOwnedSavedProfiles(profileCtx, fd, guard)
		done()
		out.ProfilesPreserved = readErr == nil && ownedProfilesIdentical(original, after)
		if !out.ProfilesPreserved {
			out.Status = "failed"
			if e == nil {
				e = errors.New("OWNED_DATA_PROFILE_READBACK_UNVERIFIED")
			}
		}
	}
	// The AP witness includes HTTP and owned DHCP/process identity. It is
	// rechecked after Stop before baseline preservation is reported.
	verifyCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	current, apErr := captureNativeAPNHotspot(verifyCtx)
	if apErr != nil || lease.management.hotspot == nil || current != *lease.management.hotspot {
		out.BaselinePreserved = false
		if out.CleanupVerified {
			out.Status = "failed"
		}
		e = errors.New("OWNED_DATA_AP_MANAGEMENT_CHANGED")
	}
	phase := "incomplete"
	if out.CleanupVerified && out.BaselinePreserved && out.ProfilesPreserved {
		phase = "complete"
	}
	if err := publish(phase, out, ownedPacketHandle{journal.PacketHandle}); err != nil {
		return out, errors.New("OWNED_DATA_FINAL_JOURNAL_UNVERIFIED")
	}
	return out, e
}
