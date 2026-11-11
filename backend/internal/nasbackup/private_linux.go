//go:build linux

package nasbackup

import (
	"errors"
	"os"
	"syscall"
)

func privateRegular(info os.FileInfo) error {
	if info == nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return errors.New("NAS_PRIVATE_FILE_UNSAFE")
	}
	if st, ok := info.Sys().(*syscall.Stat_t); !ok || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 {
		return errors.New("NAS_PRIVATE_FILE_UNSAFE")
	}
	return nil
}
func privateDirectory(info os.FileInfo) error {
	if info.Mode().Perm() != 0700 {
		return errors.New("NAS_PRIVATE_DIRECTORY_UNSAFE")
	}
	if st, ok := info.Sys().(*syscall.Stat_t); !ok || st.Uid != uint32(os.Geteuid()) {
		return errors.New("NAS_PRIVATE_DIRECTORY_UNSAFE")
	}
	return nil
}
func lockState(directory string) (func(), error) {
	file, err := os.OpenFile(directory+"/transfer.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, errors.New("NAS_LOCK_UNAVAILABLE")
	}
	info, err := file.Stat()
	if err != nil || privateRegular(info) != nil {
		file.Close()
		return nil, errors.New("NAS_LOCK_UNSAFE")
	}
	if syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		file.Close()
		return nil, errors.New("NAS_BUSY")
	}
	return func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }, nil
}
