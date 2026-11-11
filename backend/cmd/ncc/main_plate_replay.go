package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/signal"
	"path"
	"strconv"
	"syscall"
	"time"

	"github.com/np1-control-center/ncc/internal/api"
	"github.com/np1-control-center/ncc/internal/config"
	"github.com/np1-control-center/ncc/internal/platesensor"
)

// runPlateReplay is a PC verification mode: recorded dash-camera clips go
// through the real sensor, engine and server instead of the stock camera
// export. Inputs come from the environment:
//
//	NCC_PLATE_REPLAY_MANIFEST  replay manifest (clips, start times, GPS track, fps)
//	NCC_PLATE_SENSOR_SOCKET    socket the server reads (same variable as the server)
//	NCC_PLATE_ENGINE_CMD       JSON array: engine command prefix (e.g. wsl.exe ... plate-engine-host)
//	NCC_PLATE_ENGINE_MODELS    model directory as seen by the engine command
//	NCC_PLATE_MODEL_DIR        model directory as seen by this process (readiness check)
//	NCC_PLATE_REPLAY_RATE      frames processed per wall-clock second (default: manifest fps)
func runPlateReplay(confPath string) error {
	c, e := config.Load(confPath)
	if e != nil {
		return e
	}
	manifest, e := platesensor.LoadReplayManifest(os.Getenv("NCC_PLATE_REPLAY_MANIFEST"))
	if e != nil {
		return e
	}
	var command []string
	if e = json.Unmarshal([]byte(os.Getenv("NCC_PLATE_ENGINE_CMD")), &command); e != nil || len(command) == 0 {
		return errors.New("NCC_PLATE_ENGINE_CMD must be a JSON array")
	}
	models := os.Getenv("NCC_PLATE_ENGINE_MODELS")
	cfg := platesensor.Config{
		StateDir:   c.StateDir,
		ModelDir:   os.Getenv("NCC_PLATE_MODEL_DIR"),
		EnginePath: command[0],
		Socket:     api.PlateSensorSocketPath(),
		// Frames carry recording time at manifest.FPS; processing may run faster.
		FPS:     envNumber("NCC_PLATE_REPLAY_RATE", manifest.FPS, 0.5, 60),
		Threads: int(envNumber("NCC_PLATE_THREADS", 2, 1, 4)),
	}
	source := platesensor.NewReplaySource(manifest)
	start := func(ctx context.Context) (platesensor.Engine, error) {
		args := append(append([]string{}, command[1:]...),
			"--detector", path.Join(models, "detector.tflite"),
			"--ocr", path.Join(models, "ocr.tflite"),
			"--config", path.Join(models, "plate-model.json"),
			"--threads", strconv.Itoa(cfg.Threads))
		return platesensor.StartProcessEngine(ctx, command[0], args, 60*time.Second)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	sensor := platesensor.New(cfg, source, source.Locate, start)
	sensor.SetClock(source.Now)
	go sensor.Run(ctx)
	go func() {
		for ctx.Err() == nil && !source.Done {
			time.Sleep(time.Second)
		}
		if source.Done {
			log.Printf("replay finished; still serving detections and frames")
		}
	}()
	return sensor.Serve(ctx)
}
