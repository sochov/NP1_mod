//go:build !linux

package modem

import (
	"context"
	"errors"
)

func ReadStockCMNAS(context.Context) (GobiNetSignal, error) {
	return GobiNetSignal{}, errors.New("STOCK_CM_LINUX_REQUIRED")
}

func ReadStockCMSignal(context.Context) (GobiNetSignal, error) {
	return GobiNetSignal{}, errors.New("STOCK_CM_LINUX_REQUIRED")
}

func ReadStockCMProfiles(context.Context) (ProfileRecovery, ProfileInventory, error) {
	return ProfileRecovery{}, ProfileInventory{}, errors.New("STOCK_CM_LINUX_REQUIRED")
}

func ReadStockCMInventory(context.Context) (ProfileInventory, error) {
	return ProfileInventory{}, errors.New("STOCK_CM_LINUX_REQUIRED")
}
