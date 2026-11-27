package adapters

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/h264tail"
)

// cameraEncoderHub is the sole owner of a DriveRec encoder-tap socket for one
// camera. It drains the tap continuously and keeps only the current GOP. Web
// clients never recreate the tap socket and never read delayed SD recordings.
type cameraEncoderHubFrame struct {
	id     uint64
	at     time.Time
	sample h264tail.Sample
}

type cameraEncoderHub struct {
	source       string
	mu           sync.Mutex
	notify       chan struct{}
	running      bool
	webStarted   bool
	plateReading bool
	config       h264tail.Config
	frames       []cameraEncoderHubFrame
	nextID       uint64
	err          error
}

var cameraEncoderHubs = map[string]*cameraEncoderHub{
	"front": {source: "front", notify: make(chan struct{})},
	"in":    {source: "in", notify: make(chan struct{})},
}

func (h *cameraEncoderHub) signalLocked() {
	close(h.notify)
	h.notify = make(chan struct{})
}

func (h *cameraEncoderHub) start() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.webStarted = true
	h.startLocked()
}

func (h *cameraEncoderHub) startLocked() {
	if !h.running {
		h.running = true
		go h.run()
	}
}

// A plate-only subscription releases its receiver when the ImageRecog export
// recovers. An existing WebRTC hub keeps its normal lifetime.
func (h *cameraEncoderHub) stopIfUnused(stream *cameraEncoderStream) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.webStarted || h.plateReading {
		return false
	}
	if stream != nil {
		stream.Close()
	}
	h.frames = nil
	h.config = h264tail.Config{}
	h.running = false
	h.signalLocked()
	return true
}

func (h *cameraEncoderHub) run() {
	for {
		if h.stopIfUnused(nil) {
			return
		}
		stream, err := openCameraEncoderStream(h.source)
		if err != nil {
			h.mu.Lock()
			h.err = err
			h.signalLocked()
			h.mu.Unlock()
			time.Sleep(250 * time.Millisecond)
			continue
		}
		log.Printf("camera WebRTC encoder hub listening source=%s", h.source)
		announced := false
		for {
			if h.stopIfUnused(stream) {
				return
			}
			nextContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			sample, nextErr := stream.Next(nextContext)
			cancel()
			if errors.Is(nextErr, context.DeadlineExceeded) {
				log.Printf("camera WebRTC encoder hub waiting source=%s datagrams=%d dropped=%d configured=%t locked=%t", h.source, stream.received, stream.dropped, len(stream.config.AVC) != 0, stream.locked)
				continue
			}
			if nextErr != nil {
				stream.Close()
				h.mu.Lock()
				h.err = nextErr
				h.frames = nil
				h.signalLocked()
				h.mu.Unlock()
				break
			}
			if !announced {
				log.Printf("camera WebRTC encoder hub live source=%s width=%d height=%d", h.source, stream.config.Width, stream.config.Height)
				announced = true
			}
			h.mu.Lock()
			h.err = nil
			h.config = stream.config
			h.nextID++
			if sample.Keyframe {
				h.frames = h.frames[:0]
			}
			if sample.Keyframe || len(h.frames) != 0 {
				h.frames = append(h.frames, cameraEncoderHubFrame{id: h.nextID, at: time.Now(), sample: sample})
			}
			bytes := 0
			for _, frame := range h.frames {
				bytes += len(frame.sample.Data)
			}
			if len(h.frames) > 300 || bytes > 12<<20 {
				// Wait for a new IDR rather than retaining a corrupt/unbounded GOP.
				h.frames = nil
			}
			h.signalLocked()
			h.mu.Unlock()
		}
	}
}

// after returns a stable copy of all cached frames newer than cursor. A gap
// means that the RTP sender fell behind an entire GOP and must restart from
// the newest cached IDR rather than transmit stale dependent frames.
func (h *cameraEncoderHub) after(cursor uint64) (h264tail.Config, []cameraEncoderHubFrame, <-chan struct{}, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.frames) == 0 || len(h.config.AVC) == 0 {
		return h.config, nil, h.notify, false
	}
	start := 0
	if cursor != 0 {
		for start < len(h.frames) && h.frames[start].id <= cursor {
			start++
		}
		if start == len(h.frames) {
			return h.config, nil, h.notify, false
		}
		if h.frames[start].id != cursor+1 {
			return h.config, nil, h.notify, true
		}
	}
	return h.config, append([]cameraEncoderHubFrame(nil), h.frames[start:]...), h.notify, false
}
