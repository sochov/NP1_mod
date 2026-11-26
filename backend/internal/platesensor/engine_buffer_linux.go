package platesensor

import (
	"os"
	"os/exec"

	"golang.org/x/sys/unix"
)

func prepareEngineFrameBuffer(cmd *exec.Cmd) (*engineFrameBuffer, *os.File) {
	fd, err := unix.MemfdCreate("np1-plate-frame", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, nil
	}
	file := os.NewFile(uintptr(fd), "np1-plate-frame")
	if err := unix.Ftruncate(fd, FrameBytes); err != nil {
		file.Close()
		return nil, nil
	}
	if _, err := unix.FcntlInt(file.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_SHRINK|unix.F_SEAL_GROW|unix.F_SEAL_SEAL); err != nil {
		file.Close()
		return nil, nil
	}
	pixels, err := unix.Mmap(fd, 0, FrameBytes, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		file.Close()
		return nil, nil
	}
	cmd.ExtraFiles = []*os.File{file}
	cmd.Env = append(cmd.Environ(), "NCC_PLATE_FRAME_FD=3")
	return &engineFrameBuffer{pixels: pixels, release: func() { _ = unix.Munmap(pixels) }}, file
}
