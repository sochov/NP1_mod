package platesensor

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

type fakeSource struct {
	n      int
	paused bool
	err    error
}

type delayedExport struct {
	first, missed, decoded time.Time
	cancel                 context.CancelFunc
}

func (s *delayedExport) Next(_ []byte) ([2]int64, bool, error) {
	now := time.Now()
	if s.first.IsZero() {
		s.first = now
		return [2]int64{1, 0}, true, nil
	}
	if s.missed.IsZero() {
		s.missed = now
	}
	if now.Sub(s.missed) < 30*time.Millisecond {
		return [2]int64{1, 0}, false, nil
	}
	s.decoded = now
	s.cancel()
	return [2]int64{2, 0}, true, nil
}

func (f *fakeSource) Next(buf []byte) ([2]int64, bool, error) {
	if f.paused || f.err != nil {
		return [2]int64{}, false, f.err
	}
	f.n++
	for i := range buf[:FrameBytes] {
		buf[i] = byte(16 + f.n)
	}
	return [2]int64{int64(f.n), 0}, true, nil
}

type fakeEngine struct {
	plates  []EnginePlate
	backend string
	fail    bool
	calls   int
}

func (f *fakeEngine) Info() EngineInfo {
	return EngineInfo{Ready: true, Engine: "fake", Backend: "test"}
}
func (f *fakeEngine) Process(_ context.Context, seq uint64, _ []byte, _, _ int) (EngineReply, error) {
	f.calls++
	if f.fail {
		return EngineReply{}, errors.New("boom")
	}
	return EngineReply{Seq: seq, Plates: f.plates, Backend: f.backend}, nil
}
func (f *fakeEngine) Close() error { return nil }

func setup(t *testing.T, enabled bool, withModels bool) Config {
	dir := t.TempDir()
	cfg := Config{StateDir: filepath.Join(dir, "state"), ModelDir: filepath.Join(dir, "models"), EnginePath: filepath.Join(dir, "engine"), Socket: filepath.Join(dir, "s.sock"), FPS: 50}
	os.MkdirAll(filepath.Join(cfg.StateDir, "plate"), 0700)
	os.WriteFile(filepath.Join(cfg.StateDir, "plate", "settings.json"), []byte(fmt.Sprintf(`{"enabled":%v}`, enabled)), 0600)
	if withModels {
		os.MkdirAll(cfg.ModelDir, 0700)
		for _, n := range ModelFiles {
			os.WriteFile(filepath.Join(cfg.ModelDir, n), []byte("x"), 0600)
		}
		os.WriteFile(cfg.EnginePath, []byte("x"), 0700)
	}
	return cfg
}

func TestReadinessNeverFakesCapability(t *testing.T) {
	s := New(setup(t, false, true), &fakeSource{}, nil, nil)
	if st, _ := s.readiness(); st != "idle" {
		t.Fatalf("disabled = %s", st)
	}
	s = New(setup(t, true, false), &fakeSource{}, nil, nil)
	if st, _ := s.readiness(); st != "engine-missing" {
		t.Fatalf("no engine = %s", st)
	}
	cfg := setup(t, true, true)
	os.Remove(filepath.Join(cfg.ModelDir, "ocr.tflite"))
	s = New(cfg, &fakeSource{}, nil, nil)
	if st, reason := s.readiness(); st != "model-missing" || reason == "" {
		t.Fatalf("no model = %s %s", st, reason)
	}
}

