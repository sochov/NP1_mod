//go:build linux

package broker

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func apnRuntimeNoFollow() int { return unix.O_NOFOLLOW }
func trustedAPNRuntime(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	// ServeAPN requires UID 0. Accepting the current UID also permits meaningful
	// unprivileged host tests without weakening the production worker boundary.
	return ok && stat.Uid == uint32(os.Geteuid()) && info.Mode().Perm() == 0600
}
