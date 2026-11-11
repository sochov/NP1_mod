package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/h264tail"
)

type cameraTailEnvironment struct {
	root  string
	mount func() (string, error)
	now   func() time.Time
}
type cameraTailCandidate struct {
	relative string
	info     os.FileInfo
	start    time.Time
}
type cameraH264Tail struct {
	env                     cameraTailEnvironment
	source, token, relative string
	root                    *os.Root
	file                    *os.File
	info                    os.FileInfo
	config                  h264tail.Config
	demux                   *h264tail.Demuxer
	offset                  int64
	lastSample              time.Time
	lastRead                time.Time
	lastRotationCheck       time.Time
	lastMountCheck          time.Time
}

func cameraTailCandidates(env cameraTailEnvironment, source string) ([]cameraTailCandidate, error) {
	if source != "front" && source != "in" {
		return nil, errors.New("invalid camera source")
	}
	root, dir, err := openContained(env.root, recordingDirectory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	defer dir.Close()
	entries, err := dir.ReadDir(20001)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > 20000 {
		return nil, errors.New("camera recording inventory truncated")
	}
	var out []cameraTailCandidate
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			continue
		}
		camera, start, ok := parseRecordingFilename(entry.Name())
		if !ok || camera != source {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() < 16 || info.ModTime().After(env.now().Add(2*time.Second)) {
			continue
		}
		out = append(out, cameraTailCandidate{path.Join(recordingDirectory, entry.Name()), info, start})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].start.Equal(out[j].start) {
			return out[i].start.After(out[j].start)
		}
		return out[i].relative > out[j].relative
	})
	return out, nil
}

func openTailCandidate(env cameraTailEnvironment, item cameraTailCandidate) (*os.Root, *os.File, os.FileInfo, error) {
	root, file, err := openContained(env.root, item.relative)
	if err != nil {
		return nil, nil, nil, err
	}
	info, err := file.Stat()
	if err != nil || !os.SameFile(info, item.info) || !info.Mode().IsRegular() || info.Size() < item.info.Size() {
		file.Close()
		root.Close()
		return nil, nil, nil, errors.New("camera recording changed")
	}
	return root, file, info, nil
}

func openCameraH264Tail(env cameraTailEnvironment, source string) (*cameraH264Tail, error) {
	token, err := env.mount()
	if err != nil || token == "" {
		return nil, errors.New("camera SD unavailable")
	}
	items, err := cameraTailCandidates(env, source)
	if err != nil {
		return nil, err
	}
	var config h264tail.Config
	preRoll := int64(4 << 20)
	configFound := false
	for i, item := range items {
		if i >= 12 {
			break
		}
		if env.now().Sub(item.info.ModTime()) < 2*time.Second {
			continue
		}
		root, file, info, err := openTailCandidate(env, item)
		if err != nil {
			continue
		}
		config, err = h264tail.ConfigFromMP4(file, info.Size())
		if err == nil {
			if metadata, metadataError := ReadRecordingMetadata(file, info.Size()); metadataError == nil && metadata.DurationSeconds > 0 {
				preRoll = int64(float64(info.Size()) / metadata.DurationSeconds * 3)
				if preRoll < 65536 {
					preRoll = 65536
				}
				if preRoll > 4<<20 {
					preRoll = 4 << 20
				}
			}
		}
		file.Close()
		root.Close()
		if err == nil {
			configFound = true
			break
		}
	}
	if !configFound {
		return nil, errors.New("camera AVC configuration unavailable")
	}
	for i, item := range items {
		if i >= 4 {
			break
		}
		if env.now().Sub(item.info.ModTime()) > 6*time.Second {
			continue
		}
		root, file, info, err := openTailCandidate(env, item)
		if err != nil {
			continue
		}
		start, _, growing, rangeError := h264tail.MediaDataRange(file, info.Size())
		if rangeError != nil || !growing {
			file.Close()
			root.Close()
			continue
		}
		current, err := env.mount()
		if err != nil || current != token {
			file.Close()
			root.Close()
			return nil, errors.New("camera SD changed")
		}
		demux, err := h264tail.NewDemuxer(config)
		if err != nil {
			file.Close()
			root.Close()
			return nil, err
		}
		offset := start
		if info.Size()-offset > preRoll {
			offset = info.Size() - preRoll
		}
		return &cameraH264Tail{env: env, source: source, token: token, relative: item.relative, root: root, file: file, info: info, config: config, demux: demux, offset: offset, lastSample: env.now(), lastMountCheck: env.now()}, nil
	}
	return nil, errors.New("camera growing recording unavailable")
}
func (t *cameraH264Tail) Close() {
	if t.file != nil {
		t.file.Close()
		t.file = nil
	}
	if t.root != nil {
		t.root.Close()
		t.root = nil
	}
}

