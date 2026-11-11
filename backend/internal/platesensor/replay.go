package platesensor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

// Replay feeds recorded dash-camera clips through the real sensor and engine
// for verification on a PC. It is never used on the device: the device reads
// the stock camera export (ExportSource) and the stock NMEA (adapters).

type ReplayClip struct {
	Path  string       `json:"path"`
	Start time.Time    `json:"start"`
	GPS   [][3]float64 `json:"gps,omitempty"` // seconds from Start, lat, lon
}

type ReplayManifest struct {
	FPS   float64      `json:"fps"`
	Clips []ReplayClip `json:"clips"`
}

func LoadReplayManifest(path string) (ReplayManifest, error) {
	var m ReplayManifest
	b, e := os.ReadFile(path)
	if e == nil {
		e = json.Unmarshal(b, &m)
	}
	if e == nil && (len(m.Clips) == 0 || m.FPS <= 0) {
		e = errors.New("manifest needs clips and fps")
	}
	return m, e
}

// ReplaySource decodes clips with ffmpeg into 1920x1080 NV12 frames at the
// manifest FPS. Now() follows the recording time of the last frame read.
type ReplaySource struct {
	m     ReplayManifest
	mu    sync.Mutex
	clip  int
	frame int
	cmd   *exec.Cmd
	out   io.ReadCloser
	now   time.Time
	Done  bool
}

func NewReplaySource(m ReplayManifest) *ReplaySource {
	return &ReplaySource{m: m, now: m.Clips[0].Start}
}

func (r *ReplaySource) Now() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.now
}

func (r *ReplaySource) open() error {
	c := r.m.Clips[r.clip]
	r.cmd = exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin", "-i", c.Path, "-map", "0:v:0",
		"-vf", fmt.Sprintf("fps=%s,scale=%d:%d:flags=area", strconv.FormatFloat(r.m.FPS, 'f', -1, 64), FrameWidth, FrameHeight),
		"-pix_fmt", "nv12", "-f", "rawvideo", "-")
	out, e := r.cmd.StdoutPipe()
	if e != nil {
		return e
	}
	r.out, r.frame = out, 0
	return r.cmd.Start()
}

func (r *ReplaySource) Next(buf []byte) ([2]int64, bool, error) {
	for {
		if r.clip >= len(r.m.Clips) {
			r.Done = true
			return [2]int64{}, false, io.EOF
		}
		if r.out == nil {
			if e := r.open(); e != nil {
				r.clip++
				r.out = nil
				continue
			}
		}
		if _, e := io.ReadFull(r.out, buf[:FrameBytes]); e != nil {
			_ = r.cmd.Wait()
			r.out = nil
			r.clip++
			continue
		}
		c := r.m.Clips[r.clip]
		at := c.Start.Add(time.Duration(float64(r.frame) / r.m.FPS * float64(time.Second)))
		r.frame++
		r.mu.Lock()
		r.now = at
		r.mu.Unlock()
		return [2]int64{at.Unix(), int64(at.Nanosecond())}, true, nil
	}
}

// Locate returns the recorded GPS sample nearest to `at` (within 2 s).
func (r *ReplaySource) Locate(_ context.Context, at time.Time) *plate.Location {
	type sample struct {
		t        time.Time
		lat, lon float64
	}
	var best *sample
	bestGap := 2 * time.Second
	for _, c := range r.m.Clips {
		if at.Before(c.Start.Add(-5*time.Second)) || at.After(c.Start.Add(2*time.Minute)) {
			continue
		}
		pts := c.GPS
		i := sort.Search(len(pts), func(i int) bool { return c.Start.Add(time.Duration(pts[i][0] * float64(time.Second))).After(at) })
		for _, j := range []int{i - 1, i} {
			if j < 0 || j >= len(pts) {
				continue
			}
			t := c.Start.Add(time.Duration(pts[j][0] * float64(time.Second)))
			gap := at.Sub(t)
			if gap < 0 {
				gap = -gap
			}
			if gap <= bestGap {
				bestGap = gap
				best = &sample{t, pts[j][1], pts[j][2]}
			}
		}
	}
	if best == nil {
		return nil
	}
	return &plate.Location{At: best.t, Lat: best.lat, Lon: best.lon}
}
