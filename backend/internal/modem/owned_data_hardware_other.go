//go:build !linux

package modem

import (
	"context"
	"errors"
)

func InspectOwnedCellularPreflight(context.Context) (OwnedCellularPreflight, error) {
	return newOwnedPreflight(), errors.New("OWNED_DATA_LINUX_REQUIRED")
}
