//go:build linux

package adapters

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func stockSyncIdentityFor(info os.FileInfo) (stockSyncIdentity, error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return stockSyncIdentity{}, errors.New("stock singleton stat unavailable")
	}
	return stockSyncIdentity{uint64(unix.Major(st.Dev)), uint64(unix.Minor(st.Dev)), st.Ino, uint64(st.Nlink), uint64(st.Uid)}, nil
}

func stockSyncPrivateMode(info os.FileInfo) bool { return info.Mode().Perm() == 0700 }
