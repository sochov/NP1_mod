package platesensor

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

type deviceReplaySource struct {
	input io.Reader
	first time.Time
	seq   uint64
}

func (s *deviceReplaySource) Next(buf []byte) ([2]int64, bool, error) {
	if err := readDeviceReplayFrame(s.input, s.seq+1, buf); err != nil {
		return [2]int64{}, false, err
	}
	s.seq++
	return [2]int64{}, true, nil
}

func readDeviceReplayFrame(input io.Reader, seq uint64, buf []byte) error {
	var header [24]byte
	if _, err := io.ReadFull(input, header[:]); err != nil {
		if errors.Is(err, io.EOF) {
			return errEndOfReplay
		}
		return err
	}
	if string(header[:4]) != protocolMagic || binary.LittleEndian.Uint32(header[4:8]) != FrameWidth ||
		binary.LittleEndian.Uint32(header[8:12]) != FrameHeight || binary.LittleEndian.Uint32(header[12:16]) != formatNV12 ||
		binary.LittleEndian.Uint64(header[16:24]) != seq {
		return errors.New("invalid device replay frame")
	}
	_, err := io.ReadFull(input, buf[:FrameBytes])
	return err
}

const deviceReplayInterval = 125 * time.Millisecond
const deviceReplayFrameSize = 24 + FrameBytes

func startDeviceTestEngine(ctx context.Context, models, path, cache string) (Engine, error) {
	return StartProcessEngine(ctx, path, []string{
		"--detector", filepath.Join(models, "detector.tflite"), "--ocr", filepath.Join(models, "ocr.tflite"),
		"--config", filepath.Join(models, "plate-model.json"), "--min-confidence", ".60", "--threads", "2", "--gpu-cache", cache,
	}, 45*time.Second)
}

type measuredDeviceEngine struct {
	Engine
	frames, errors int
	first, last    time.Time
	end            time.Time
	maxGap         time.Duration
	requireGPU     bool
}

func (e *measuredDeviceEngine) Process(ctx context.Context, seq uint64, frame []byte, width, height int) (EngineReply, error) {
	started := time.Now()
	reply, err := e.Engine.Process(ctx, seq, frame, width, height)
	if err != nil || reply.Error != "" {
		if time.Now().Before(e.end) || reply.Error != "" {
			e.errors++
		}
		return reply, err
	}
	if e.requireGPU && !strings.Contains(reply.Backend, "gpu detector") {
		e.errors++
	}
	if !e.last.IsZero() {
		e.maxGap = max(e.maxGap, started.Sub(e.last))
	} else {
		e.first = started
	}
	e.last = started
	e.frames++
	return reply, nil
}

