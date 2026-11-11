package nasbackup

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"time"
)

type Session interface {
	Remote
	Close() error
}

// A normal dual-camera clip can need several complete readbacks over a slow
// remote route. This is a transfer-wide bound, not a socket handshake timeout.
const TransferTimeout = 30 * time.Minute

type TransportOptions struct {
	DialContext func(context.Context, string, string) (net.Conn, error)
	RootCAs     *x509.CertPool
}

func (o TransportOptions) dial(ctx context.Context, network, address string) (net.Conn, error) {
	if o.DialContext != nil {
		return o.DialContext(ctx, network, address)
	}
	return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, address)
}
func Connect(ctx context.Context, cfg Config) (Session, error) {
	return ConnectWithTransport(ctx, cfg, TransportOptions{})
}
func ConnectWithTransport(ctx context.Context, cfg Config, options TransportOptions) (Session, error) {
	switch cfg.protocol {
	case "smb":
		return connectSMB(ctx, cfg, options)
	case "sftp":
		return connectSFTP(ctx, cfg, options)
	case "webdav":
		return connectWebDAV(ctx, cfg, options)
	default:
		return nil, errors.New("NAS_PROTOCOL_INVALID")
	}
}
