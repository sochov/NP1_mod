//go:build linux

package led

import (
	"errors"
	"os"
	"syscall"
)

func screenFilePermissions(st os.FileInfo) error {
	stat, ok := st.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || st.Mode().Perm() != 0600 {
		return errors.New("unsafe screen runtime permissions")
	}
	return nil
}
