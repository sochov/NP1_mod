// Read only newly appended video from the stock recorder's front-camera file.
package platesensor

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"os"
	"regexp"
	"time"

	"github.com/np1-control-center/ncc/internal/h264tail"
)

const frontVideoDirectory = "/mnt/sdcard/DRIVERECDATA/Video"

var frontRecordingName = regexp.MustCompile(`^FILEM_[0-9]{6}_[0-9]{6}_[0-9]{4}\.MP4$`)

type liveRecording struct {
	file          *os.File
	offset        int64
	demux         *h264tail.Demuxer
	observed      time.Time // previous live-edge observation
	skipPredicted bool      // a skipped reference invalidates P frames until the next IDR
}

// The first access unit of the growing mdat contains AUD, SPS and PPS.
// Reading its parameter sets avoids depending on a previous finalized file
// (for example, the first recording on an empty card). No image is replayed.
func recordingDemux(f *os.File, start int64) (*h264tail.Demuxer, error) {
	var aud [6]byte
	if _, err := f.ReadAt(aud[:], start); err != nil || aud != [6]byte{0, 0, 0, 2, 9, 0x10} {
		return nil, h264tail.ErrUnsupported
	}
	pos := start + 6
	var sets []byte
	for _, kind := range []byte{7, 8} {
		var prefix [4]byte
		if _, err := f.ReadAt(prefix[:], pos); err != nil {
			return nil, err
		}
		n := binary.BigEndian.Uint32(prefix[:])
		if n == 0 || n > 1024 {
			return nil, h264tail.ErrUnsupported
		}
		nal := make([]byte, n)
		if _, err := f.ReadAt(nal, pos+4); err != nil {
			return nil, err
		}
		if nal[0]&31 != kind {
			return nil, h264tail.ErrUnsupported
		}
		sets = append(sets, 0, 0, 0, 1)
		sets = append(sets, nal...)
		pos += 4 + int64(n)
	}
	// Only the SPS/PPS and slice validation are used here. Frames are delivered
	// on file growth, not scheduled on this nominal front-camera frame rate.
	config, err := h264tail.ConfigFromAnnexB(sets, 27000)
	if err != nil {
		return nil, err
	}
	if config.Width != FrameWidth || config.Height != FrameHeight {
		return nil, h264tail.ErrUnsupported
	}
	return h264tail.NewDemuxer(config)
}

func openLiveRecording(dir string, now time.Time) (*liveRecording, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, err
	}
	// ReadDir is sorted by filename. FILEM uses the local recording start time.
	count := 0
	for i := len(entries) - 1; i >= 0 && count < 12; i-- {
		entry := entries[i]
		if !frontRecordingName.MatchString(entry.Name()) || !entry.Type().IsRegular() {
			continue
		}
		count++
		info, err := entry.Info()
		if err != nil {
			continue
		}
		age := now.Sub(info.ModTime())
		if age > 4*time.Second || age < -2*time.Second {
			continue
		}
		f, err := root.Open(entry.Name())
		if err != nil {
			continue
		}
		current, err := f.Stat()
		if err == nil && os.SameFile(info, current) && current.Mode().IsRegular() {
			start, _, growing, err := h264tail.MediaDataRange(f, current.Size())
			if err == nil && growing {
				demux, err := recordingDemux(f, start)
				if err == nil {
					return &liveRecording{file: f, offset: current.Size(), demux: demux, observed: now}, nil
				}
			}
		}
		f.Close()
	}
	return nil, errors.New("no growing front recording")
}

func annexSample(data []byte) error {
	// Feed returns dedicated, validated AVCC sample storage. Reuse its length
	// prefixes in place instead of copying every compressed video frame again.
	for pos := 0; pos < len(data); {
		if len(data)-pos < 4 {
			return h264tail.ErrUnsupported
		}
		n := binary.BigEndian.Uint32(data[pos:])
		if n == 0 || uint64(n) > uint64(len(data)-pos-4) {
			return h264tail.ErrUnsupported
		}
		copy(data[pos:pos+4], []byte{0, 0, 0, 1})
		pos += 4 + int(n)
	}
	return nil
}

func (r *liveRecording) readGrowth(ctx context.Context, dst io.Writer, buf []byte, checked time.Time) (bool, error) {
	info, err := r.file.Stat()
	if err != nil {
		return false, err
	}
	start, end, growing, err := h264tail.MediaDataRange(r.file, info.Size())
	if err != nil {
		return false, err
	}
	if !growing {
		// Reopen at the live edge of the new file.
		return false, io.EOF
	}
	if end < r.offset {
		return false, errors.New("front recording discontinuity")
	}
	advanced := end > r.offset
	oldest := r.observed
	r.observed = checked
	if advanced && (end-r.offset > 4<<20 || checked.Sub(oldest) >= cameraFrameWait) {
		return r.seekLiveEdge(start)
	}
	for r.offset < end {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if time.Since(checked) >= cameraFrameWait {
			return r.seekLiveEdge(start)
		}
		n, err := r.file.ReadAt(buf[:min(int64(len(buf)), end-r.offset)], r.offset)
		if err != nil {
			return false, err
		}
		r.offset += int64(n)
		samples, err := r.demux.Feed(buf[:n])
		if err != nil {
			return false, err
		}
		for _, sample := range samples {
			// All stock P pictures are references. When the decoder is falling
			// behind, drop the rest of this GOP. Sending a later P before
			// the next IDR would reference a frame we never decoded.
			if sample.Keyframe {
				r.skipPredicted = false
			} else if time.Since(oldest) > 300*time.Millisecond {
				r.skipPredicted = true
			}
			if r.skipPredicted {
				continue
			}
			if err := annexSample(sample.Data); err != nil {
				return false, err
			}
			if _, err := io.Copy(dst, bytes.NewReader(sample.Data)); err != nil {
				return false, err
			}
		}
	}
	return advanced, nil
}

// A delayed reader keeps the same decoder process. Discard the demuxer's
// pending AU as well as its reference chain, then wait for a newly written IDR.
func (r *liveRecording) seekLiveEdge(start int64) (bool, error) {
	info, err := r.file.Stat()
	if err != nil {
		return false, err
	}
	demux, err := recordingDemux(r.file, start)
	if err != nil {
		return false, err
	}
	r.offset, r.demux, r.observed = info.Size(), demux, time.Now()
	r.skipPredicted = false
	return true, nil
}

func streamFrontRecording(ctx context.Context, dst io.Writer) error {
	buf := make([]byte, 128<<10)
	ticker := time.NewTicker(75 * time.Millisecond)
	defer ticker.Stop()
	for ctx.Err() == nil {
		file, err := openLiveRecording(frontVideoDirectory, time.Now())
		if err != nil {
			return err
		}
		lastGrowth := time.Now()
		for err == nil {
			select {
			case <-ctx.Done():
				err = ctx.Err()
			case <-ticker.C:
				// A queued timer tick can be seconds older than the SD stat below.
				checked := time.Now()
				var advanced bool
				advanced, err = file.readGrowth(ctx, dst, buf, checked)
				if advanced {
					lastGrowth = file.observed
				} else if err == nil && checked.Sub(lastGrowth) >= cameraFrameWait {
					err = errors.New("front recording stopped")
				}
			}
		}
		file.file.Close()
		if !errors.Is(err, io.EOF) {
			return err
		}
	}
	return ctx.Err()
}
