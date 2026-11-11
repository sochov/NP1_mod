package adapters

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

var errAPNApplyBusy = errors.New("APPLY_IN_PROGRESS")

func apnEditorDirectory(directory string, create bool) error {
	for current := filepath.Clean(directory); ; current = filepath.Dir(current) {
		info, e := os.Lstat(current)
		if e != nil && !errors.Is(e, os.ErrNotExist) || e == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return errors.New("APN_PRIVATE_DIRECTORY_INVALID")
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	if create {
		if e := os.MkdirAll(directory, 0700); e != nil {
			return errors.New("APN_CONFIG_WRITE_FAILED")
		}
	}
	info, e := os.Lstat(directory)
	if errors.Is(e, os.ErrNotExist) && !create {
		return nil
	}
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("APN_PRIVATE_DIRECTORY_INVALID")
	}
	if runtime.GOOS == "linux" && info.Mode().Perm() != 0700 {
		return errors.New("APN_PRIVATE_DIRECTORY_INVALID")
	}
	return apnEditorOwner(info)
}
func apnEditorLock(directory string, exclusive, create bool) (func(), error) {
	if e := apnEditorDirectory(directory, create); e != nil {
		return nil, e
	}
	path := filepath.Join(directory, "config.lock")
	info, e := os.Lstat(path)
	if errors.Is(e, os.ErrNotExist) && !create {
		return func() {}, nil
	}
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return nil, errors.New("APN_EDITOR_LOCK_INVALID")
	}
	if info != nil {
		if e := apnEditorLockInfo(info); e != nil {
			return nil, e
		}
	}
	root, e := os.OpenRoot(directory)
	if e != nil {
		return nil, errors.New("APN_EDITOR_LOCK_OPEN_FAILED")
	}
	defer root.Close()
	flags := os.O_RDONLY
	if create || exclusive {
		flags = os.O_RDWR
	}
	if create {
		flags |= os.O_CREATE
	}
	flags |= apnEditorNoFollow()
	file, e := root.OpenFile("config.lock", flags, 0600)
	if e != nil {
		return nil, errors.New("APN_EDITOR_LOCK_OPEN_FAILED")
	}
	opened, e := file.Stat()
	if e != nil || apnEditorLockInfo(opened) != nil {
		file.Close()
		return nil, errors.New("APN_EDITOR_LOCK_INVALID")
	}
	current, e := root.Lstat("config.lock")
	if e != nil || !os.SameFile(opened, current) || apnEditorLockInfo(current) != nil {
		file.Close()
		return nil, errors.New("APN_EDITOR_LOCK_CHANGED")
	}
	if e = apnEditorOSLock(file, exclusive); e != nil {
		file.Close()
		return nil, e
	}
	return func() { apnEditorOSUnlock(file); file.Close() }, nil
}
func apnEditorLockInfo(info os.FileInfo) error {
	if info == nil || !info.Mode().IsRegular() || info.Mode()&(os.ModeSymlink|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || info.Size() != 0 {
		return errors.New("APN_EDITOR_LOCK_INVALID")
	}
	if runtime.GOOS == "linux" && info.Mode().Perm() != 0600 {
		return errors.New("APN_EDITOR_LOCK_PERMISSIONS")
	}
	return apnEditorOwner(info)
}
func (s *APNStore) mutationLock() (func(), error) {
	if !s.mu.TryLock() {
		return nil, errAPNApplyBusy
	}
	unlock, e := apnEditorLock(s.directory, true, true)
	if e != nil {
		s.mu.Unlock()
		return nil, e
	}
	return func() { unlock(); s.mu.Unlock() }, nil
}
