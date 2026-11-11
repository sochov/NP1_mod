//go:build !linux

package modem

import (
	"context"
	"errors"
)

func ProbeOwnedCellularSession(context.Context) (OwnedDataSessionResult, error) {
	return newOwnedDataSessionResult(), errors.New("OWNED_DATA_LINUX_REQUIRED")
}