// TestDevicePlateLive reads the stock export while running the real sensor loop
// with temporary settings. Only aggregate timings leave the device.
func TestDevicePlateLive(t *testing.T) {
	value := os.Getenv("NCC_PLATE_TEST_LIVE_SECONDS")
	if value == "" {
		t.Skip("requires the NP1 live camera benchmark")
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 10 || seconds > 90 {
		t.Fatal("invalid live camera benchmark duration")
	}
	duration := time.Duration(seconds) * time.Second
	state := t.TempDir()
	settingsDir := filepath.Join(state, "plate")
	if err := os.Mkdir(settingsDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(`{"enabled":true,"minConfidence":0.60}`), 0600); err != nil {
		t.Fatal(err)
	}
	models, path, cache := os.Getenv("NCC_PLATE_TEST_MODELS"), os.Getenv("NCC_PLATE_TEST_ENGINE"), os.Getenv("NCC_PLATE_TEST_CACHE")
	startup, stopStartup := context.WithTimeout(context.Background(), 45*time.Second)
	engine, err := startDeviceTestEngine(startup, models, path, cache)
	stopStartup()
	if err != nil {
		t.Fatal(err)
	}
	measured := &measuredDeviceEngine{Engine: engine, requireGPU: cache != ""}
	started := false
	defer func() {
		if !started {
			engine.Close()
		}
	}()
	if cache != "" && !strings.Contains(engine.Info().Backend, "gpu detector") {
		t.Fatal("the configured GPU detector was unavailable")
	}
	t.Log("live camera engine ready")
	sensor := New(Config{StateDir: state, ModelDir: models, EnginePath: path, FPS: 8}, &ExportSource{Path: FrontExport}, nil,
		func(context.Context) (Engine, error) {
			if started {
				return nil, errors.New("live camera benchmark engine already stopped")
			}
			started = true
			return measured, nil
		})
	from := time.Now()
	measured.end = from.Add(duration)
	ctx, cancel := context.WithDeadline(context.Background(), measured.end)
	defer cancel()
	sensor.Run(ctx)
	maxGap := max(measured.maxGap, measured.first.Sub(from), measured.end.Sub(measured.last))
	if measured.frames < 2 || measured.errors != 0 || maxGap >= cameraFrameWait || sensor.status.LastFrameAt == nil {
		t.Fatal("live camera recognition stalled or an inference failed")
	}
	t.Logf("live camera frames: %d; fps: %.3f; max gap ms: %d",
		measured.frames, float64(measured.frames)/duration.Seconds(), maxGap.Milliseconds())
}

type pacedDeviceReplaySource struct {
	input     io.ReaderAt
	first     time.Time
	frames    int
	selected  []int
	seq       int
	complete  bool
	readError error
}

func (s *pacedDeviceReplaySource) Next(buf []byte) ([2]int64, bool, error) {
	if s.first.IsZero() {
		s.first = time.Now()
	}
	due := 1 + int(time.Since(s.first)/deviceReplayInterval)
	if due > s.frames {
		s.complete = true
		return [2]int64{}, false, errEndOfReplay
	}
	if due <= s.seq {
		return [2]int64{}, false, nil
	}
	packet := io.NewSectionReader(s.input, int64(due-1)*deviceReplayFrameSize, deviceReplayFrameSize)
	if err := readDeviceReplayFrame(packet, uint64(due), buf); err != nil {
		s.readError = err
		return [2]int64{}, false, err
	}
	s.seq = due
	s.selected = append(s.selected, due)
	return [2]int64{}, true, nil
}

// TestDevicePlatePipeline is run by the bounded ADB benchmark using recorded
// NV12 frames. The device's own recognition service remains in place.
func TestDevicePlatePipeline(t *testing.T) {
	fixture := os.Getenv("NCC_PLATE_TEST_FRAMES")
	if fixture == "" {
		t.Skip("requires the NP1 device benchmark fixture")
	}
	var expected struct {
		Number       string `json:"number"`
		MinSightings int    `json:"minSightings"`
		Frames       int    `json:"frames"`
	}
	expectation, err := os.ReadFile(os.Getenv("NCC_PLATE_TEST_EXPECTED"))
	if err != nil || json.Unmarshal(expectation, &expected) != nil || expected.Number == "" ||
		expected.MinSightings < 2 || expected.Frames < expected.MinSightings || expected.Frames > 64 {
		t.Fatal("invalid device replay reference")
	}
	input, err := os.Open(fixture)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	paced := os.Getenv("NCC_PLATE_TEST_PACED") == "1"
	if paced {
		info, err := input.Stat()
		if err != nil || filepath.Ext(fixture) != ".npf" || info.Size() != int64(expected.Frames)*deviceReplayFrameSize {
			t.Fatal("paced replay requires a complete uncompressed fixture")
		}
	}
	var stream io.Reader = input
	if filepath.Ext(fixture) == ".gz" {
		compressed, err := gzip.NewReader(input)
		if err != nil {
			t.Fatal(err)
		}
		defer compressed.Close()
		stream = compressed
	}
	source := &deviceReplaySource{input: bufio.NewReaderSize(stream, 64*1024), first: time.Unix(0, 0).UTC()}
	models := os.Getenv("NCC_PLATE_TEST_MODELS")
	enginePath := os.Getenv("NCC_PLATE_TEST_ENGINE")
	ctx, cancel := context.WithTimeout(context.Background(), 130*time.Second)
	defer cancel()
	cache := os.Getenv("NCC_PLATE_TEST_CACHE")
	engine, err := startDeviceTestEngine(ctx, models, enginePath, cache)
	if err != nil {
		t.Fatal(err)
	}
	var ownedEngine Engine = engine
	defer func() {
		if ownedEngine != nil {
			ownedEngine.Close()
		}
	}()
	backend := engine.Info().Backend
	if cache != "" && !strings.Contains(backend, "gpu detector") {
		t.Fatal("the configured GPU detector was unavailable")
	}
	t.Log("engine ready for the combined check")
	state := t.TempDir()
	var sensor *Sensor
	first := source.first
	if paced {
		settingsDir := filepath.Join(state, "plate")
		if err := os.Mkdir(settingsDir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(`{"enabled":true,"minConfidence":0.60}`), 0600); err != nil {
			t.Fatal(err)
		}
		camera := &pacedDeviceReplaySource{input: input, frames: expected.Frames}
		sensor = New(Config{StateDir: state, ModelDir: models, EnginePath: enginePath, FPS: 8}, camera, nil,
			func(context.Context) (Engine, error) {
				if ownedEngine == nil {
					return nil, errors.New("device replay engine already closed")
				}
				started := ownedEngine
				ownedEngine = nil
				return started, nil
			})
		sensor.Run(ctx)
		if camera.readError != nil {
			t.Fatal(camera.readError)
		}
		if ctx.Err() != nil || !camera.complete || len(camera.selected) == 0 || len(sensor.processed) != len(camera.selected) {
			t.Fatal("paced acquisition stopped before the reference duration")
		}
		first = camera.first
		t.Logf("paced camera offered %d frames; processed %d frames", expected.Frames, len(camera.selected))
		t.Logf("paced input frame indexes: %v", camera.selected)
	} else {
		sensor = New(Config{StateDir: state, FPS: 8}, source, nil, nil)
		sensor.SetClock(func() time.Time { return source.first.Add(time.Duration(source.seq) * deviceReplayInterval) })
	}
	store := plate.NewStore(filepath.Join(state, "plate"))
	settings := plate.DefaultSettings()
	ingest := func() {
		sensor.flushPending(ctx, true)
		for _, event := range sensor.queue {
			if _, err := store.Ingest(event, settings, nil); err != nil {
				t.Fatal(err)
			}
		}
		sensor.queue = sensor.queue[:0]
	}
	if !paced {
		buffer := make([]byte, FrameBytes+stampBytes)
		for i := range expected.Frames {
			if err := sensor.step(ctx, engine, buffer); err != nil {
				t.Fatal(err)
			}
			if source.seq != uint64(i+1) {
				t.Fatalf("replay did not consume frame %d", i+1)
			}
			ingest()
		}
		t.Logf("recognition processed %d frames", source.seq)
		if _, _, err := source.Next(buffer); !errors.Is(err, errEndOfReplay) {
			t.Fatal("replay did not end after the reference frame count")
		}
		sensor.finishTracks(sensor.clock(), true)
	}
	ingest()
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}
	records, err := store.ListAll(plate.Query{From: plate.Day(first), To: plate.Day(sensor.clock())})
	if err != nil {
		t.Fatal(err)
	}
	matches := 0
	for _, rec := range records {
		if rec.Reading.Number == expected.Number {
			matches++
			if rec.Sightings < expected.MinSightings || !rec.LastSeen.After(rec.FirstSeen) || rec.LastSeen.After(sensor.clock()) {
				t.Fatal("expected vehicle was missing its tracked interval")
			}
		}
	}
	if paced {
		t.Logf("paced expected record count: %d", matches)
		return
	}
	if matches != 1 {
		t.Fatalf("expected one matching record, got %d of %d records", matches, len(records))
	}
	t.Logf("combined OCR and sensor read-back passed on %d frames", source.seq)
}
