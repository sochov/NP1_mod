//go:build linux

package broker

import (
	"net"
	"syscall"
)

func peerAllowed(c net.Conn, uid uint32) bool {
	u, ok := c.(*net.UnixConn)
	if !ok {
		return false
	}
	raw, e := u.SyscallConn()
	if e != nil {
		return false
	}
	allowed := false
	e = raw.Control(func(fd uintptr) {
		cred, err := syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
		allowed = err == nil && cred.Uid == uid
	})
	return e == nil && allowed
}
