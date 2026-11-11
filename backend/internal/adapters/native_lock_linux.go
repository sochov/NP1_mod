//go:build linux

package adapters

import (
	"errors"
	"os"
	"syscall"
)

func lockNativeControl() (func(), error) {
	f, e := os.OpenFile("/run/np1-control-native/control.lock", os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, errors.New("別の純正設定操作が進行中です")
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}
