//go:build linux

package adapters

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestNativeUpdateSharedGuardBlocksDuringExclusiveUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update.lock")
	release, err := lockNativeUpdateAt(path, uint32(os.Geteuid()))
	if err != nil {
		t.Fatal(err)
	}
	release()
	if err := os.WriteFile(path, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if release, err = lockNativeUpdateAt(path, uint32(os.Geteuid())); err == nil {
		release()
		t.Fatal("native entered during updater exclusive lock")
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	release, err = lockNativeUpdateAt(path, uint32(os.Geteuid()))
	if err != nil {
		t.Fatal(err)
	}
	release()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "unchanged" {
		t.Fatal("guard read changed contents")
	}
}

func TestNativeUpdateSharedGuardRejectsUntrustedFileIdentity(t *testing.T) {
	for _, kind := range []string{"mode", "owner", "symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "update.lock")
			uid := uint32(os.Geteuid())
			if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "mode":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "owner":
				uid++
			case "symlink":
				target := path + ".original"
				if err := os.Rename(path, target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(path, path+".alias"); err != nil {
					t.Fatal(err)
				}
			}
			if release, err := lockNativeUpdateAt(path, uid); err == nil {
				release()
				t.Fatal("untrusted update lock accepted")
			}
		})
	}
}
