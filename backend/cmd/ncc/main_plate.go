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
	plateModelDir      = "/overlay/owner-apps/np1-control/plate/models"
	plateEngine        = "/run/np1-control-plate/plate-engine"
	plateEngineStartup = 45 * time.Second
)

func envNumber(name string, fallback, lo, hi float64) float64 {
	v, e := strconv.ParseFloat(os.Getenv(name), 64)
	if e != nil || v < lo || v > hi {
		return fallback
	}
	return v
}

// runPlateSensor runs the plate-recognition sensor (np1-control-plate.service).
// It reads only the stock camera export, the stock per-second position record and NMEA sidecars.
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
		FPS:        envNumber("NCC_PLATE_FPS", 4, 0.5, 10),
		Threads:    int(envNumber("NCC_PLATE_THREADS", 2, 1, 4)),
	}
	// The position of the detection's own second: the stock per-second record, or an NMEA fix
	// when one is already on the SD card. Records left without one are filled in later by the
	// server from the recorder's GNSS log.
	locate := func(ctx context.Context, at time.Time) *plate.Location {
		fix := adapters.PlatePosition(ctx, at)
		if fix == nil {
			return nil
		}
		return &plate.Location{At: fix.At, Lat: fix.Lat, Lon: fix.Lon, AccuracyM: fix.AccuracyM}
	}
	forceCPU := false
	start := func(ctx context.Context) (platesensor.Engine, error) {
		minConf := platesensor.ReadMinConfidence(cfg.StateDir)
		args := []string{
			"--detector", filepath.Join(cfg.ModelDir, "detector.tflite"),
			"--ocr", filepath.Join(cfg.ModelDir, "ocr.tflite"),
			"--config", filepath.Join(cfg.ModelDir, "plate-model.json"),
			"--threads", strconv.Itoa(cfg.Threads),
			"--min-confidence", strconv.FormatFloat(minConf, 'f', 2, 64),
		}
		if forceCPU {
			return platesensor.StartProcessEngine(ctx, cfg.EnginePath, append(args, "--backend", "cpu"), plateEngineStartup)
		}
		engine, err := platesensor.StartProcessEngine(ctx, cfg.EnginePath, args, plateEngineStartup)
		if err == nil || ctx.Err() != nil {
			return engine, err
		}
		// A stale driver program or a wedged GPU must not stop recognition.
		forceCPU = true
		engine, retryErr := platesensor.StartProcessEngine(ctx, cfg.EnginePath, append(args, "--backend", "cpu"), plateEngineStartup)
		if retryErr != nil {
			return nil, errors.Join(err, retryErr)
		}
		return engine, nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go adapters.TrackLivePosition(ctx)
	sensor := platesensor.New(cfg, &platesensor.ExportSource{Path: platesensor.FrontExport}, locate, start)
	go sensor.Run(ctx)
	return sensor.Serve(ctx)
}
