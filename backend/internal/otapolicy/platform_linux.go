//go:build linux

package otapolicy

import (
	"os"
	"syscall"
)

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
