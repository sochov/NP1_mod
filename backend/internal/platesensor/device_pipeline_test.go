package platesensor

import (
	"bufio"
	"bytes"
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
	"sync"
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
	input                 io.ReaderAt
	first                 time.Time
	frames                int
	compressed            []replayPacket
	selected              []int
	seq                   int
	complete              bool
	readError             error
	readTime, maxReadTime time.Duration
	prefetch              *pacedReplayPrefetch
}

type replayPacket struct{ offset, size int64 }

// The camera produces independently of the inference loop. Two buffers let a
// compressed fixture do the same without keeping the decoded file in RAM.
type pacedReplayPrefetch struct {
	mu          sync.Mutex
	frame       []byte
	seq         int
	err         error
	readTime    time.Duration
	maxReadTime time.Duration
	maxLate     time.Duration
	cancel      context.CancelFunc
	done        chan struct{}
}

func readPacedReplayIndex(input *os.File, frames int) ([]replayPacket, error) {
	info, err := input.Stat()
	if err != nil {
		return nil, err
	}
	if filepath.Ext(input.Name()) == ".npf" && info.Size() == int64(frames)*deviceReplayFrameSize {
		return nil, nil
	}
	if filepath.Ext(input.Name()) != ".npg" {
		return nil, errors.New("paced replay requires a complete fixture")
	}
	index := make([]byte, 8+4*frames)
	if _, err := input.ReadAt(index, 0); err != nil {
		return nil, err
	}
	if string(index[:4]) != "NPG1" || int(binary.LittleEndian.Uint32(index[4:8])) != frames {
		return nil, errors.New("invalid paced replay compression index")
	}
	packets, offset := make([]replayPacket, frames), int64(len(index))
	for i := range packets {
		size := int64(binary.LittleEndian.Uint32(index[8+4*i:]))
		if size == 0 || size > deviceReplayFrameSize+65536 || offset+size > info.Size() {
			return nil, errors.New("invalid compressed replay packet length")
		}
		packets[i] = replayPacket{offset, size}
		offset += size
	}
	if offset != info.Size() {
		return nil, errors.New("invalid compressed replay fixture size")
	}
	return packets, nil
}

func (s *pacedDeviceReplaySource) readFrame(seq int, buf []byte) error {
	if s.compressed == nil {
		packet := io.NewSectionReader(s.input, int64(seq-1)*deviceReplayFrameSize, deviceReplayFrameSize)
		return readDeviceReplayFrame(packet, uint64(seq), buf)
	}
	p := s.compressed[seq-1]
	stream, err := gzip.NewReader(io.NewSectionReader(s.input, p.offset, p.size))
	if err != nil {
		return err
	}
	defer stream.Close()
	if err := readDeviceReplayFrame(stream, uint64(seq), buf); err != nil {
		return err
	}
	var extra [1]byte
	if n, err := io.ReadFull(stream, extra[:]); n != 0 || !errors.Is(err, io.EOF) {
		return errors.New("invalid compressed replay frame")
	}
	return nil
}

func TestIndexedPacedReplayReadsSelectedPacket(t *testing.T) {
	index := []byte("NPG1")
	index = binary.LittleEndian.AppendUint32(index, 2)
	var members bytes.Buffer
	for i := uint64(1); i <= 2; i++ {
		packet := make([]byte, deviceReplayFrameSize)
		copy(packet, protocolMagic)
		binary.LittleEndian.PutUint32(packet[4:], FrameWidth)
		binary.LittleEndian.PutUint32(packet[8:], FrameHeight)
		binary.LittleEndian.PutUint32(packet[12:], formatNV12)
		binary.LittleEndian.PutUint64(packet[16:], i)
		packet[24], packet[len(packet)-1] = byte(i), byte(i)
		start := members.Len()
		writer := gzip.NewWriter(&members)
		if _, err := writer.Write(packet); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		index = binary.LittleEndian.AppendUint32(index, uint32(members.Len()-start))
	}
	path := filepath.Join(t.TempDir(), "frames.npg")
	if err := os.WriteFile(path, append(index, members.Bytes()...), 0600); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	packets, err := readPacedReplayIndex(input, 2)
	if err != nil {
		t.Fatal(err)
	}
	source := &pacedDeviceReplaySource{input: input, frames: 2, compressed: packets}
	buf := make([]byte, FrameBytes)
	if err := source.readFrame(2, buf); err != nil || buf[0] != 2 || buf[len(buf)-1] != 2 {
		t.Fatalf("could not read the second indexed frame: %v", err)
	}
	if _, err := readPacedReplayIndex(input, 1); err == nil {
		t.Fatal("a mismatched packet count was accepted")
	}
	if err := source.startPrefetch(); err != nil {
		t.Fatal(err)
	}
	defer source.stopPrefetch()
	p := source.prefetch
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
		t.Fatal("the second frame was not offered")
	}
	if p.err != nil || p.seq != 2 || p.frame[0] != 2 {
		t.Fatalf("invalid prefetched frame: %v", p.err)
	}
}

