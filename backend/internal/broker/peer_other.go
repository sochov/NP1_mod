//go:build !linux

package broker

import "net"

func peerAllowed(c net.Conn, uid uint32) bool { return false }
