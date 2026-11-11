//go:build linux

package modem

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

func captureNativeAPNHotspot(ctx context.Context) (nativeAPNHotspotBaseline, error) {
	return readNativeAPNHotspot(ctx, nativeAPNHotspotIO{os.ReadFile, nativeAPNCommand, nativeAPNHotspotHTTP, time.Now})
}

func nativeAPNHotspotHTTP(parent context.Context, address string) error {
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: time.Second}).DialContext, DisableKeepAlives: true, MaxResponseHeaderBytes: 8192}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, "GET", "http://"+net.JoinHostPort(address, "80")+"/", nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 8193))
	if err != nil || !nativeAPNHotspotHTTPResponse(response.StatusCode, data) {
		return errors.New("APN_HOTSPOT_HTTP_RESPONSE_UNVERIFIED")
	}
	return nil
}