func (s *pacedDeviceReplaySource) startPrefetch() error {
	front := make([]byte, FrameBytes)
	started := time.Now()
	if err := s.readFrame(1, front); err != nil {
		return err
	}
	readTime := time.Since(started)
	ctx, cancel := context.WithCancel(context.Background())
	p := &pacedReplayPrefetch{frame: front, seq: 1, readTime: readTime, maxReadTime: readTime, cancel: cancel, done: make(chan struct{})}
	s.prefetch, s.first = p, time.Now()
	go func() {
		defer close(p.done)
		back := make([]byte, FrameBytes)
		for seq := 2; seq <= s.frames && ctx.Err() == nil; seq++ {
			started := time.Now()
			err := s.readFrame(seq, back)
			elapsed := time.Since(started)
			if err != nil {
				p.mu.Lock()
				p.err = err
				p.mu.Unlock()
				return
			}
			due := s.first.Add(time.Duration(seq-1) * deviceReplayInterval)
			timer := time.NewTimer(time.Until(due))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			p.mu.Lock()
			p.frame, back = back, p.frame
			p.seq = seq
			p.readTime += elapsed
			p.maxReadTime = max(p.maxReadTime, elapsed)
			p.maxLate = max(p.maxLate, time.Since(due))
			p.mu.Unlock()
		}
	}()
	return nil
}

func (s *pacedDeviceReplaySource) stopPrefetch() {
	if p := s.prefetch; p != nil {
		p.cancel()
		<-p.done
	}
}

func (s *pacedDeviceReplaySource) Next(buf []byte) ([2]int64, bool, error) {
	if s.first.IsZero() {
		if s.compressed == nil {
			s.first = time.Now()
		} else if err := s.startPrefetch(); err != nil {
			s.readError = err
			return [2]int64{}, false, err
		}
	}
	due := 1 + int(time.Since(s.first)/deviceReplayInterval)
	if due > s.frames {
		s.complete = true
		return [2]int64{}, false, errEndOfReplay
	}
	if due <= s.seq {
		return [2]int64{}, false, nil
	}
	if p := s.prefetch; p != nil {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.err != nil {
			s.readError = p.err
			return [2]int64{}, false, p.err
		}
		if p.seq <= s.seq {
			return [2]int64{}, false, nil
		}
		copy(buf[:FrameBytes], p.frame)
		s.seq = p.seq
		s.selected = append(s.selected, s.seq)
		return [2]int64{}, true, nil
	}
	started := time.Now()
	if err := s.readFrame(due, buf); err != nil {
		s.readError = err
		return [2]int64{}, false, err
	}
	elapsed := time.Since(started)
	s.readTime += elapsed
	s.maxReadTime = max(s.maxReadTime, elapsed)
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
	var compressedPackets []replayPacket
	if paced {
		compressedPackets, err = readPacedReplayIndex(input, expected.Frames)
		if err != nil {
			t.Fatal(err)
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
		camera := &pacedDeviceReplaySource{input: input, frames: expected.Frames, compressed: compressedPackets}
		defer camera.stopPrefetch()
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
		camera.stopPrefetch()
		if camera.readError != nil {
			t.Fatal(camera.readError)
		}
		if ctx.Err() != nil || !camera.complete || len(camera.selected) == 0 || len(sensor.processed) != len(camera.selected) {
			t.Fatal("paced acquisition stopped before the reference duration")
		}
		first = camera.first
		t.Logf("paced camera offered %d frames; processed %d frames", expected.Frames, len(camera.selected))
		t.Logf("paced input frame indexes: %v", camera.selected)
		if p := camera.prefetch; p != nil {
			if p.err != nil || p.seq != expected.Frames || p.maxLate >= deviceReplayInterval/2 {
				t.Fatalf("compressed source missed the planned input cadence: offered %d; max delay ms: %d",
					p.seq, p.maxLate.Milliseconds())
			}
			t.Logf("paced frame read mean us: %d; max us: %d; max offer delay us: %d",
				(p.readTime / time.Duration(p.seq)).Microseconds(), p.maxReadTime.Microseconds(), p.maxLate.Microseconds())
		} else {
			t.Logf("paced frame read mean us: %d; max us: %d",
				(camera.readTime / time.Duration(len(camera.selected))).Microseconds(), camera.maxReadTime.Microseconds())
		}
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
