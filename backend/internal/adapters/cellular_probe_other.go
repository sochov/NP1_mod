//go:build !linux

package adapters

import (
	"context"
	"errors"
	"net"
	"net/netip"
)

func cellularProbeDialTCP(context.Context, string, netip.Addr, netip.AddrPort) (net.Conn, error) {
	return nil, errors.New("CELLULAR_PROBE_LINUX_REQUIRED")
}
