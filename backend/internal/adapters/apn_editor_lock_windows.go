//go:build windows

package adapters

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
)

func apnEditorNoFollow() int           { return 0 }
func apnEditorOwner(os.FileInfo) error { return nil } // Developer tests; NP1 enforces UID on Linux.
func apnEditorOSLock(file *os.File, exclusive bool) error {
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if exclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	e := windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 1, 0, &windows.Overlapped{})
	if errors.Is(e, windows.ERROR_LOCK_VIOLATION) {
		return errAPNApplyBusy
	}
	if e != nil {
		return errors.New("APN_EDITOR_LOCK_FAILED")
	}
	return nil
}
func apnEditorOSUnlock(file *os.File) {
	_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &windows.Overlapped{})
}
