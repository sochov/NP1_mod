//go:build linux

package adapters

import (
	"fmt"
	"os"
	"syscall"
)

func recordingMountToken(root string) (string, error) {
	st, e := os.Stat(root)
	if e != nil {
		return "", e
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("mount identity unavailable")
	}
	var fs syscall.Statfs_t
	if e = syscall.Statfs(root, &fs); e != nil {
		return "", e
	}
	return fmt.Sprintf("%d/%v", sys.Dev, fs.Fsid), nil
}
