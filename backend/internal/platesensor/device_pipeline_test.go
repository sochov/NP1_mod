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
	var header [24]byte
	if _, err := io.ReadFull(s.input, header[:]); err != nil {
		if errors.Is(err, io.EOF) {
			return [2]int64{}, false, errEndOfReplay
		}
		return [2]int64{}, false, err
	}
	if string(header[:4]) != protocolMagic || binary.LittleEndian.Uint32(header[4:8]) != FrameWidth ||
		binary.LittleEndian.Uint32(header[8:12]) != FrameHeight || binary.LittleEndian.Uint32(header[12:16]) != formatNV12 ||
		binary.LittleEndian.Uint64(header[16:24]) != s.seq+1 {
		return [2]int64{}, false, errors.New("invalid device replay frame")
	}
	if _, err := io.ReadFull(s.input, buf[:FrameBytes]); err != nil {
		return [2]int64{}, false, err
	}
	s.seq++
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
	var stream io.Reader = input
	if filepath.Ext(fixture) == ".gz" {
		compressed, err := gzip.NewReader(input)
		if err != nil {
			t.Fatal(err)
		}
		defer compressed.Close()
		stream = compressed
	}
	const interval = 125 * time.Millisecond
	source := &deviceReplaySource{input: bufio.NewReaderSize(stream, 64*1024), first: time.Unix(0, 0).UTC()}
	models := os.Getenv("NCC_PLATE_TEST_MODELS")
	enginePath := os.Getenv("NCC_PLATE_TEST_ENGINE")
	ctx, cancel := context.WithTimeout(context.Background(), 130*time.Second)
	defer cancel()
	cache := os.Getenv("NCC_PLATE_TEST_CACHE")
	engine, err := StartProcessEngine(ctx, enginePath, []string{
		"--detector", filepath.Join(models, "detector.tflite"), "--ocr", filepath.Join(models, "ocr.tflite"),
		"--config", filepath.Join(models, "plate-model.json"), "--min-confidence", ".60", "--threads", "2",
		"--gpu-cache", cache,
	}, 45*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	backend := engine.Info().Backend
	if cache != "" && !strings.Contains(backend, "gpu detector") {
		t.Fatal("the configured GPU detector was unavailable")
	}
	t.Log("engine ready for the combined check")
	state := t.TempDir()
	sensor := New(Config{StateDir: state, FPS: 8}, source, nil, nil)
	sensor.SetClock(func() time.Time { return source.first.Add(time.Duration(source.seq) * interval) })
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
	ingest()
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}
	day := plate.Day(source.first)
	records, err := store.ListAll(plate.Query{From: day, To: day})
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
	if matches != 1 {
		t.Fatalf("expected one matching record, got %d of %d records", matches, len(records))
	}
	t.Logf("combined OCR and sensor read-back passed on %d frames", source.seq)
}
