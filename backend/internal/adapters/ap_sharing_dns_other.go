//go:build !linux

package adapters

import (
	"context"
	"errors"
	"net"
	"net/netip"
)

func apDialBoundDNS(context.Context, string, netip.Addr, netip.AddrPort) (*net.UDPConn, error) {
	return nil, errors.New("AP_CELLULAR_BOUND_DNS_UNSUPPORTED")
}
