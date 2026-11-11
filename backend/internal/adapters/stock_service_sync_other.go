//go:build !linux

package adapters

import (
	"errors"
	"os"
)

func stockSyncIdentityFor(os.FileInfo) (stockSyncIdentity, error) {
	return stockSyncIdentity{}, errors.New("stock singleton control requires Linux")
}

// Developer fixtures only: non-Linux execution refuses the identity above.
func stockSyncPrivateMode(os.FileInfo) bool { return true }
