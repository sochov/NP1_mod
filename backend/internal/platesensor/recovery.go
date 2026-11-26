package platesensor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"
)

type frontFeed struct {
	name string
	run  func(context.Context, func([]byte)) error
}

// Each decoder owns its frame buffers. A stopped decoder cannot publish into
// the next session. A slow inference consumer only ever sees the latest frame.
type recoverySession struct {
	cancel   context.CancelFunc
	done     chan error
	mu       sync.Mutex
	latest   []byte
	received time.Time
	pending  bool
}

func (r *recoverySession) publish(frame []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.latest = append(r.latest[:0], frame...)
	r.received, r.pending = time.Now(), true
}

type recoveringFront struct {
	primary              FrameSource
	feeds                []frontFeed
	clock                func() time.Time
	session              *recoverySession
	lastPrimary, retryAt time.Time
	attempt              int
	index                int
	active               string
}

func NewFrontSource() FrameSource {
	return &recoveringFront{primary: &ExportSource{Path: FrontExport}, clock: time.Now,
		feeds: []frontFeed{decodedFeed("front-encoder", streamFrontEncoder), decodedFeed("front-recording", streamFrontRecording)}}
}

func (s *recoveringFront) stop() {
	if s.session != nil {
		s.session.cancel()
		s.session = nil
	}
}

func (s *recoveringFront) reset() {
	s.stop()
	s.lastPrimary, s.retryAt = time.Time{}, time.Time{}
	s.index, s.attempt, s.active = 0, 0, ""
}

func (s *recoveringFront) Next(buf []byte) ([2]int64, bool, error) {
	stamp, ok, err := s.primary.Next(buf)
	now := s.clock()
	if ok && err == nil {
		if s.active != "" && s.active != "front-export" {
			log.Print("plate camera primary front export resumed")
		}
		s.reset()
		s.active, s.lastPrimary = "front-export", now
		return stamp, true, nil
	}
	if s.lastPrimary.IsZero() {
		s.lastPrimary = now
	}
	if now.Sub(s.lastPrimary) < cameraFrameWait || now.Before(s.retryAt) {
		return stamp, false, err
	}
	if s.session == nil {
		ctx, cancel := context.WithCancel(context.Background())
		r := &recoverySession{cancel: cancel, done: make(chan error, 1), received: now}
		s.session = r
		feed := s.feeds[s.index]
		log.Printf("plate camera opening %s", feed.name)
		go func() { r.done <- feed.run(ctx, r.publish) }()
	}
	r := s.session
	select {
	case failure := <-r.done:
		s.failed(now, failure)
	default:
		r.mu.Lock()
		age := now.Sub(r.received)
		fresh := r.pending && age < cameraFrameWait
		if fresh {
			copy(buf, r.latest)
			r.pending = false
			s.attempt = 0
			name := s.feeds[s.index].name
			if s.active != name {
				log.Printf("plate camera receiving %s", name)
				s.active = name
			}
		}
		t := r.received
		r.mu.Unlock()
		if fresh {
			return [2]int64{t.Unix(), int64(t.Nanosecond())}, true, nil
		}
		// Allow time to cross a recording boundary and acquire an IDR.
		if age >= 12*time.Second {
			s.failed(now, errors.New("no new decoded front frame for 12s"))
		}
	}
	if s.session != nil {
		// A poll between backup frames is normal even when the primary export
		// is missing. Let the sensor measure time since the last real frame.
		return stamp, false, nil
	}
	return stamp, false, err
}

func (s *recoveringFront) failed(now time.Time, err error) {
	log.Printf("plate camera %s interrupted: %v", s.feeds[s.index].name, err)
	s.stop()
	s.active = ""
	s.index = (s.index + 1) % len(s.feeds)
	s.retryAt = now.Add(time.Second << min(s.attempt, 4))
	s.attempt = min(s.attempt+1, 4)
}

func decodedFeed(name string, stream func(context.Context, io.Writer) error) frontFeed {
	return frontFeed{name, func(ctx context.Context, publish func([]byte)) error {
		return decodeFront(ctx, stream, publish)
	}}
}

func decodeFront(ctx context.Context, stream func(context.Context, io.Writer) error, publish func([]byte)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gst-launch-1.0", "-q",
		"fdsrc", "fd=0", "do-timestamp=true", "!", "h264parse", "!",
		"avdec_h264", "max-threads=2", "output-corrupt=false", "!",
		"videorate", "drop-only=true", "!", "video/x-raw,framerate=8/1", "!",
		"videoconvert", "!", "video/x-raw,format=NV12,width=1920,height=1080", "!",
		"fdsink", "fd=1", "sync=false")
	cmd.Stderr = os.Stderr
	input, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return err
	}
	if err = cmd.Start(); err != nil {
		input.Close()
		output.Close()
		return err
	}
	sent := make(chan error, 1)
	go func() {
		err := stream(ctx, input)
		input.Close()
		sent <- err
		cancel()
	}()
	frame := make([]byte, FrameBytes)
	for {
		if _, err = io.ReadFull(output, frame); err != nil {
			break
		}
		publish(frame)
	}
	cancel()
	input.Close()
	waitErr := cmd.Wait()
	return fmt.Errorf("front decoder stopped: %w", errors.Join(err, waitErr, <-sent))
}
