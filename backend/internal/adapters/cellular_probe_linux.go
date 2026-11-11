//go:build linux

package adapters

import (
	"context"
	"net"
	"net/netip"
	"syscall"
)

func cellularProbeDialTCP(ctx context.Context, iface string, source netip.Addr, target netip.AddrPort) (net.Conn, error) {
	dialer := net.Dialer{LocalAddr: net.TCPAddrFromAddrPort(netip.AddrPortFrom(source, 0)), Control: func(_, _ string, raw syscall.RawConn) error {
		var bindError error
		err := raw.Control(func(fd uintptr) {
			bindError = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, iface)
		})
		if err != nil {
			return err
		}
		return bindError
	}}
	return dialer.DialContext(ctx, "tcp4", target.String())
}
