package adapters

import (
	"net/http"
	"time"
)

// HandlePlateFront is reachable only on the broker's separate root-only socket.
// The source is fixed. A subscriber waits for the next live IDR, so starting
// after hours of input loss cannot turn a cached GOP into fresh OCR records.
func HandlePlateFront(w http.ResponseWriter, r *http.Request) {
	hub := cameraEncoderHubs["front"]
	hub.mu.Lock()
	if hub.plateReading {
		hub.mu.Unlock()
		http.Error(w, "front encoder already in use by recognition", http.StatusConflict)
		return
	}
	hub.plateReading = true
	cursor := hub.nextID
	hub.startLocked()
	hub.mu.Unlock()
	defer func() {
		hub.mu.Lock()
		hub.plateReading = false
		hub.mu.Unlock()
	}()
	streamPlateFront(w, r, hub, cursor)
}

func streamPlateFront(w http.ResponseWriter, r *http.Request, hub *cameraEncoderHub, cursor uint64) {
	controller := http.NewResponseController(w)
	w.Header().Set("Content-Type", "video/h264")
	sent, synced := false, false
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	for {
		cfg, frames, notify, gap := hub.after(cursor)
		if gap {
			// A lost GOP invalidates inter-frame references. Only resume on a
			// fresh IDR; never drain the preceding GOP into the decoder.
			cursor = 0
			synced = false
			continue
		}
		for _, frame := range frames {
			cursor = frame.id
			if time.Since(frame.at) > 750*time.Millisecond || cfg.Width != 1920 || cfg.Height != 1080 {
				synced = false
				continue
			}
			if !synced && !frame.sample.Keyframe {
				continue
			}
			data, err := cameraAVCCToAnnexB(frame.sample.Data)
			if err != nil {
				synced = false
				continue
			}
			synced = true
			// A blocked decoder may recover later, but must not drain seconds
			// of stale video as new input. Drop a slow connection instead.
			_ = controller.SetWriteDeadline(time.Now().Add(time.Second))
			if _, err = w.Write(data); err != nil {
				return
			}
			if err = controller.Flush(); err != nil {
				return
			}
			sent = true
			deadline.Reset(8 * time.Second)
		}
		select {
		case <-r.Context().Done():
			return
		case <-notify:
		case <-deadline.C:
			if !sent {
				http.Error(w, "no current front encoder frames", http.StatusServiceUnavailable)
			}
			return
		}
	}
}
