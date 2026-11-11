//go:build !linux

package otapolicy

import (
	"os"
	"sync"
)

var hostLock sync.Mutex

func identify(info os.FileInfo, out *snapshot)      { out.Mode = 0644 }
func setOwner(file *os.File, uid, gid uint32) error { return nil }
func lockPolicy(path string) (func(), error)        { hostLock.Lock(); return hostLock.Unlock, nil }
