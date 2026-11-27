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

	"github.com/np1-control-center/ncc/internal/api"
	"github.com/np1-control-center/ncc/internal/config"
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
// The broker and record collector own position sampling and matching.
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
	forceCPU := false
	start := func(ctx context.Context) (platesensor.Engine, error) {
		minConf := platesensor.ReadMinConfidence(cfg.StateDir)
		args := []string{
			"--detector", filepath.Join(cfg.ModelDir, "detector.tflite"),
			"--ocr", filepath.Join(cfg.ModelDir, "ocr.tflite"),
			"--config", filepath.Join(cfg.ModelDir, "plate-model.json"),
			"--threads", strconv.Itoa(cfg.Threads),
			"--min-confidence", strconv.FormatFloat(minConf, 'f', 2, 64),
			"--ocr-queue", strconv.Itoa(int(envNumber("NCC_PLATE_OCR_QUEUE", 2, 1, 4))),
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
	sensor := platesensor.New(cfg, platesensor.NewFrontSource(), nil, start)
	go sensor.Run(ctx)
	return sensor.Serve(ctx)
}
