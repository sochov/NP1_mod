package adapters

import (
	"os"
	"testing"
)

// testing.T.TempDir does not promise the 0700 mode required by private stores.
func privateTempDir(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	return directory
}