func (t *cameraH264Tail) rotate() error {
	items, err := cameraTailCandidates(t.env, t.source)
	if err != nil {
		return err
	}
	for i, item := range items {
		if i >= 4 {
			break
		}
		if item.relative == t.relative || t.env.now().Sub(item.info.ModTime()) > 6*time.Second {
			continue
		}
		_, currentStart, valid := parseRecordingFilename(path.Base(t.relative))
		if !valid || item.start.Before(currentStart) || (item.start.Equal(currentStart) && item.relative <= t.relative) {
			continue
		}
		root, file, info, err := openTailCandidate(t.env, item)
		if err != nil {
			continue
		}
		start, _, growing, err := h264tail.MediaDataRange(file, info.Size())
		if err != nil || !growing {
			file.Close()
			root.Close()
			continue
		}
		// Keep the demuxer's final pending AU until the next real AUD. This
		// preserves the last frame at a file boundary without a forged delimiter.
		t.file.Close()
		t.root.Close()
		t.root, t.file, t.info, t.relative, t.offset = root, file, info, item.relative, start
		return nil
	}
	return io.EOF
}

func (t *cameraH264Tail) Next(ctx context.Context) ([]h264tail.Sample, error) {
	var buffer [65536]byte
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if t.env.now().Sub(t.lastMountCheck) >= time.Second {
			current, err := t.env.mount()
			if err != nil || current != t.token {
				return nil, errors.New("camera SD changed")
			}
			t.lastMountCheck = t.env.now()
		}
		info, err := t.file.Stat()
		if err != nil || info.Size() < t.offset {
			return nil, errors.New("camera recording truncated")
		}
		_, end, growing, err := h264tail.MediaDataRange(t.file, info.Size())
		if err != nil {
			return nil, err
		}
		if t.offset < end {
			next := int64(len(buffer))
			if end-t.offset < next {
				next = end - t.offset
			}
			n, readError := t.file.ReadAt(buffer[:int(next)], t.offset)
			if readError != nil && readError != io.EOF {
				return nil, readError
			}
			t.offset += int64(n)
			if n > 0 {
				t.lastRead = t.env.now()
			}
			samples, err := t.demux.Feed(buffer[:n])
			if err != nil {
				return nil, err
			}
			if len(samples) > 0 {
				t.lastSample = t.env.now()
				return samples, nil
			}
			if n > 0 {
				continue
			}
		}
		// The stock writer can start the next clip before finalizing the old
		// size-zero mdat header. At EOF, switch only to a newer, currently growing
		// clip after a quiet interval; waiting for moov alone can stall forever.
		if (!growing || t.env.now().Sub(t.lastRead) > time.Second || t.env.now().Sub(t.lastSample) > 2*time.Second) && t.env.now().Sub(t.lastRotationCheck) >= time.Second {
			t.lastRotationCheck = t.env.now()
			if err := t.rotate(); err == nil {
				continue
			} else if err != io.EOF {
				return nil, err
			}
		}
		// Both an open-ended header and a finalized old file can remain during
		// the stock writer's handoff. Header state is not evidence of a stop.
		if t.env.now().Sub(t.lastSample) > 30*time.Second {
			return nil, errors.New("camera recording stopped")
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

var cameraH264Slots = make(chan struct{}, 2)
var cameraH264StartMu sync.Mutex
var cameraH264DiagnosticMu sync.Mutex

// HandleCameraH264 reads only stock MP4 data. The original AVC access-unit bytes
// are remuxed for MSE. No camera device/session/encoder or source file is changed.
func HandleCameraH264(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 1 || len(r.URL.Query()["source"]) != 1 {
		http.Error(w, "invalid camera source", 400)
		return
	}
	if source := r.URL.Query().Get("source"); source != "front" && source != "in" {
		http.Error(w, "invalid camera source", 400)
		return
	}
	select {
	case cameraH264Slots <- struct{}{}:
		defer func() { <-cameraH264Slots }()
	default:
		http.Error(w, "camera streams busy", 429)
		return
	}
	if !cameraH264StartMu.TryLock() {
		http.Error(w, "camera preparation busy", 429)
		return
	}
	tail, err := openCameraH264Tail(cameraTailEnvironment{SDRoot, func() (string, error) {
		if SDStatus().Status != "available" {
			return "", errors.New("no SD")
		}
		return recordingMountToken(SDRoot)
	}, time.Now}, r.URL.Query().Get("source"))
	cameraH264StartMu.Unlock()
	if err != nil {
		http.Error(w, "camera recording unavailable", 503)
		return
	}
	defer tail.Close()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	samples, err := tail.Next(ctx)
	if err != nil {
		http.Error(w, "camera video unavailable", 503)
		return
	}
	init, err := tail.config.InitSegment()
	if err != nil {
		http.Error(w, "camera codec unavailable", 503)
		return
	}
	codec, err := tail.config.Codec()
	if err != nil {
		http.Error(w, "camera codec unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Camera-Codec", codec)
	w.Header().Set("X-Camera-Width", strconv.Itoa(int(tail.config.Width)))
	w.Header().Set("X-Camera-Height", strconv.Itoa(int(tail.config.Height)))
	w.Header().Set("X-Camera-FPS", fmt.Sprintf("%.3f", float64(tail.config.Timescale)/float64(tail.config.SampleDuration)))
	w.Header().Set("X-Camera-Audio", "none")
	controller := http.NewResponseController(w)
	write := func(data []byte) error {
		_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
		defer controller.SetWriteDeadline(time.Time{})
		if _, err := w.Write(data); err != nil {
			return err
		}
		return controller.Flush()
	}
	if write(init) != nil {
		return
	}
	sequence, dts := uint32(1), uint64(0)
	finish := func(stage string, err error) {
		if err == nil {
			return
		}
		detail := map[string]any{"source": tail.source, "stage": stage, "error": err.Error(), "fragments": sequence - 1, "offset": tail.offset, "at": time.Now().UTC(), "file": tail.relative, "dropped": tail.demux.Dropped, "readAgeSeconds": time.Since(tail.lastRead).Seconds(), "sampleAgeSeconds": time.Since(tail.lastSample).Seconds()}
		if info, e := tail.file.Stat(); e == nil {
			detail["fileSize"] = info.Size()
			detail["modifiedAgeSeconds"] = time.Since(info.ModTime()).Seconds()
			_, _, open, e := h264tail.MediaDataRange(tail.file, info.Size())
			detail["openEnded"] = open
			if e != nil {
				detail["boxError"] = e.Error()
			}
		}
		if items, e := cameraTailCandidates(tail.env, tail.source); e == nil {
			var names []string
			for i, item := range items {
				if i == 4 {
					break
				}
				names = append(names, item.relative)
			}
			detail["latestFiles"] = names
		}
		data, _ := json.Marshal(detail)
		cameraH264DiagnosticMu.Lock()
		defer cameraH264DiagnosticMu.Unlock()
		_ = os.WriteFile("/run/np1-control/camera-h264-"+tail.source+".json", data, 0600)
		filename := "/run/np1-control/camera-h264-history-" + tail.source + ".json"
		var history []json.RawMessage
		if previous, err := os.ReadFile(filename); err == nil && len(previous) <= 64<<10 {
			_ = json.Unmarshal(previous, &history)
		}
		history = append(history, json.RawMessage(data))
		if len(history) > 8 {
			history = history[len(history)-8:]
		}
		if data, err := json.Marshal(history); err == nil {
			_ = os.WriteFile(filename, data, 0600)
		}
	}
	for {
		// Config.Fragment bounds each batch. Tail reads are 64KiB, but unusually
		// small frames can yield more AUs, so split before creating fragments.
		for len(samples) > 0 {
			n := len(samples)
			if n > 120 {
				n = 120
			}
			fragment, err := tail.config.Fragment(sequence, dts, samples[:n])
			if err != nil {
				finish("fragment", err)
				return
			}
			if err := write(fragment); err != nil {
				finish("client-write", err)
				return
			}
			sequence++
			dts += uint64(n) * uint64(tail.config.SampleDuration)
			samples = samples[n:]
		}
		samples, err = tail.Next(ctx)
		if err != nil {
			finish("tail", err)
			return
		}
	}
}
