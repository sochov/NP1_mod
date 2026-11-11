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

type fakeSource struct{ n int }

func (f *fakeSource) Next(buf []byte) ([2]int64, bool, error) {
	f.n++
	for i := range buf[:FrameBytes] {
		buf[i] = byte(16 + f.n)
	}
	return [2]int64{int64(f.n), 0}, true, nil
}

type fakeEngine struct {
	plates []EnginePlate
	fail   bool
	calls  int
}

func (f *fakeEngine) Info() EngineInfo {
	return EngineInfo{Ready: true, Engine: "fake", Backend: "test"}
}
func (f *fakeEngine) Process(_ context.Context, seq uint64, _ []byte, _, _ int) (EngineReply, error) {
	f.calls++
	if f.fail {
		return EngineReply{}, errors.New("boom")
	}
	return EngineReply{Seq: seq, Plates: f.plates}, nil
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
	now = now.Add(frameTTL + time.Second)
	s.expireFrames(now)
	if res, _ = http.Get(srv.URL + "/frames/1"); res.StatusCode != 404 {
		t.Fatal("expired frame served")
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
	fmt.Fprintln(out, `{"ready":true,"engine":"helper","backend":"cpu"}`)
	out.Flush()
	header := make([]byte, 24)
	for {
		if _, e := io.ReadFull(os.Stdin, header); e != nil {
			os.Exit(0)
		}
		w, h := binary.LittleEndian.Uint32(header[4:]), binary.LittleEndian.Uint32(header[8:])
		seq := binary.LittleEndian.Uint64(header[16:])
		pixels := make([]byte, int(w*h*3/2))
		io.ReadFull(os.Stdin, pixels)
		if string(header[:4]) != protocolMagic || pixels[0] != 99 {
			fmt.Fprintf(out, `{"seq":%d,"error":"bad frame"}`+"\n", seq)
		} else {
			fmt.Fprintf(out, `{"seq":%d,"ms":1.5,"plates":[{"box":[10,20,30,15],"score":0.8,"reading":{"number":"77","confidence":0.8}}]}`+"\n", seq)
		}
		out.Flush()
	}
}

func TestProcessEngineProtocol(t *testing.T) {
	os.Setenv("NCC_PLATE_HELPER", "1")
	defer os.Unsetenv("NCC_PLATE_HELPER")
	e, err := StartProcessEngine(context.Background(), os.Args[0], []string{"-test.run=^TestHelperEngine$"}, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if e.Info().Engine != "helper" {
		t.Fatalf("info = %+v", e.Info())
	}
	frame := make([]byte, 64*32*3/2)
	frame[0] = 99
	for seq := uint64(1); seq <= 3; seq++ {
		reply, err := e.Process(context.Background(), seq, frame, 64, 32)
		if err != nil || reply.Seq != seq || len(reply.Plates) != 1 || reply.Plates[0].Reading.Number != "77" {
			t.Fatalf("reply %d = %+v %v", seq, reply, err)
		}
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
