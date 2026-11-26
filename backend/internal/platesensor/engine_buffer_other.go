//go:build !linux

package platesensor

import (
	"os"
	"os/exec"
)

func prepareEngineFrameBuffer(_ *exec.Cmd) (*engineFrameBuffer, *os.File) {
	return nil, nil
}
