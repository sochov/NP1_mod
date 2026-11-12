package broker

import (
	"context"
	"net/http"
	"time"
)

// Keep JPEG conversion serial while allowing concurrent readers to wait briefly.
func snapshotHandler(snapshot func(string, string) ([]byte, error)) http.Handler {
	gate := make(chan struct{}, 1)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mode := r.URL.Query().Get("color")
		if mode != "auto" && mode != "source" {
			http.Error(w, "invalid color mode", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		select {
		case gate <- struct{}{}:
			defer func() { <-gate }()
		case <-ctx.Done():
			status := http.StatusTooManyRequests
			if r.Context().Err() == context.Canceled {
				status = http.StatusRequestTimeout
			}
			http.Error(w, "snapshot wait ended", status)
			return
		}
		if r.Context().Err() != nil {
			http.Error(w, "snapshot canceled", http.StatusRequestTimeout)
			return
		}
		b, err := snapshot(r.URL.Query().Get("source"), mode)
		if err != nil {
			http.Error(w, "camera unavailable or stale", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(b)
	})
}
