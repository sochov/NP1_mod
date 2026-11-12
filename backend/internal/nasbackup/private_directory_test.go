package nasbackup

import (
	"os"
	"testing"
)

// Set the fixture's intended private permissions independently of host umask.
func privateTempDir(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	return directory
}