func TestRunRetriesAnUnchangedExportWithinTheInferenceInterval(t *testing.T) {
	cfg := setup(t, true, true)
	cfg.FPS = 4
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	source := &delayedExport{cancel: cancel}
	engine := &fakeEngine{}
	locationPolls := 0
	s := New(cfg, source, func(context.Context, time.Time) *plate.Location {
		locationPolls++
		return nil
	}, func(context.Context) (Engine, error) { return engine, nil })
	s.pending = append(s.pending, pending{since: time.Now()})
	s.Run(ctx)
	if engine.calls != 2 || source.decoded.IsZero() || s.frameSeq != 2 {
		t.Fatalf("expected just two fresh frames, decoded=%v calls=%d", source.decoded, engine.calls)
	}
	if gap := source.missed.Sub(source.first); gap < 230*time.Millisecond {
		t.Fatalf("polled a new frame ahead of the 4 fps schedule after %s", gap)
	}
	if wait := source.decoded.Sub(source.missed); wait >= 200*time.Millisecond {
		t.Fatalf("the next frame arrived during the idle poll but was delayed by %s", wait)
	}
	if locationPolls != 3 {
		t.Fatalf("early camera polls changed the GNSS lookup rate: got %d lookups", locationPolls)
	}
}

func TestStalledCameraClearsFPSAndRecoversWithoutRestart(t *testing.T) {
	now := time.Now()
	source := &fakeSource{}
	s := New(Config{}, source, nil, nil)
	s.SetClock(func() time.Time { return now })
	engine := &fakeEngine{}
	buf := make([]byte, FrameBytes+stampBytes)
	step := func(delta time.Duration) {
		t.Helper()
		now = now.Add(delta)
		if err := s.step(context.Background(), engine, buf); err != nil {
			t.Fatal(err)
		}
	}
	step(0)
	step(250 * time.Millisecond)
	last := *s.status.LastFrameAt
	source.paused = true
	step(time.Second)
	if s.status.State != "running" || s.status.FPS != 4 {
		t.Fatalf("status during a short gap = %+v", s.status)
	}
	step(cameraFrameWait)
	if s.status.State != "camera-waiting" || s.status.Reason == "" || s.status.FPS != 0 ||
		!s.status.LastFrameAt.Equal(last) || engine.calls != 2 {
		t.Fatalf("status after a stalled camera = %+v, calls=%d", s.status, engine.calls)
	}
	source.paused = false
	step(250 * time.Millisecond)
	if s.status.State != "running" || s.status.Reason != "" || s.status.FPS != 0 || engine.calls != 3 {
		t.Fatalf("status after camera resumes = %+v, calls=%d", s.status, engine.calls)
	}
	step(250 * time.Millisecond)
	if s.status.FPS != 4 {
		t.Fatalf("FPS after camera resumes = %v", s.status.FPS)
	}
	source.err = os.ErrNotExist
	step(250 * time.Millisecond)
	if s.status.State != "camera-waiting" || s.status.FPS != 0 || engine.calls != 4 {
		t.Fatalf("status after camera read error = %+v, calls=%d", s.status, engine.calls)
	}
}

