//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly && !windows

package adapters

import (
	"errors"
	"os"
)

func apnEditorNoFollow() int               { return 0 }
func apnEditorOwner(os.FileInfo) error     { return errors.New("APN_EDITOR_LOCK_PLATFORM_UNSUPPORTED") }
func apnEditorOSLock(*os.File, bool) error { return errors.New("APN_EDITOR_LOCK_PLATFORM_UNSUPPORTED") }
func apnEditorOSUnlock(*os.File)           {}
