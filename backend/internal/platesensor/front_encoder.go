package platesensor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/np1-control-center/ncc/internal/broker"
)

func streamFrontEncoder(ctx context.Context, dst io.Writer) error {
	client := broker.ClientWithHeaderTimeout(broker.PlateFrontSocket, 10*time.Second)
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://broker/front", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "video/h264" {
		return fmt.Errorf("front encoder unavailable: %d", resp.StatusCode)
	}
	_, err = io.Copy(dst, resp.Body)
	if err == nil {
		return io.EOF
	}
	return err
}
