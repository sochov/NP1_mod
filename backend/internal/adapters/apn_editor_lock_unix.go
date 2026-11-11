//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package adapters

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"syscall"
)

func apnEditorNoFollow() int { return unix.O_NOFOLLOW }
func apnEditorOwner(info os.FileInfo) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Geteuid()) {
		return errors.New("APN_PRIVATE_OWNER_INVALID")
	}
	return nil
}
func apnEditorOSLock(file *os.File, exclusive bool) error {
	mode := unix.LOCK_SH | unix.LOCK_NB
	if exclusive {
		mode = unix.LOCK_EX | unix.LOCK_NB
	}
	e := unix.Flock(int(file.Fd()), mode)
	if errors.Is(e, unix.EWOULDBLOCK) || errors.Is(e, unix.EAGAIN) {
		return errAPNApplyBusy
	}
	if e != nil {
		return errors.New("APN_EDITOR_LOCK_FAILED")
	}
	return nil
}
func apnEditorOSUnlock(file *os.File) { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN) }
