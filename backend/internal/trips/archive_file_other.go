//go:build !linux

package trips

import "os"

// POSIX owner/mode and directory fsync guarantees are verified on NP1 Linux.
func archiveFilePrivate(os.FileInfo) bool { return true }
func syncArchiveDirectory(string) error   { return nil }