func TestExportDoesNotProcessStaleOrDuplicateCameraFrames(t *testing.T) {
	source := &ExportSource{Path: filepath.Join(t.TempDir(), "value")}
	raw := make([]byte, FrameBytes+stampBytes)
	raw[0] = 96
	binary.LittleEndian.PutUint64(raw[FrameBytes:], 1)
	if err := os.WriteFile(source.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	setModified := func(at time.Time) {
		t.Helper()
		if err := os.Chtimes(source.Path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	buf := make([]byte, len(raw))
	check := func(want bool) {
		t.Helper()
		_, got, err := source.Next(buf)
		if err != nil || got != want {
			t.Fatalf("new camera frame=%v err=%v, want %v", got, err, want)
		}
	}
	setModified(time.Now().Add(-2 * cameraFrameWait))
	check(false)
	setModified(time.Now())
	check(true)
	if buf[0] != 96 {
		t.Fatalf("camera image was not copied: %d", buf[0])
	}
	buf[0] = 0
	check(false)
	if buf[0] != 0 {
		t.Fatalf("copied a duplicate camera image: %d", buf[0])
	}

	// Older producers that cannot supply a stamp still expose the file mtime.
	binary.LittleEndian.PutUint64(raw[FrameBytes:], 0)
	if err := os.WriteFile(source.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	setModified(time.Now().Add(time.Second))
	check(true)
	check(false)
}

func TestStepQueuesDetectionsWithLocationAndFrames(t *testing.T) {
	now := time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)
	located := false
	locate := func(_ context.Context, at time.Time) *plate.Location {
		if !located {
			return nil
		}
		return &plate.Location{At: at, Lat: 35, Lon: 139}
	}
	s := New(setup(t, true, true), &fakeSource{}, locate, nil)
	s.clock = func() time.Time { return now }
	engine := &fakeEngine{plates: []EnginePlate{{Box: [4]int{1, 2, 3, 4}, Score: .9, Reading: plate.Reading{Number: "1234", Confidence: .9}}}}
	buf := make([]byte, FrameBytes+stampBytes)
	if e := s.step(context.Background(), engine, buf); e != nil {
		t.Fatal(e)
	}
	now = now.Add(trackRejoinGap + trackReadGrace)
	s.finishTracks(now, false)
	s.flushPending(context.Background(), false)
	if len(s.queue) != 0 {
		t.Fatal("published before the GNSS wait without a fix")
	}
	located = true
	s.flushPending(context.Background(), false)
	if len(s.queue) != 1 || s.queue[0].Location == nil || s.queue[0].Seq != 1 || s.queue[0].Frame != "1" {
		t.Fatalf("queue = %+v", s.queue)
	}
	// Without any fix, detections are still published after the wait.
	located = false
	s.step(context.Background(), engine, buf)
	now = now.Add(locateWait)
	s.finishTracks(now, false)
	s.flushPending(context.Background(), false)
	if len(s.queue) != 2 || s.queue[1].Location != nil {
		t.Fatalf("no-fix publish = %+v", s.queue)
	}

	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	var batch struct {
		Instance   string            `json:"instance"`
		Next       uint64            `json:"next"`
		Detections []plate.Detection `json:"detections"`
	}
	res, _ := http.Get(srv.URL + "/detections?after=1")
	json.NewDecoder(res.Body).Decode(&batch)
	res.Body.Close()
	if batch.Next != 2 || len(batch.Detections) != 1 || batch.Instance == "" {
		t.Fatalf("batch = %+v", batch)
	}
	res, _ = http.Get(srv.URL + "/frames/1")
	img, e := jpeg.Decode(res.Body)
	res.Body.Close()
	if e != nil || img.Bounds().Dx() != FrameWidth {
		t.Fatalf("frame jpeg: %v", e)
	}
	if res, _ = http.Get(srv.URL + "/frames/..%2f1"); res.StatusCode == 200 {
		t.Fatal("bad frame id served")
	}
	s.expireFrames(time.Now().Add(frameTTL + time.Second))
	if res, _ = http.Get(srv.URL + "/frames/1"); res.StatusCode != 404 {
		t.Fatal("expired frame served")
	}
}

func TestStepUpdatesBackendAfterFallback(t *testing.T) {
	s := New(setup(t, true, true), &fakeSource{}, nil, nil)
	s.status.Backend = "gpu"
	engine := &fakeEngine{backend: "cpu"}
	buf := make([]byte, FrameBytes+stampBytes)
	if err := s.step(context.Background(), engine, buf); err != nil {
		t.Fatal(err)
	}
	if s.status.Backend != "cpu" {
		t.Fatalf("fallback reported backend %q", s.status.Backend)
	}

	engine.backend = "" // older engines omit the field on frame replies
	if err := s.step(context.Background(), engine, buf); err != nil {
		t.Fatal(err)
	}
	if s.status.Backend != "cpu" {
		t.Fatalf("reply with no backend erased %q", s.status.Backend)
	}
}

func TestRunReportsStatesAndRestartsFailedEngine(t *testing.T) {
	cfg := setup(t, true, true)
	engine := &fakeEngine{fail: true}
	starts := 0
	s := New(cfg, &fakeSource{}, nil, func(context.Context) (Engine, error) { starts++; return engine, nil })
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	s.Run(ctx)
	if starts != 1 || s.status.State != "error" {
		t.Fatalf("starts=%d state=%s", starts, s.status.State)
	}
	s2 := New(setup(t, true, false), &fakeSource{}, nil, func(context.Context) (Engine, error) { t.Fatal("engine started without files"); return nil, nil })
	ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	s2.Run(ctx2)
	if s2.status.State != "engine-missing" {
		t.Fatalf("state = %s", s2.status.State)
	}
}

func TestEncodeJPEGExpandsVideoRange(t *testing.T) {
	w, h := 16, 8
	nv12 := make([]byte, w*h*3/2)
	for i := range nv12[:w*h] {
		nv12[i] = 16 // video black
	}
	for i := w * h; i < len(nv12); i++ {
		nv12[i] = 128
	}
	b, e := EncodeJPEG(nv12, w, h, 90)
	if e != nil {
		t.Fatal(e)
	}
	img, _ := jpeg.Decode(bytesReader(b))
	r, g, bl, _ := img.At(3, 3).RGBA()
	if r>>8 > 6 || g>>8 > 6 || bl>>8 > 6 {
		t.Fatalf("video black not mapped to black: %d %d %d", r>>8, g>>8, bl>>8)
	}
	if _, e := EncodeJPEG(nv12[:10], w, h, 90); e == nil {
		t.Fatal("short frame accepted")
	}
}

// TestHelperEngine is executed as a child process by TestProcessEngineProtocol.
func TestHelperEngine(t *testing.T) {
	if os.Getenv("NCC_PLATE_HELPER") != "1" {
		t.Skip("helper process only")
	}
	out := bufio.NewWriter(os.Stdout)
	var shared *os.File
	if os.Getenv("NCC_PLATE_HELPER_SHARED") == "1" && os.Getenv("NCC_PLATE_FRAME_FD") == "3" {
		shared = os.NewFile(3, "shared-frame")
	}
	fmt.Fprintf(out, "{\"ready\":true,\"engine\":\"helper\",\"backend\":\"cpu\",\"sharedFrames\":%t}\n", shared != nil)
	out.Flush()
	header := make([]byte, 24)
	for {
		if _, e := io.ReadFull(os.Stdin, header); e != nil {
			os.Exit(0)
		}
		w, h := binary.LittleEndian.Uint32(header[4:]), binary.LittleEndian.Uint32(header[8:])
		seq := binary.LittleEndian.Uint64(header[16:])
		pixels := make([]byte, int(w*h*3/2))
		var err error
		switch string(header[:4]) {
		case protocolMagic:
			if shared != nil && len(pixels) == FrameBytes {
				os.Exit(2)
			}
			_, err = io.ReadFull(os.Stdin, pixels)
		case mappedMagic:
			if shared == nil {
				os.Exit(2)
			}
			_, err = shared.ReadAt(pixels, 0)
		default:
			os.Exit(2)
		}
		if err != nil || pixels[0] != 99 || pixels[len(pixels)-1] != byte(seq) {
			fmt.Fprintf(out, `{"seq":%d,"error":"bad frame"}`+"\n", seq)
		} else {
			fmt.Fprintf(out, `{"seq":%d,"ms":1.5,"plates":[{"box":[10,20,30,15],"score":0.8,"reading":{"number":"77","confidence":0.8}}]}`+"\n", seq)
		}
		out.Flush()
	}
}

func TestProcessEngineProtocol(t *testing.T) {
	t.Setenv("NCC_PLATE_HELPER", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	e, err := StartProcessEngine(ctx, os.Args[0], []string{"-test.run=^TestHelperEngine$"}, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if e.Info().Engine != "helper" || e.Info().SharedFrames {
		t.Fatalf("info = %+v", e.Info())
	}
	frame := make([]byte, FrameBytes)
	frame[0] = 99
	for seq := uint64(1); seq <= 3; seq++ {
		frame[len(frame)-1] = byte(seq)
		reply, err := e.Process(ctx, seq, frame, FrameWidth, FrameHeight)
		if err != nil || reply.Seq != seq || len(reply.Plates) != 1 || reply.Plates[0].Reading.Number != "77" {
			t.Fatalf("reply %d = %+v %v", seq, reply, err)
		}
	}
}

func TestProcessEngineMappedProtocol(t *testing.T) {
	t.Setenv("NCC_PLATE_HELPER", "1")
	t.Setenv("NCC_PLATE_HELPER_SHARED", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	e, err := StartProcessEngine(ctx, os.Args[0], []string{"-test.run=^TestHelperEngine$"}, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if !e.Info().SharedFrames {
		t.Skip("shared frame buffers unavailable on this system")
	}
	frame := make([]byte, FrameBytes)
	frame[0] = 99
	for seq := uint64(1); seq <= 3; seq++ {
		frame[len(frame)-1] = byte(seq)
		if reply, err := e.Process(ctx, seq, frame, FrameWidth, FrameHeight); err != nil || reply.Error != "" || reply.Seq != seq {
			t.Fatalf("reply %d = %+v %v", seq, reply, err)
		}
	}
}

func TestLiveExportReadsDirectlyIntoMappedEngineFrame(t *testing.T) {
	t.Setenv("NCC_PLATE_HELPER", "1")
	t.Setenv("NCC_PLATE_HELPER_SHARED", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	engine, err := StartProcessEngine(ctx, os.Args[0], []string{"-test.run=^TestHelperEngine$"}, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if !engine.Info().SharedFrames {
		t.Skip("shared frame buffers unavailable on this system")
	}
	source := &ExportSource{Path: filepath.Join(t.TempDir(), "value")}
	s := New(Config{}, source, nil, nil)
	measured := &measuredDeviceEngine{Engine: engine}
	fallback := make([]byte, FrameBytes+stampBytes)
	raw := make([]byte, FrameBytes+stampBytes)
	raw[0] = 99
	for seq := byte(1); seq <= 2; seq++ {
		raw[FrameBytes-1] = seq
		binary.LittleEndian.PutUint64(raw[FrameBytes:], uint64(seq))
		staged := source.Path + ".new"
		if err := os.WriteFile(staged, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(staged, source.Path); err != nil {
			t.Fatal(err)
		}
		if err := s.step(ctx, measured, fallback); err != nil {
			t.Fatal(err)
		}
		if fallback[0] != 0 || fallback[FrameBytes-1] != 0 {
			t.Fatal("mapped camera read used the fallback buffer")
		}
	}
	if s.frameSeq != 2 || measured.frames != 2 {
		t.Fatalf("processed %d frames, measured %d, want 2", s.frameSeq, measured.frames)
	}
	for id, last := range map[string]byte{"1": 1, "2": 2} {
		frame := s.acquireFrame(id)
		if frame == nil {
			t.Fatalf("frame %s was not retained", id)
		}
		if frame.data[0] != 99 || frame.data[FrameBytes-1] != last {
			t.Fatalf("cached frame %s was changed by later input", id)
		}
		s.releaseFrame(id, frame)
	}
	if err := s.step(ctx, measured, fallback); err != nil || s.frameSeq != 2 {
		t.Fatalf("duplicate camera frame advanced inference: seq=%d, err=%v", s.frameSeq, err)
	}
}

func bytesReader(b []byte) io.Reader { return &sliceReader{b: b} }

type sliceReader struct{ b []byte }

func (r *sliceReader) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.b)
	r.b = r.b[n:]
	return n, nil
}

func TestFramesKeptForPendingAndConfidentReadsOnly(t *testing.T) {
	now := time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)
	s := New(setup(t, true, true), &fakeSource{}, func(context.Context, time.Time) *plate.Location { return nil }, nil)
	s.clock = func() time.Time { return now }
	buf := make([]byte, FrameBytes+stampBytes)
	weak := &fakeEngine{plates: []EnginePlate{{Box: [4]int{20, 20, 60, 30}, Reading: plate.Reading{Number: "12", Confidence: 0.2}}}}
	s.step(context.Background(), weak, buf)
	if len(s.frames) != 0 || len(s.pending) != 0 {
		t.Fatal("a frame was kept for a reading that cannot become a record")
	}
	strong := &fakeEngine{plates: []EnginePlate{{Box: [4]int{20, 20, 60, 30}, Reading: plate.Reading{Number: "1234", Confidence: 0.9}}}}
	for range trackEarlyVotes {
		s.step(context.Background(), strong, buf)
	}
	first := s.pending[0].det.Frame
	for i := 0; i < frameRingSize+5; i++ {
		s.step(context.Background(), strong, buf)
	}
	if _, ok := s.frames[first]; !ok {
		t.Fatal("frame of a detection still waiting for GNSS was evicted")
	}
}

func TestExpiredFrameIsReusedAfterJPEGReaderReleasesIt(t *testing.T) {
	s := New(setup(t, true, true), &fakeSource{}, nil, nil)
	s.keepFrame("held", []byte{1, 2, 3})
	s.keepFrame("recent", []byte{4, 5, 6})
	held := s.acquireFrame("held")
	s.frames["held"].expires = time.Now().Add(-time.Second)
	s.expireFrames(time.Now())

	if s.acquireFrame("held") != nil {
		t.Fatal("an expired frame can still be acquired")
	}
	s.keepFrame("new", []byte{7, 8, 9})
	if held == nil || held.data[0] != 1 || held.data[2] != 3 {
		t.Fatal("eviction changed a frame held by a reader")
	}
	address := &held.data[0]
	s.releaseFrame("held", held)
	s.keepFrame("reused", []byte{10, 11, 12})
	reused := s.frames["reused"].data
	if &reused[0] != address || reused[0] != 10 || reused[2] != 12 {
		t.Fatal("the released frame buffer was not reused")
	}
}

func TestReadMinConfidenceAndRestartOnChange(t *testing.T) {
	cfg := setup(t, true, true)
	if got := ReadMinConfidence(cfg.StateDir); got != 0.6 {
		t.Fatalf("default minConfidence = %v, want 0.6", got)
	}
	settingsPath := filepath.Join(cfg.StateDir, "plate", "settings.json")
	os.WriteFile(settingsPath, []byte(`{"enabled":true,"minConfidence":0.35}`), 0600)
	if got := ReadMinConfidence(cfg.StateDir); got != 0.35 {
		t.Fatalf("custom minConfidence = %v, want 0.35", got)
	}
	os.WriteFile(settingsPath, []byte(`{"enabled":true,"minConfidence":0.1}`), 0600)
	if got := ReadMinConfidence(cfg.StateDir); got != 0.6 {
		t.Fatalf("out-of-range minConfidence = %v, want fallback 0.6", got)
	}

	os.WriteFile(settingsPath, []byte(`{"enabled":true,"minConfidence":0.6}`), 0600)
	now := time.Date(2026, 11, 20, 6, 0, 0, 0, time.UTC)
	var seen []float64
	s := New(cfg, &fakeSource{}, nil, func(context.Context) (Engine, error) {
		seen = append(seen, ReadMinConfidence(cfg.StateDir))
		return &fakeEngine{}, nil
	})
	s.clock = func() time.Time {
		now = now.Add(settingsPoll)
		if len(seen) == 1 {
			os.WriteFile(settingsPath, []byte(`{"enabled":true,"minConfidence":0.3}`), 0600)
		}
		return now
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	s.Run(ctx)
	if len(seen) < 2 || seen[0] != 0.6 || seen[1] != 0.3 {
		t.Fatalf("engine restarts on minConfidence change = %v", seen)
	}
}
