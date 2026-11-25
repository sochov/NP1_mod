package androidauto

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// The hardware H.264 encoder is used through the stock GStreamer OpenMAX
// element (the same path the stock recorder uses). Frames go in as raw NV12 on
// stdin; encoded access units come back on stdout framed by gdppay, which keeps
// buffer boundaries so no frame waits for the next one to be split off.

// AccessUnit is one encoded picture in Annex B.
type AccessUnit struct {
	Data []byte
	Key  bool
	At   time.Time // when its input frame was submitted
}

// EncoderConfig selects the stream.
type EncoderConfig struct {
	Width, Height int
	FPS           int
	BitrateKbps   int
	IDRSeconds    int
	Launch        string // gst-launch-1.0 path
}

// Encoder is one running encoder process.
type Encoder struct {
	cfg    EncoderConfig
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	out    chan AccessUnit
	done   chan struct{}
	err    error
	mu     sync.Mutex
	queued []time.Time
	stderr bytes.Buffer
}

// Args returns the gst-launch arguments for cfg.
func (cfg EncoderConfig) Args() []string {
	fps := max(cfg.FPS, 1)
	idr := max(cfg.IDRSeconds, 1) * fps
	return []string{"-q",
		"fdsrc", "fd=0", "blocksize=" + strconv.Itoa(cfg.Width*cfg.Height*3/2), "!",
		"rawvideoparse", "format=nv12", "width=" + strconv.Itoa(cfg.Width), "height=" + strconv.Itoa(cfg.Height),
		"framerate=" + strconv.Itoa(fps) + "/1", "!",
		"omxh264enc", "control-rate=variable", "target-bitrate=" + strconv.Itoa(max(cfg.BitrateKbps, 200)*1000),
		"interval-intraframes=" + strconv.Itoa(idr), "periodicity-idr=1", "b-frames=0", "!",
		"video/x-h264,profile=baseline,stream-format=byte-stream,alignment=au", "!",
		"gdppay", "!", "fdsink", "fd=1", "sync=false",
	}
}

// StartEncoder launches the encoder.
func StartEncoder(cfg EncoderConfig) (*Encoder, error) {
	if cfg.Launch == "" {
		cfg.Launch = "/usr/bin/gst-launch-1.0"
	}
	if cfg.Width%16 != 0 && cfg.Width%8 != 0 {
		return nil, fmt.Errorf("encoder width %d", cfg.Width)
	}
	e := &Encoder{cfg: cfg, out: make(chan AccessUnit, 8), done: make(chan struct{})}
	// Not CommandContext: killing gst-launch mid-stream leaves the video
	// driver failing to stop its session. Close ends it with an EOS instead.
	e.cmd = exec.Command(cfg.Launch, cfg.Args()...)
	e.cmd.Stderr = &limitedWriter{buf: &e.stderr, max: 4096}
	var err error
	if e.stdin, err = e.cmd.StdinPipe(); err != nil {
		return nil, err
	}
	stdout, err := e.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = e.cmd.Start(); err != nil {
		return nil, err
	}
	go e.read(stdout)
	return e, nil
}

type limitedWriter struct {
	buf *bytes.Buffer
	max int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.buf.Len() < w.max {
		w.buf.Write(p[:min(len(p), w.max-w.buf.Len())])
	}
	return len(p), nil
}

// gdp header layout (GStreamer Data Protocol 1.0).
const (
	gdpHeader     = 62
	gdpTypeBuffer = 1
)

func (e *Encoder) read(r io.Reader) {
	defer close(e.done)
	defer close(e.out)
	br := bufio.NewReaderSize(r, 256*1024)
	var h [gdpHeader]byte
	for {
		if _, err := io.ReadFull(br, h[:]); err != nil {
			e.finish(err)
			return
		}
		typ := binary.BigEndian.Uint16(h[4:])
		n := binary.BigEndian.Uint32(h[6:])
		if n > 16<<20 {
			e.finish(errors.New("encoder: oversized buffer"))
			return
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(br, payload); err != nil {
			e.finish(err)
			return
		}
		if typ != gdpTypeBuffer || n == 0 {
			continue
		}
		e.mu.Lock()
		var at time.Time
		if len(e.queued) > 0 {
			at, e.queued = e.queued[0], e.queued[1:]
		}
		e.mu.Unlock()
		e.out <- AccessUnit{Data: payload, Key: hasNAL(payload, 5), At: at}
	}
}

func (e *Encoder) finish(err error) {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		err = nil
	}
	werr := e.cmd.Wait()
	if err == nil && werr != nil {
		err = fmt.Errorf("encoder exited: %v %s", werr, bytes.TrimSpace(e.stderr.Bytes()))
	}
	e.err = err
}

// Pending is how many submitted frames have not come out yet.
func (e *Encoder) Pending() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.queued)
}

// Write submits one NV12 frame.
func (e *Encoder) Write(frame []byte, now time.Time) error {
	e.mu.Lock()
	e.queued = append(e.queued, now)
	if len(e.queued) > 64 {
		e.queued = e.queued[len(e.queued)-64:]
	}
	e.mu.Unlock()
	_, err := e.stdin.Write(frame)
	return err
}

// Out delivers encoded access units; it closes when the encoder stops.
func (e *Encoder) Out() <-chan AccessUnit { return e.out }

// Close stops the encoder: end of input lets the pipeline drain and release
// the hardware session; a stuck one is terminated, then killed.
func (e *Encoder) Close() error {
	e.stdin.Close()
	go func() {
		// Nobody reads the remaining output once the session is gone.
		for range e.out {
		}
	}()
	select {
	case <-e.done:
		return e.err
	case <-time.After(3 * time.Second):
	}
	_ = e.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-e.done:
	case <-time.After(2 * time.Second):
		_ = e.cmd.Process.Kill()
		<-e.done
	}
	return e.err
}

// NAL helpers.

func forEachNAL(b []byte, fn func(nal []byte) bool) {
	i := 0
	start := -1
	for i+3 <= len(b) {
		if b[i] == 0 && b[i+1] == 0 && b[i+2] == 1 {
			if start >= 0 {
				end := i
				if end > start && b[end-1] == 0 {
					end--
				}
				if !fn(b[start:end]) {
					return
				}
			}
			i += 3
			start = i
			continue
		}
		i++
	}
	if start >= 0 && start < len(b) {
		fn(b[start:])
	}
}

func hasNAL(b []byte, typ byte) bool {
	found := false
	forEachNAL(b, func(n []byte) bool {
		if len(n) > 0 && n[0]&0x1f == typ {
			found = true
			return false
		}
		return true
	})
	return found
}

// splitConfig returns the SPS/PPS of an access unit as Annex B, and the access
// unit without access unit delimiters.
func splitConfig(au []byte) (config, rest []byte) {
	forEachNAL(au, func(n []byte) bool {
		if len(n) == 0 {
			return true
		}
		switch n[0] & 0x1f {
		case 7, 8:
			config = append(append(config, 0, 0, 0, 1), n...)
			rest = append(append(rest, 0, 0, 0, 1), n...)
		case 9: // access unit delimiter: not needed by head units
		default:
			rest = append(append(rest, 0, 0, 0, 1), n...)
		}
		return true
	})
	return config, rest
}
