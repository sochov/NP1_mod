//go:build linux

package adapters

import (
	"errors"
	"os"
	"syscall"
)

func lockNativeUpdateAt(guardPath string, requiredUID uint32) (func(), error) {
	// RuntimeDirectory may be replaced across a worker restart. The updater's
	// lock lives outside it, so the new worker cannot enter vendor code halfway
	// through an update. Reading/flocking an existing file requires no RW mount.
	var update *os.File
	guardInfo, e := os.Lstat(guardPath)
	if e == nil {
		owner, ok := guardInfo.Sys().(*syscall.Stat_t)
		if !ok || !guardInfo.Mode().IsRegular() || guardInfo.Mode().Perm() != 0600 || owner.Uid != requiredUID || owner.Nlink != 1 {
			return nil, errors.New("純正設定サービスの更新ロックを確認できません")
		}
		update, e = os.OpenFile(guardPath, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if e != nil {
			return nil, e
		}
		opened, err := update.Stat()
		if err != nil || !os.SameFile(guardInfo, opened) {
			update.Close()
			return nil, errors.New("純正設定サービスの更新ロックが変更されました")
		}
		if e = syscall.Flock(int(update.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); e != nil {
			update.Close()
			return nil, errors.New("純正設定サービスを更新中です")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	releaseUpdate := func() {
		if update != nil {
			syscall.Flock(int(update.Fd()), syscall.LOCK_UN)
			update.Close()
		}
	}
	return releaseUpdate, nil
}

func lockNativeControl() (func(), error) {
	releaseUpdate, e := lockNativeUpdateAt("/run/np1-native-control-update.lock", 0)
	if e != nil {
		return nil, e
	}
	f, e := os.OpenFile("/run/np1-control-native/control.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		releaseUpdate()
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		releaseUpdate()
		return nil, errors.New("別の純正設定操作が進行中です")
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close(); releaseUpdate() }, nil
}

// The broker can coordinate with the native worker without a writable mount of
// that worker's runtime directory. Linux flock also supports EX on a read FD.
func lockNativeControlExisting() (func(), error) {
	releaseUpdate, err := lockNativeUpdateAt("/run/np1-native-control-update.lock", 0)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile("/run/np1-control-native/control.lock", os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		releaseUpdate()
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		releaseUpdate()
		return nil, err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || owner.Uid != 0 || owner.Nlink != 1 {
		file.Close()
		releaseUpdate()
		return nil, errors.New("native control lock identity invalid")
	}
	if syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		file.Close()
		releaseUpdate()
		return nil, errors.New("native control operation in progress")
	}
	return func() { syscall.Flock(int(file.Fd()), syscall.LOCK_UN); file.Close(); releaseUpdate() }, nil
}

func InitializeNativeControlLock() error {
	file, err := os.OpenFile("/run/np1-control-native/control.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || owner.Uid != 0 || owner.Nlink != 1 {
		return errors.New("native control lock identity invalid")
	}
	return nil
}
