//go:build !linux

package led

import "os"

func screenFilePermissions(st os.FileInfo) error { return nil }
