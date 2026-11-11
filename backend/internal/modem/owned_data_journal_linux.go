//go:build linux

package modem

import (
	"errors"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func openOwnedDataJournal() (root *os.Root, lock *os.File, err error) {
	root, err = profileRecoveryRoot(ownedDataJournalDirectory, true, verifyProfileRecoveryOwner)
	if err != nil {
		return nil, nil, errors.New("OWNED_DATA_JOURNAL_ROOT_UNAVAILABLE")
	}
	defer func() {
		if err != nil {
			if lock != nil {
				lock.Close()
			}
			root.Close()
			root = nil
			lock = nil
		}
	}()
	lock, err = root.OpenFile("lock", os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return root, lock, errors.New("OWNED_DATA_LOCK_UNAVAILABLE")
	}
	info, e := lock.Stat()
	if e != nil || !info.Mode().IsRegular() || verifyProfileRecoveryOwner(info, 0600) != nil {
		return root, lock, errors.New("OWNED_DATA_LOCK_OWNER_INVALID")
	}
	if unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB) != nil {
		return root, lock, errors.New("OWNED_DATA_PROBE_ALREADY_RUNNING")
	}
	info, e = root.Lstat("state.json")
	if errors.Is(e, os.ErrNotExist) {
		return root, lock, nil
	}
	if e != nil || !info.Mode().IsRegular() || info.Size() > maximumOwnedDataJournal || verifyProfileRecoveryOwner(info, 0600) != nil {
		return root, lock, errors.New("OWNED_DATA_PREVIOUS_JOURNAL_INVALID")
	}
	file, e := root.OpenFile("state.json", os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if e != nil {
		return root, lock, errors.New("OWNED_DATA_PREVIOUS_JOURNAL_UNAVAILABLE")
	}
	raw, e := io.ReadAll(io.LimitReader(file, maximumOwnedDataJournal+1))
	file.Close()
	if e != nil || !ownedJournalReusable(raw) {
		return root, lock, errors.New("OWNED_DATA_PREVIOUS_CLEANUP_UNVERIFIED")
	}
	return root, lock, nil
}

func newPrivateOwnedJournal(lease ownedDataLease, backup string) (privateOwnedDataJournal, error) {
	var state privateOwnedDataJournal
	raw, e := nativeAPNReadBounded("/proc/sys/kernel/random/boot_id", 128)
	if e != nil || !nativeAPNBootPattern.MatchString(strings.TrimSpace(string(raw))) {
		return state, errors.New("OWNED_DATA_BOOT_ID_UNAVAILABLE")
	}
	state.BootID = strings.TrimSpace(string(raw))
	state.ProcessID = os.Getpid()
	raw, e = nativeAPNReadBounded("/proc/self/stat", 8192)
	if e != nil {
		return state, errors.New("OWNED_DATA_PROCESS_ID_UNAVAILABLE")
	}
	state.ProcessStart, e = stockCMProcessStart(raw, uint32(os.Getpid()))
	if e != nil {
		return state, errors.New("OWNED_DATA_PROCESS_ID_UNAVAILABLE")
	}
	if _, e = strconv.ParseUint(state.ProcessStart, 10, 64); e != nil {
		return state, errors.New("OWNED_DATA_PROCESS_ID_UNAVAILABLE")
	}
	state.Schema, state.Phase, state.ProfileBackup = 1, "prepared", backup
	state.OwnerPID, state.OwnerStart = lease.owner.pid, lease.owner.start
	state.IfIndexes = [3]int{lease.interfaces[0].index, lease.interfaces[1].index, lease.interfaces[2].index}
	state.WiFiSSID, state.WiFiID, state.WiFiIPv4 = lease.management.wifi.ssid, lease.management.wifi.id, lease.management.wifi.address
	state.MainRoutes, state.Rules = lease.routes, lease.rules
	return state, nil
}
