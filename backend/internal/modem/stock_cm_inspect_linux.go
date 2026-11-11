//go:build linux

package modem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func readStockCMProcess(pid uint32, executable string) (stockCMProcess, error) {
	result := stockCMProcess{pid: pid, executable: executable}
	base := filepath.Join("/proc", strconv.FormatUint(uint64(pid), 10))
	actual, e := os.Readlink(base + "/exe")
	if e != nil || actual != executable {
		return result, errors.New("STOCK_CM_EXECUTABLE_CHANGED")
	}
	want := stockCMOwnerExecutableSHA256
	if executable == stockCMExecutable {
		want = stockCMExecutableSHA256
	} else if executable != stockCMOwnerExecutable {
		return result, errors.New("STOCK_CM_EXECUTABLE_UNSUPPORTED")
	}
	if nativeAPNFileHash(base+"/exe", want) != nil {
		return result, errors.New("STOCK_CM_EXECUTABLE_UNVERIFIED")
	}
	result.digest = want
	raw, e := nativeAPNReadBounded(base+"/stat", 8192)
	if e != nil {
		return result, errors.New("STOCK_CM_PROCESS_STAT_UNAVAILABLE")
	}
	result.start, e = stockCMProcessStart(raw, pid)
	if e != nil {
		return result, e
	}
	raw, e = nativeAPNReadBounded(base+"/status", 32768)
	if e != nil {
		return result, errors.New("STOCK_CM_PROCESS_IDS_UNAVAILABLE")
	}
	result.uids, result.gids, e = stockCMProcessIDs(raw)
	if e != nil {
		return result, e
	}
	raw, e = nativeAPNReadBounded(base+"/cgroup", 16384)
	if e != nil {
		return result, errors.New("STOCK_CM_CGROUP_UNAVAILABLE")
	}
	result.cgroups, e = stockCMProcessCgroups(raw)
	if e != nil {
		return result, e
	}
	result.networkNamespace, e = os.Readlink(base + "/ns/net")
	if e != nil {
		return result, errors.New("STOCK_CM_NETNS_UNAVAILABLE")
	}
	if executable == stockCMExecutable {
		raw, e = nativeAPNReadBounded(base+"/cmdline", 256)
		if e != nil {
			return result, errors.New("STOCK_CM_ARGUMENTS_UNAVAILABLE")
		}
		result.arguments, e = stockCMProcessArguments(raw)
		if e != nil {
			return result, e
		}
	}
	return result, nil
}

// InspectStockCMOwnership reads proc/sysfs and immutable executable files only.
// No other process's device FD is opened/duplicated, and no AT/QMI is sent.
func InspectStockCMOwnership(parent context.Context) (StockCMOwnershipProof, error) {
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	out := StockCMOwnershipProof{ObservedAt: time.Now().UTC(), Source: "linux-stock-cm-provenance-readonly", Status: "unknown"}
	if verifyGobiNet() != nil {
		return out, errors.New("STOCK_CM_MODEM_UNVERIFIED")
	}
	pids, e := os.ReadDir("/proc")
	if e != nil {
		return out, errors.New("STOCK_CM_PROC_UNAVAILABLE")
	}
	var owner stockCMProcess
	var candidates []stockCMProcess
	ownerCount := 0
	for _, item := range pids {
		if e := ctx.Err(); e != nil {
			return out, errors.New("STOCK_CM_INSPECTION_TIMEOUT")
		}
		n, e := strconv.ParseUint(item.Name(), 10, 32)
		if e != nil || n == 0 {
			continue
		}
		exe, e := os.Readlink(filepath.Join("/proc", item.Name(), "exe"))
		if e != nil {
			continue
		}
		if exe != stockCMOwnerExecutable && exe != stockCMExecutable {
			continue
		}
		identity, e := readStockCMProcess(uint32(n), exe)
		if e != nil {
			return out, e
		}
		if exe == stockCMOwnerExecutable {
			owner = identity
			ownerCount++
		} else {
			candidates = append(candidates, identity)
		}
	}
	out.CMProcesses = len(candidates)
	if ownerCount != 1 {
		return out, errors.New("STOCK_CM_OWNER_NOT_UNIQUE")
	}
	if e := verifiedStockCMOwner(owner); e != nil {
		return out, e
	}
	out.OwnerVerified = true
	if len(candidates) > 1 {
		return out, errors.New("STOCK_CM_NOT_UNIQUE")
	}
	if len(candidates) == 0 {
		after, e := readStockCMProcess(owner.pid, stockCMOwnerExecutable)
		if e != nil || !stockCMProcessUnchanged(owner, after) {
			return out, errors.New("STOCK_CM_OWNER_CHANGED_DURING_INSPECTION")
		}
		out.Status = "no-cm"
		return out, nil
	}
	cm := candidates[0]
	if e := verifiedStockCMProcess(cm, owner); e != nil {
		return out, e
	}
	for _, item := range pids {
		if ctx.Err() != nil {
			return out, errors.New("STOCK_CM_INSPECTION_TIMEOUT")
		}
		n, e := strconv.ParseUint(item.Name(), 10, 32)
		if e != nil || n == 0 {
			continue
		}
		dir := filepath.Join("/proc", item.Name(), "fd")
		fds, e := os.ReadDir(dir)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return out, errors.New("STOCK_CM_FD_INVENTORY_UNAVAILABLE")
		}
		for _, fd := range fds {
			target, e := os.Readlink(filepath.Join(dir, fd.Name()))
			if errors.Is(e, os.ErrNotExist) {
				continue
			}
			if e != nil {
				return out, errors.New("STOCK_CM_FD_LINK_UNAVAILABLE")
			}
			if target != qmiDevice {
				continue
			}
			if uint32(n) != cm.pid {
				return out, errors.New("STOCK_CM_OTHER_QMI_OWNER")
			}
			out.QMIDescriptors++
			if out.QMIDescriptors > 32 {
				return out, errors.New("STOCK_CM_FD_COUNT_UNSUPPORTED")
			}
		}
	}
	if out.QMIDescriptors == 0 {
		return out, errors.New("STOCK_CM_QMI_FD_NOT_OBSERVED")
	}
	after, e := readStockCMProcess(cm.pid, stockCMExecutable)
	if e != nil || !stockCMProcessUnchanged(cm, after) {
		return out, errors.New("STOCK_CM_CHANGED_DURING_INSPECTION")
	}
	ownerAfter, e := readStockCMProcess(owner.pid, stockCMOwnerExecutable)
	if e != nil || !stockCMProcessUnchanged(owner, ownerAfter) {
		return out, errors.New("STOCK_CM_OWNER_CHANGED_DURING_INSPECTION")
	}
	if ctx.Err() != nil {
		return out, errors.New("STOCK_CM_INSPECTION_TIMEOUT")
	}
	out.Status = "available"
	out.Verified = true
	out.ObservedAt = time.Now().UTC()
	return out, nil
}
