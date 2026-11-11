//go:build !linux

package modem

import (
	"context"
	"errors"
	"os"
)

func ReadGobiNetProfiles(context.Context) (ProfileRecovery, error) {
	return ProfileRecovery{}, errors.New("GobiNet profiles require Linux")
}
func ReadGobiNetProfileInventory(context.Context) (ProfileInventory, error) {
	return ProfileInventory{}, errors.New("GobiNet profile inventory requires Linux")
}
func requireProfileRecoveryRoot() error { return errors.New("profile recovery requires Linux root") }
func verifyProfileRecoveryOwner(os.FileInfo, os.FileMode) error {
	return errors.New("profile recovery requires Linux root")
}

// Public Save is unsupported here. This allows portable structural tests of
// the internal atomic publisher without claiming POSIX directory durability.
func syncProfileRecoveryDirectory(*os.Root) error { return nil }
