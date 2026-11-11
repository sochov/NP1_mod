//go:build linux

package modem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// This only chooses the stricter path; it is not itself an authorization.
// A deleted/replaced executable still selects the CM path and fails its hash/
// exe proof, rather than getting a less restrictive retry.
func stockCMProcessPresent(ctx context.Context) (bool, error) {
	items, err := os.ReadDir("/proc")
	if err != nil || len(items) > 4096 {
		return false, errors.New("STOCK_CM_ROUTE_PROC_UNAVAILABLE")
	}
	for _, item := range items {
		if ctx.Err() != nil {
			return false, errors.New("STOCK_CM_ROUTE_TIMEOUT")
		}
		pid, err := strconv.ParseUint(item.Name(), 10, 32)
		if err != nil || pid == 0 {
			continue
		}
		exe, err := os.Readlink(filepath.Join("/proc", item.Name(), "exe"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, errors.New("STOCK_CM_ROUTE_IDENTITY_UNAVAILABLE")
		}
		if strings.TrimSuffix(exe, " (deleted)") == stockCMExecutable {
			return true, nil
		}
	}
	return false, nil
}
