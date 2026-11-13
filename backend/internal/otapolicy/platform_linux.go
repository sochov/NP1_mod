//go:build linux

package otapolicy

import (
	"os"
	"syscall"
	"unsafe"
)

// FS_IOC_GETFLAGS / FS_IOC_SETFLAGS on arm64 and amd64, and FS_IMMUTABLE_FL.
const (
	fsIocGetflags = 0x80086601
	fsIocSetflags = 0x40086602
	fsImmutableFl = 0x00000010
)

func inodeFlags(file *os.File) (int, error) {
	var flags int
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), fsIocGetflags, uintptr(unsafe.Pointer(&flags)))
	if errno != 0 {
		return 0, errno
	}
	return flags, nil
}

// setImmutable needs CAP_LINUX_IMMUTABLE. A missing file is not an error when lifting.
func setImmutable(path string, on bool) error {
	file, e := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if e != nil {
		if !on && os.IsNotExist(e) {
			return nil
		}
		return e
	}
	defer file.Close()
	flags, e := inodeFlags(file)
	if e != nil {
		return e
	}
	next := flags &^ fsImmutableFl
	if on {
		next = flags | fsImmutableFl
	}
	if next == flags {
		return nil
	}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), fsIocSetflags, uintptr(unsafe.Pointer(&next)))
	if errno != 0 {
		return errno
	}
	return nil
}
func isImmutable(path string) bool {
	file, e := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if e != nil {
		return false
	}
	defer file.Close()
	flags, e := inodeFlags(file)
	return e == nil && flags&fsImmutableFl != 0
}

func identify(info os.FileInfo, out *snapshot) {
	if value, ok := info.Sys().(*syscall.Stat_t); ok {
		out.UID = value.Uid
		out.GID = value.Gid
		out.Device = uint64(value.Dev)
		out.Inode = value.Ino
	}
}
func setOwner(file *os.File, uid, gid uint32) error { return file.Chown(int(uid), int(gid)) }
func lockPolicy(path string) (func(), error) {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, ErrConflict
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}
