package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"

	"github.com/np1-control-center/ncc/internal/androidauto"
	"github.com/np1-control-center/ncc/internal/config"
)

// runAndroidAuto runs the Android Auto projection (np1-control-android-auto.service).
// It reads the stock front-camera export and the local NP1CC API, and encodes
// with the stock hardware encoder through GStreamer.
func runAndroidAuto(confPath string) error {
	c, e := config.Load(confPath)
	if e != nil {
		return e
	}
	if c.StateDir == "" {
		return errors.New("stateDir is not configured")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// NCC_AA_DATA_DIR, NCC_AA_API and NCC_AA_SOCKET let a bench run use scratch files.
	if addr := os.Getenv("NCC_AA_PPROF"); addr != "" {
		// Bench profiling only (loopback address expected).
		go func() { log.Println(http.ListenAndServe(addr, nil)) }()
	}
	svc := androidauto.New(androidauto.Config{StateDir: c.StateDir, DataDir: os.Getenv("NCC_AA_DATA_DIR"), APIBase: os.Getenv("NCC_AA_API"), Socket: os.Getenv("NCC_AA_SOCKET"), Logf: log.Printf})
	return svc.Run(ctx)
}
