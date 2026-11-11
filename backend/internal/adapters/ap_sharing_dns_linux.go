//go:build linux

package adapters

import (
	"context"
	"net"
	"net/netip"
	"syscall"
)

func apDialBoundDNS(ctx context.Context, iface string, source netip.Addr, server netip.AddrPort) (*net.UDPConn, error) {
	dialer := net.Dialer{LocalAddr: net.UDPAddrFromAddrPort(netip.AddrPortFrom(source, 0)), Control: func(network, address string, raw syscall.RawConn) error {
		var bindError error
		e := raw.Control(func(fd uintptr) {
			bindError = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, iface)
		})
		if e != nil {
			return e
		}
		return bindError
	}}
	connection, e := dialer.DialContext(ctx, "udp4", server.String())
	if e != nil {
		return nil, e
	}
	return connection.(*net.UDPConn), nil
}
