package broker

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/np1-control-center/ncc/internal/adapters"
)

const PlateFrontSocket = "/run/np1-control/plate-front.sock"

func runPlateFront(ctx context.Context, brokerDir string) {
	for ctx.Err() == nil {
		err := servePlateFront(ctx, filepath.Join(brokerDir, filepath.Base(PlateFrontSocket)))
		if ctx.Err() != nil {
			return
		}
		log.Printf("plate front encoder socket: %v", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

func servePlateFront(ctx context.Context, path string) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return errors.New("plate encoder socket path occupied")
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer l.Close()
	if err := os.Chmod(path, 0600); err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /front", adapters.HandlePlateFront)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, MaxHeaderBytes: 4096, BaseContext: func(net.Listener) context.Context { return ctx }}
	stop := context.AfterFunc(ctx, func() { _ = srv.Close() })
	defer stop()
	return srv.Serve(listener{l, 0})
}
