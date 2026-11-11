package broker

import (
	"context"
	"net"
	"net/http"
	"time"
)

func Client(socket string) *http.Client {
	return &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: 20 * time.Second, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}, MaxIdleConns: 4, MaxIdleConnsPerHost: 4}}
}
