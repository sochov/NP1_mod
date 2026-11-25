package androidauto

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/np1-control-center/ncc/internal/androidauto/aap"
	"github.com/np1-control-center/ncc/internal/androidauto/render"
)

const (
	cameraW, cameraH = 1920, 1080
	cameraBytes      = cameraW * cameraH * 3 / 2
)

// cameraReader reads the stock export only when it was replaced, and then only
// the rows the scaler samples.
type cameraReader struct {
	path   string
	ranges [][2]int
	buf    []byte
	mtime  int64
	stamp  [2]int64
	at     time.Time
}

// next returns a new frame, nil when the export did not change.
func (c *cameraReader) next(now time.Time) []byte {
	info, err := os.Stat(c.path)
	if err != nil {
		return nil
	}
	// The producer replaces the file atomically, so a new frame has a new mtime.
	mt := info.ModTime().UnixNano()
	if mt == c.mtime {
		return nil
	}
	f, err := os.Open(c.path)
	if err != nil {
		return nil
	}
	defer f.Close()
	if c.buf == nil {
		c.buf = make([]byte, cameraBytes+16)
	}
	if c.ranges == nil {
		if _, err = io.ReadFull(f, c.buf); err != nil {
			return nil
		}
	} else {
		for _, r := range c.ranges {
			if _, err = f.ReadAt(c.buf[r[0]:r[1]], int64(r[0])); err != nil {
				return nil
			}
		}
		if _, err = f.ReadAt(c.buf[cameraBytes:], cameraBytes); err != nil {
			return nil
		}
	}
	c.mtime = mt
	stamp := [2]int64{int64(binary.LittleEndian.Uint64(c.buf[cameraBytes:])), int64(binary.LittleEndian.Uint64(c.buf[cameraBytes+8:]))}
	if stamp == c.stamp && stamp != ([2]int64{}) {
		return nil
	}
	c.stamp, c.at = stamp, now
	return c.buf[:cameraBytes]
}

