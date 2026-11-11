//go:build !linux

package adapters

import (
	"fmt"
	"os"
)

func recordingMountToken(root string) (string, error) {
	st, e := os.Stat(root)
	if e != nil {
		return "", e
	}
	return fmt.Sprintf("%s/%v", root, st.Sys()), nil
}
