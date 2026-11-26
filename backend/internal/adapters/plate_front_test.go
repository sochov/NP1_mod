package adapters

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/h264tail"
)

func TestPlateFrontWaitsForCurrentFrontKeyframe(t *testing.T) {
	hub := &cameraEncoderHub{notify: make(chan struct{})}
	publish := func(key bool, id byte, age time.Duration, width uint16) {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		hub.config = h264tail.Config{AVC: []byte{1}, Width: width, Height: 1080}
		hub.nextID++
		if key {
			hub.frames = nil
		}
		hub.frames = append(hub.frames, cameraEncoderHubFrame{
			at: time.Now().Add(-age), id: hub.nextID,
			sample: h264tail.Sample{Keyframe: key, Data: []byte{0, 0, 0, 2, 0x65, id}},
		})
		hub.signalLocked()
	}
	publish(true, 1, 0, 1920)
	ready := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(ready)
		streamPlateFront(w, r, hub, 1)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	result := make(chan []byte, 1)
	go func() {
		resp, err := server.Client().Do(req)
		if err != nil {
			result <- nil
			return
		}
		defer resp.Body.Close()
		data := make([]byte, 12)
		if _, err := io.ReadFull(resp.Body, data); err != nil {
			data = nil
		}
		result <- data
	}()
	<-ready
	publish(false, 2, 0, 1920)
	publish(true, 3, time.Minute, 1920)
	publish(true, 4, 0, 1280)
	select {
	case data := <-result:
		t.Fatalf("accepted stale or unrelated geometry: %x", data)
	case <-time.After(50 * time.Millisecond):
	}
	publish(true, 5, 0, 1920)
	publish(false, 6, 0, 1920)
	want := []byte{0, 0, 0, 1, 0x65, 5, 0, 0, 0, 1, 0x65, 6}
	if data := <-result; !bytes.Equal(data, want) {
		t.Fatalf("got %x want %x", data, want)
	}
}

func TestPlateOnlyHubReleasesFramesUnlessWebStarted(t *testing.T) {
	h := &cameraEncoderHub{running: true, notify: make(chan struct{}), frames: []cameraEncoderHubFrame{{id: 1}}}
	h.plateReading = true
	if h.stopIfUnused(nil) {
		t.Fatal("stopped active subscriber")
	}
	h.plateReading = false
	h.webStarted = true
	if h.stopIfUnused(nil) {
		t.Fatal("stopped WebRTC")
	}
	h.webStarted = false
	if !h.stopIfUnused(nil) || h.running || len(h.frames) != 0 {
		t.Fatal("leaked plate-only receiver")
	}
}
