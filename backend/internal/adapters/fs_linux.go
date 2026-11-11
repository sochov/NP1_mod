//go:build linux

package adapters

import (
	"strconv"
	"syscall"
)

func diskUsage(path string) map[string]any {
	var st syscall.Statfs_t
	if syscall.Statfs(path, &st) != nil {
		return nil
	}
	total := st.Blocks * uint64(st.Bsize)
	free := st.Bavail * uint64(st.Bsize)
	if total == 0 {
		return nil
	}
	return map[string]any{"totalBytes": strconv.FormatUint(total, 10), "freeBytes": strconv.FormatUint(free, 10), "usedBytes": strconv.FormatUint(total-free, 10), "usedPercent": float64(total-free) * 100 / float64(total)}
}