// project streams the dashboard until the session ends.
func (s *Service) project(ctx context.Context, sess *aap.Session, st Settings) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-sess.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	vc := sess.Chosen
	// Lay out in logical pixels (square on the screen); a head unit with
	// non-square pixels gets the picture stretched back to the video size.
	lw, lh := render.LogicalSize(vc.Width, vc.Height, vc.PixelAspectE4)
	layout := render.NewLayout(lw, lh, vc.MarginW*lw/vc.Width, vc.MarginH*lh/vc.Height)
	var tileReady atomic.Bool
	s.tiles.OnReady = func() { tileReady.Store(true) }
	dash := render.NewDashboard(layout, s.tiles, st.MapZoom)
	var stretch *render.Stretcher
	if lw != vc.Width || lh != vc.Height {
		stretch = render.NewStretcher(dash.Frame(), vc.Width, vc.Height)
		s.cfg.Logf("android auto: laying out %dx%d for %dx%d (pixel aspect %.3f)", lw, lh, vc.Width, vc.Height, float64(vc.PixelAspectE4)/10000)
	}
	src := NewSource(s.cfg.APIBase)
	go src.Run(ctx)
	cam := &cameraReader{path: s.cfg.CameraPath, ranges: dash.CameraRanges()}
	fps := min(st.FPS, vc.FPS)
	// The bitrate setting is for 800x480; larger pictures get proportionally more.
	bitrate := max(500, min(st.BitrateKbps*vc.Width*vc.Height/(800*480), 16000))

	var (
		enc        *Encoder
		encGen     = -1
		senderWG   sync.WaitGroup
		sendErr    = make(chan error, 1)
		start      = time.Now()
		stats      StreamStats
		statMu     sync.Mutex
		window     []time.Time
		bytesWin   []int
		renderSum  time.Duration
		renders    int
		encodeSum  time.Duration
		encodes    int
		lastSubmit time.Time
	)
	stats.Width, stats.Height = vc.Width, vc.Height
	stopEncoder := func() {
		if enc != nil {
			_ = enc.Close()
			senderWG.Wait()
			enc = nil
		}
	}
	defer stopEncoder()
	startEncoder := func() error {
		e, err := StartEncoder(EncoderConfig{Width: vc.Width, Height: vc.Height, FPS: fps, BitrateKbps: bitrate, IDRSeconds: 2, Launch: s.cfg.Encoder})
		if err != nil {
			return err
		}
		enc = e
		senderWG.Add(1)
		go func() {
			defer senderWG.Done()
			configured := false
			for au := range e.Out() {
				if !configured {
					cfg, _ := splitConfig(au.Data)
					if cfg == nil || !au.Key {
						continue
					}
					if err := sess.SendCodecConfig(ctx, cfg); err != nil {
						select {
						case sendErr <- err:
						default:
						}
						return
					}
					configured = true
				}
				_, data := splitConfig(au.Data)
				if err := sess.SendFrame(ctx, time.Since(start), data); err != nil {
					select {
					case sendErr <- err:
					default:
					}
					return
				}
				now := time.Now()
				statMu.Lock()
				if !au.At.IsZero() {
					encodeSum += now.Sub(au.At)
					encodes++
				}
				window = append(window, now)
				bytesWin = append(bytesWin, len(data))
				for len(window) > 0 && now.Sub(window[0]) > 3*time.Second {
					window, bytesWin = window[1:], bytesWin[1:]
				}
				statMu.Unlock()
			}
		}()
		return nil
	}

	tick := time.NewTicker(time.Second / time.Duration(fps))
	defer tick.Stop()
	statTick := time.NewTicker(time.Second)
	defer statTick.Stop()
	for {
		select {
		case <-ctx.Done():
			if err := sess.Err(); err != nil {
				return err
			}
			return ctx.Err()
		case err := <-sendErr:
			return err
		case <-statTick.C:
			ss := sess.Stats()
			statMu.Lock()
			stats.Frames, stats.Acks, stats.Stalls, stats.Touches, stats.MaxUnacked = ss.Frames, ss.Acks, ss.Stalls, ss.Touches, ss.MaxUnacked
			if n := len(window); n > 1 {
				span := window[n-1].Sub(window[0]).Seconds()
				total := 0
				for _, b := range bytesWin[1:] {
					total += b
				}
				if span > 0 {
					stats.FPS = float64(n-1) / span
					stats.Kbps = float64(total*8) / span / 1000
				}
			}
			if renders > 0 {
				stats.RenderMs = float64(renderSum.Microseconds()) / float64(renders) / 1000
				renderSum, renders = 0, 0
			}
			if encodes > 0 {
				stats.EncodeMs = float64(encodeSum.Microseconds()) / float64(encodes) / 1000
				encodeSum, encodes = 0, 0
			}
			snapshot := stats
			statMu.Unlock()
			focused, _ := sess.Focus()
			s.mu.Lock()
			s.status.Stream = &snapshot
			s.mu.Unlock()
			if focused {
				s.setState("projecting", "")
			} else {
				s.setState("background", "ヘッドユニットが他の画面を表示しています")
			}
			continue
		case <-tick.C:
		}
		focused, gen := sess.Focus()
		if !focused {
			stopEncoder()
			continue
		}
		if enc == nil || gen != encGen {
			// A new focus needs a key frame first: restart the encoder.
			stopEncoder()
			if err := startEncoder(); err != nil {
				return err
			}
			encGen = gen
		}
		t0 := time.Now()
		state := src.State()
		state.Dark = darkTheme(st.Theme, state.Position, t0)
		dash.SetState(state, t0)
		if tileReady.Swap(false) {
			dash.MarkMapDirty()
		}
		camFrame := cam.next(t0)
		dash.Camera(camFrame, t0)
		changed := dash.Compose(ctx, t0) || camFrame != nil
		// An unchanged picture is not encoded again, but a frame still goes
		// out every half second so the head unit sees a live stream.
		if !changed && t0.Sub(lastSubmit) < 500*time.Millisecond {
			continue
		}
		statMu.Lock()
		renderSum += time.Since(t0)
		renders++
		statMu.Unlock()
		// Never queue: a late frame is worse than a skipped one.
		if enc.Pending() > 1 || !sess.Window() {
			statMu.Lock()
			stats.Dropped++
			statMu.Unlock()
			continue
		}
		lastSubmit = t0
		out := dash.Frame()
		if stretch != nil {
			out = stretch.Frame()
		}
		if err := enc.Write(out.Bytes(), time.Now()); err != nil {
			if isClosed(err) || errors.Is(err, os.ErrClosed) {
				return errors.New("encoder stopped: " + err.Error())
			}
			return err
		}
	}
}

// darkTheme decides the theme; "auto" follows the sun at the position (or
// 18:00–6:00 in Japan without one).
func darkTheme(theme string, pos *render.LatLon, now time.Time) bool {
	switch theme {
	case "dark":
		return true
	case "light":
		return false
	}
	if pos == nil {
		h := now.In(japan).Hour()
		return h >= 18 || h < 6
	}
	return sunElevation(pos.Lat, pos.Lon, now) < -3
}

// sunElevation is the solar elevation in degrees (NOAA approximation).
func sunElevation(lat, lon float64, t time.Time) float64 {
	t = t.UTC()
	day := float64(t.YearDay())
	hour := float64(t.Hour()) + float64(t.Minute())/60 + float64(t.Second())/3600
	g := 2 * math.Pi / 365 * (day - 1 + (hour-12)/24)
	decl := 0.006918 - 0.399912*math.Cos(g) + 0.070257*math.Sin(g) - 0.006758*math.Cos(2*g) + 0.000907*math.Sin(2*g) - 0.002697*math.Cos(3*g) + 0.00148*math.Sin(3*g)
	eqt := 229.18 * (0.000075 + 0.001868*math.Cos(g) - 0.032077*math.Sin(g) - 0.014615*math.Cos(2*g) - 0.040849*math.Sin(2*g))
	tst := hour*60 + eqt + 4*lon
	ha := (tst/4 - 180) * math.Pi / 180
	la := lat * math.Pi / 180
	cosZ := math.Sin(la)*math.Sin(decl) + math.Cos(la)*math.Cos(decl)*math.Cos(ha)
	return 90 - math.Acos(math.Max(-1, math.Min(1, cosZ)))*180/math.Pi
}
