//go:build !linux

package modem

import (
	"context"
	"errors"
)

func InspectStockCMOwnership(context.Context) (StockCMOwnershipProof, error) {
	return StockCMOwnershipProof{Status: "unknown"}, errors.New("STOCK_CM_LINUX_REQUIRED")
}
