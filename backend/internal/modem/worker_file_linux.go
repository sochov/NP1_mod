//go:build linux

package modem

import (
	"os"
	"syscall"
)

func trustedWorkerFile(st os.FileInfo) bool {
	owner, ok := st.Sys().(*syscall.Stat_t)
	return ok && owner.Uid == 0 && st.Mode().Perm()&0022 == 0
}
