//go:build linux

package trips

import (
	"os"
	"syscall"
)

func archiveFilePrivate(info os.FileInfo) bool {
	value, ok := info.Sys().(*syscall.Stat_t)
	return ok && value.Uid == uint32(os.Geteuid()) && info.Mode().Perm() == 0600
}
func syncArchiveDirectory(directory string) error {
	f, e := os.Open(directory)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
