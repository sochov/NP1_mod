package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/np1-control-center/ncc/internal/adapters"
	"github.com/np1-control-center/ncc/internal/api"
	"github.com/np1-control-center/ncc/internal/config"
	"github.com/np1-control-center/ncc/internal/plate"
	"github.com/np1-control-center/ncc/internal/platesensor"
)

const (
	plateModelDir = "/overlay/owner-apps/np1-control/plate/models"
	plateEngine   = "/run/np1-control-plate/plate-engine"
)

func envNumber(name string, fallback, lo, hi float64) float64 {
	v, e := strconv.ParseFloat(os.Getenv(name), 64)
	if e != nil || v < lo || v > hi {
		return fallback
	}
	return v
}

// runPlateSensor runs the plate-recognition sensor (np1-control-plate.service).
// It reads only the stock camera export and NMEA sidecars.
func runPlateSensor(confPath string) error {
	c, e := config.Load(confPath)
	if e != nil {
		return e
	}
	if c.StateDir == "" {
		return errors.New("stateDir is not configured")
	}
	cfg := platesensor.Config{
		StateDir:   c.StateDir,
		ModelDir:   plateModelDir,
		EnginePath: plateEngine,
		Socket:     api.PlateSensorSocket,
		FPS:        envNumber("NCC_PLATE_FPS", 2, 0.5, 10), // device: detector ~196 ms + reader ~19 ms/plate on 2 A76 threads
		Threads:    int(envNumber("NCC_PLATE_THREADS", 2, 1, 4)),
	}
	locate := func(ctx context.Context, at time.Time) *plate.Location {
		fix, e := adapters.LatestGNSS(ctx, at)
		if e != nil || fix == nil {
			return nil
		}
		return &plate.Location{At: fix.At, Lat: fix.Lat, Lon: fix.Lon, AccuracyM: fix.AccuracyM}
	}
	start := func(ctx context.Context) (platesensor.Engine, error) {
		args := []string{
			"--detector", filepath.Join(cfg.ModelDir, "detector.tflite"),
			"--ocr", filepath.Join(cfg.ModelDir, "ocr.tflite"),
			"--config", filepath.Join(cfg.ModelDir, "plate-model.json"),
			"--threads", strconv.Itoa(cfg.Threads),
		}
		return platesensor.StartProcessEngine(ctx, cfg.EnginePath, args, 30*time.Second)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	sensor := platesensor.New(cfg, &platesensor.ExportSource{Path: platesensor.FrontExport}, locate, start)
	go sensor.Run(ctx)
	return sensor.Serve(ctx)
}
