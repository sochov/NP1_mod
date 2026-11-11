//go:build !linux

package modem

import (
	"context"
	"errors"
)

func lockNativeAPNTransaction() (func(), error) { return nil, errors.New("APN_APPLY_REQUIRES_LINUX") }
func saveNativeAPNTransactionBackup(ProfileRecovery, nativeAPNNetworkBaseline) error {
	return errors.New("APN_BACKUP_REQUIRES_LINUX")
}
func RecoverNativeAPNManagementRoute(context.Context) (string, error) {
	return "unknown", errors.New("APN_RECOVERY_REQUIRES_LINUX")
}
