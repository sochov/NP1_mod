//go:build !linux

package nasbackup

import (
	"errors"
	"os"
)

// Host fixtures use the platform's ordinary file permissions. Production runs
// on Linux and enforces 0600/0700, owner UID, hardlink and flock checks.
func privateRegular(info os.FileInfo) error {
	if info == nil || !info.Mode().IsRegular() {
		return errors.New("NAS_PRIVATE_FILE_UNSAFE")
	}
	return nil
}
func privateDirectory(os.FileInfo) error { return nil }
func lockState(string) (func(), error)   { return func() {}, nil }
