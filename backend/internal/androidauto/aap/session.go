package aap

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// VideoConfig is one video mode a head unit offers.
type VideoConfig struct {
	Index   int `json:"index"`
	Width   int `json:"width"`
	Height  int `json:"height"`
	FPS     int `json:"fps"`
	MarginW int `json:"marginWidth"`
	MarginH int `json:"marginHeight"`
	Density int `json:"density"`
	// PixelAspectE4 is the displayed width/height of one video pixel x 10000:
	// head units that stretch a standard picture over an odd-shaped screen
	// (e.g. 1280x720 over 1600x600) report it instead of margins.
	PixelAspectE4 int  `json:"pixelAspectE4"`
	H264          bool `json:"h264"`
	Supported     bool `json:"supported"`
}

// HeadUnit is what the head unit told us about itself.
type HeadUnit struct {
	Make         string        `json:"make"`
	Model        string        `json:"model"`
	Year         string        `json:"year"`
	HeadUnitMake string        `json:"headUnitMake"`
	HeadUnitName string        `json:"headUnitModel"`
	Software     string        `json:"software"`
	DisplayName  string        `json:"displayName"`
	Video        []VideoConfig `json:"video"`
	TouchWidth   int           `json:"touchWidth"`
	TouchHeight  int           `json:"touchHeight"`

	videoChannel int
	inputChannel int
}

// Touch is one touch report from the head unit, in touch-screen coordinates.
type Touch struct {
	Action int // 0 down, 1 up, 2 move
	X, Y   int
}

// Options configures the phone side.
type Options struct {
	TLS        *tls.Config
	PhoneName  string
	PhoneBrand string
	// The largest picture we are willing to encode. The biggest offered mode
	// that fits is chosen.
	MaxWidth, MaxHeight int
	Prefer60            bool
	HandshakeTimeout    time.Duration
	Logf                func(format string, args ...any)
	OnTouch             func(Touch)
}

// link is one framed, optionally encrypted connection.
type link struct {
	rw  io.ReadWriter
	tls *tlsLayer
	asm assembler
	wmu sync.Mutex
}

// Session is one connected head unit.
type Session struct {
	link
	opts Options

	HU     HeadUnit
	Chosen VideoConfig

	mu         sync.Mutex
	cond       *sync.Cond
	maxUnacked int
	unacked    int
	focused    bool
	focusGen   int
	closed     bool
	err        error
	done       chan struct{}
	sessionID  int32
	inputOpen  bool
	stats      Stats
}

// Stats counts what was exchanged.
type Stats struct {
	Frames     uint64 `json:"frames"`
	Bytes      uint64 `json:"bytes"`
	Acks       uint64 `json:"acks"`
	Touches    uint64 `json:"touches"`
	Stalls     uint64 `json:"stalls"`
	MaxUnacked int    `json:"maxUnacked"`
}

// ackStall is how long a full window may wait before it is reopened.
const ackStall = 3 * time.Second

var (
	// ErrNoVideo means the head unit offers no H.264 picture we can produce.
	ErrNoVideo = errors.New("aap: head unit offers no usable H.264 video mode")
	errClosed  = errors.New("aap: session closed")
)

func (s *Session) logf(format string, args ...any) {
	if s.opts.Logf != nil {
		s.opts.Logf(format, args...)
	}
}

// Connect runs the phone side of the handshake on an established transport
// (TCP or a Bluetooth/WiFi socket) and opens the video channel. The head unit
// speaks first.
func Connect(ctx context.Context, rw io.ReadWriter, opts Options) (*Session, error) {
	if opts.TLS == nil {
		return nil, errors.New("aap: no TLS configuration")
	}
	if opts.MaxWidth == 0 {
		opts.MaxWidth, opts.MaxHeight = 800, 480
	}
	if opts.HandshakeTimeout == 0 {
		opts.HandshakeTimeout = 20 * time.Second
	}
	s := &Session{link: link{rw: rw}, opts: opts, done: make(chan struct{})}
	s.cond = sync.NewCond(&s.mu)
	if c, ok := rw.(interface{ SetDeadline(time.Time) error }); ok {
		_ = c.SetDeadline(time.Now().Add(opts.HandshakeTimeout))
		defer c.SetDeadline(time.Time{})
	}
	stop := context.AfterFunc(ctx, func() {
		if c, ok := rw.(io.Closer); ok {
			c.Close()
		}
	})
	defer stop()
	if e := s.handshake(); e != nil {
		return nil, e
	}
	if e := s.discover(); e != nil {
		return nil, e
	}
	if e := s.openVideo(); e != nil {
		return nil, e
	}
	go s.readLoop()
	go s.pingLoop()
	return s, nil
}

func (s *link) writeRaw(b []byte) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, e := s.rw.Write(b)
	return e
}

// sendPlain sends an unencrypted control message (version and TLS handshake).
func (s *link) sendPlain(t uint16, body []byte) error {
	p := binary.BigEndian.AppendUint16(nil, t)
	p = append(p, body...)
	f, e := appendFrame(nil, controlChannel, flagSingle, -1, p)
	if e != nil {
		return e
	}
	return s.writeRaw(f)
}

// send encrypts and sends one message, fragmenting it as needed.
func (s *link) send(ch byte, t uint16, body []byte, control bool) error {
	plain := binary.BigEndian.AppendUint16(make([]byte, 0, len(body)+2), t)
	plain = append(plain, body...)
	n := (len(plain) + maxFragment - 1) / maxFragment
	if n == 0 {
		n = 1
	}
	var extra byte = flagEncrypted
	if control && ch != controlChannel {
		extra |= flagControl
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	out := make([]byte, 0, len(plain)+n*40)
	for i := 0; i < n; i++ {
		end := min((i+1)*maxFragment, len(plain))
		rec, e := s.tls.encrypt(plain[i*maxFragment : end])
		if e != nil {
			return e
		}
		out, e = appendFrame(out, ch, fragmentFlags(i, n)|extra, len(plain), rec)
		if e != nil {
			return e
		}
	}
	_, e := s.rw.Write(out)
	return e
}

// readMessage returns the next whole message. Before TLS is up only plain
// frames are accepted.
func (s *link) readMessage() (byte, uint16, []byte, error) {
	for {
		f, e := readFrame(s.rw)
		if e != nil {
			return 0, 0, nil, e
		}
		payload := f.Payload
		if f.Flags&flagEncrypted != 0 {
			if s.tls == nil {
				return 0, 0, nil, errors.New("aap: encrypted frame before TLS")
			}
			if payload, e = s.tls.decrypt(payload); e != nil {
				return 0, 0, nil, fmt.Errorf("aap: decrypt: %w", e)
			}
		}
		msg, whole, e := s.asm.add(f.Channel, f.Flags, payload)
		if e != nil {
			return 0, 0, nil, e
		}
		if !whole {
			continue
		}
		if len(msg) < 2 {
			return 0, 0, nil, errors.New("aap: short message")
		}
		return f.Channel, binary.BigEndian.Uint16(msg), msg[2:], nil
	}
}

func (s *Session) handshake() error {
	ch, t, body, e := s.readMessage()
	if e != nil {
		return fmt.Errorf("aap: waiting for version request: %w", e)
	}
	if ch != controlChannel || t != msgVersionRequest || len(body) < 4 {
		return fmt.Errorf("aap: expected version request, got channel %d type %d", ch, t)
	}
	major, minor := binary.BigEndian.Uint16(body), binary.BigEndian.Uint16(body[2:])
	s.logf("head unit protocol %d.%d", major, minor)
	resp := binary.BigEndian.AppendUint16(nil, major)
	resp = binary.BigEndian.AppendUint16(resp, minor)
	resp = binary.BigEndian.AppendUint16(resp, 0) // version match
	if e = s.sendPlain(msgVersionResponse, resp); e != nil {
		return e
	}
	next := func() ([]byte, error) {
		ch, t, body, e := s.readMessage()
		if e != nil {
			return nil, e
		}
		if ch != controlChannel || t != msgEncapsulatedSSL {
			return nil, fmt.Errorf("aap: expected TLS handshake, got channel %d type %d", ch, t)
		}
		return body, nil
	}
	sendTLS := func(b []byte) error {
		for len(b) > 0 {
			n := min(len(b), maxFragment)
			if e := s.sendPlain(msgEncapsulatedSSL, b[:n]); e != nil {
				return e
			}
			b = b[n:]
		}
		return nil
	}
	if s.tls, e = handshakeTLS(func(c net.Conn) *tls.Conn { return tls.Server(c, s.opts.TLS) }, next, sendTLS); e != nil {
		return fmt.Errorf("aap: TLS handshake: %w", e)
	}
	// Some head units flush their TLS engine once more after the handshake,
	// which arrives as a further (usually empty) handshake message.
	for i := 0; ; i++ {
		ch, t, body, e = s.readMessage()
		if e != nil {
			return fmt.Errorf("aap: waiting for auth complete: %w", e)
		}
		if ch != controlChannel || t != msgEncapsulatedSSL || i >= 4 {
			break
		}
		s.logf("ignoring %d trailing TLS bytes before auth complete", len(body))
	}
	if ch != controlChannel || t != msgAuthComplete {
		return fmt.Errorf("aap: expected auth complete, got channel %d type %d", ch, t)
	}
	if m, e := pbDecode(body); e == nil {
		if st, ok := m.int32(1); ok && st != 0 {
			return fmt.Errorf("aap: head unit rejected authentication (status %d)", st)
		}
	}
	return nil
}

var resolutions = map[uint64][2]int{1: {800, 480}, 2: {1280, 720}, 3: {1920, 1080}, 4: {2560, 1440}, 5: {3840, 2160}, 6: {720, 1280}, 7: {1080, 1920}, 8: {1440, 2560}, 9: {2160, 3840}}

func (s *Session) discover() error {
	var w pbWriter
	w.string(4, s.opts.PhoneName)
	w.string(5, s.opts.PhoneBrand)
	if e := s.send(controlChannel, msgServiceDiscoveryRequest, w.b, false); e != nil {
		return e
	}
	body, e := s.expect(controlChannel, msgServiceDiscoveryResponse)
	if e != nil {
		return e
	}
	hu, e := parseDiscovery(body)
	if e != nil {
		return e
	}
	s.HU = hu
	s.logf("head unit %s %s (%s %s), video modes %s", hu.Make, hu.Model, hu.HeadUnitMake, hu.HeadUnitName, describeModes(hu.Video))
	best := -1
	for i, v := range hu.Video {
		if !v.Supported || v.Width > s.opts.MaxWidth || v.Height > s.opts.MaxHeight {
			continue
		}
		if best < 0 {
			best = i
			continue
		}
		b := hu.Video[best]
		area, bArea := v.Width*v.Height, b.Width*b.Height
		if area > bArea || (area == bArea && (v.FPS == 60) == s.opts.Prefer60 && (b.FPS == 60) != s.opts.Prefer60) {
			best = i
		}
	}
	if best < 0 || hu.videoChannel == 0 {
		return ErrNoVideo
	}
	s.Chosen = hu.Video[best]
	return nil
}

func describeModes(v []VideoConfig) string {
	out := ""
	for _, c := range v {
		codec := "h264"
		if !c.H264 {
			codec = "other"
		}
		out += fmt.Sprintf("[%dx%d@%d %s margin %dx%d dpi %d par %.4f] ", c.Width, c.Height, c.FPS, codec, c.MarginW, c.MarginH, c.Density, float64(c.PixelAspectE4)/10000)
	}
	if out == "" {
		return "none"
	}
	return out
}

func parseDiscovery(body []byte) (HeadUnit, error) {
	m, e := pbDecode(body)
	if e != nil {
		return HeadUnit{}, e
	}
	hu := HeadUnit{Make: m.str(2), Model: m.str(3), Year: m.str(4), HeadUnitMake: m.str(7), HeadUnitName: m.str(8), Software: m.str(10), DisplayName: m.str(14)}
	for _, raw := range m.all(1) {
		svc, e := pbDecode(raw)
		if e != nil {
			continue
		}
		id, _ := svc.uint(1)
		if sink, ok := svc.sub(3); ok && hu.videoChannel == 0 {
			typ, _ := sink.uint(1)
			// A video sink may still carry audio_type = NONE; only a real
			// stream type or audio configurations make it an audio sink.
			audioType, _ := sink.uint(2)
			isAudio := audioType != 0 || len(sink.all(3)) > 0
			disp, _ := sink.uint(7)
			configs := sink.all(4)
			if !isAudio && disp == 0 && len(configs) > 0 && (typ == 3 || typ == 7 || typ == 5 || typ == 6) {
				hu.videoChannel = int(id)
				for i, c := range configs {
					vc, e := pbDecode(c)
					if e != nil {
						continue
					}
					res, _ := vc.uint(1)
					rate, _ := vc.uint(2)
					codec, hasCodec := vc.uint(10)
					mw, _ := vc.uint(3)
					mh, _ := vc.uint(4)
					dens, _ := vc.uint(5)
					par, _ := vc.uint(8)
					if par < 2500 || par > 40000 {
						par = 10000
					}
					wh := resolutions[res]
					cfg := VideoConfig{Index: i, Width: wh[0], Height: wh[1], FPS: 30, MarginW: int(clampU32(mw)), MarginH: int(clampU32(mh)), Density: int(clampU32(dens)), PixelAspectE4: int(par)}
					if rate == 1 {
						cfg.FPS = 60
					}
					cfg.H264 = (hasCodec && codec == 3) || (!hasCodec && typ == 3)
					cfg.Supported = cfg.H264 && wh[0] > 0 && wh[0] >= wh[1] && wh[0] <= 1920
					hu.Video = append(hu.Video, cfg)
				}
			}
		}
		if in, ok := svc.sub(4); ok && hu.inputChannel == 0 {
			hu.inputChannel = int(id)
			if ts, ok := in.sub(2); ok {
				w, _ := ts.uint(1)
				h, _ := ts.uint(2)
				hu.TouchWidth, hu.TouchHeight = int(clampU32(w)), int(clampU32(h))
			}
		}
	}
	return hu, nil
}

// expect reads until the wanted message arrives, answering everything else.
func (s *Session) expect(ch byte, t uint16) ([]byte, error) {
	for {
		c, mt, body, e := s.readMessage()
		if e != nil {
			return nil, e
		}
		if c == ch && mt == t {
			return body, nil
		}
		if e = s.dispatch(c, mt, body); e != nil {
			return nil, e
		}
	}
}

func (s *Session) openChannel(ch int) error {
	var w pbWriter
	w.int(1, 0)
	w.int(2, int64(ch))
	if e := s.send(byte(ch), msgChannelOpenRequest, w.b, true); e != nil {
		return e
	}
	body, e := s.expect(byte(ch), msgChannelOpenResponse)
	if e != nil {
		return e
	}
	m, e := pbDecode(body)
	if e != nil {
		return e
	}
	if st, _ := m.int32(1); st != 0 {
		return fmt.Errorf("aap: head unit refused channel %d (status %d)", ch, st)
	}
	return nil
}

func (s *Session) openVideo() error {
	vc := byte(s.HU.videoChannel)
	if e := s.openChannel(s.HU.videoChannel); e != nil {
		return e
	}
	if s.HU.inputChannel != 0 && s.opts.OnTouch != nil {
		if e := s.openChannel(s.HU.inputChannel); e != nil {
			s.logf("input channel not opened: %v", e)
		} else {
			s.inputOpen = true
			if e = s.send(byte(s.HU.inputChannel), inputBindingRequest, nil, false); e != nil {
				return e
			}
		}
	}
	var w pbWriter
	w.uint(1, 3) // H.264 baseline
	if e := s.send(vc, mediaSetup, w.b, false); e != nil {
		return e
	}
	body, e := s.expect(vc, mediaConfig)
	if e != nil {
		return e
	}
	m, e := pbDecode(body)
	if e != nil {
		return e
	}
	maxUnacked, _ := m.uint(2)
	s.mu.Lock()
	s.maxUnacked = max(1, int(min(maxUnacked, 64)))
	s.stats.MaxUnacked = s.maxUnacked
	s.mu.Unlock()
	w = pbWriter{}
	w.int(1, int64(s.sessionID))
	w.uint(2, uint64(s.Chosen.Index))
	if e = s.send(vc, mediaStart, w.b, false); e != nil {
		return e
	}
	return s.RequestFocus()
}

// RequestFocus asks the head unit to show the projection.
func (s *Session) RequestFocus() error {
	var w pbWriter
	w.uint(2, 1) // projected
	w.uint(3, 0)
	return s.send(byte(s.HU.videoChannel), mediaVideoFocusRequest, w.b, false)
}

func (s *Session) dispatch(ch byte, t uint16, body []byte) error {
	if ch == controlChannel {
		switch t {
		case msgPingRequest:
			m, _ := pbDecode(body)
			ts, _ := m.uint(1)
			var w pbWriter
			w.int(1, int64(ts))
			return s.send(controlChannel, msgPingResponse, w.b, false)
		case msgPingResponse:
			return nil
		case msgByeByeRequest:
			_ = s.send(controlChannel, msgByeByeResponse, nil, false)
			return errors.New("aap: head unit ended the session")
		case msgByeByeResponse:
			return errClosed
		case msgNavFocusRequest:
			var w pbWriter
			w.uint(1, 1)
			return s.send(controlChannel, msgNavFocusNotification, w.b, false)
		}
		return nil
	}
	if int(ch) == s.HU.videoChannel {
		switch t {
		case mediaAck:
			m, _ := pbDecode(body)
			n, _ := m.uint(2)
			if n == 0 {
				n = 1
			}
			s.mu.Lock()
			s.unacked = max(0, s.unacked-int(n))
			s.stats.Acks += n
			s.cond.Broadcast()
			s.mu.Unlock()
		case mediaVideoFocusNotify:
			m, _ := pbDecode(body)
			mode, _ := m.uint(1)
			s.mu.Lock()
			f := mode == 1 || mode == 4
			if f != s.focused {
				s.focused = f
				s.focusGen++
				// A head unit that took the screen back drops what was in flight.
				s.unacked = 0
			}
			s.cond.Broadcast()
			s.mu.Unlock()
			s.logf("video focus mode %d", mode)
		case mediaStop:
			s.mu.Lock()
			if s.focused {
				s.focused = false
				s.focusGen++
			}
			s.cond.Broadcast()
			s.mu.Unlock()
		}
		return nil
	}
	if int(ch) == s.HU.inputChannel && t == inputEvent && s.opts.OnTouch != nil {
		m, e := pbDecode(body)
		if e != nil {
			return nil
		}
		te, ok := m.sub(3)
		if !ok {
			return nil
		}
		action, _ := te.uint(3)
		idx, _ := te.uint(2)
		ptrs := te.all(1)
		if int(idx) < len(ptrs) {
			p, _ := pbDecode(ptrs[idx])
			x, _ := p.uint(1)
			y, _ := p.uint(2)
			s.mu.Lock()
			s.stats.Touches++
			s.mu.Unlock()
			s.opts.OnTouch(Touch{Action: int(action), X: int(clampU32(x)), Y: int(clampU32(y))})
		}
	}
	return nil
}

func (s *Session) fail(e error) {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		s.err = e
		close(s.done)
	}
	s.cond.Broadcast()
	s.mu.Unlock()
	if c, ok := s.rw.(io.Closer); ok {
		c.Close()
	}
}

func (s *Session) readLoop() {
	for {
		ch, t, body, e := s.readMessage()
		if e == nil {
			e = s.dispatch(ch, t, body)
		}
		if e != nil {
			s.fail(e)
			return
		}
	}
}

func (s *Session) pingLoop() {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-tick.C:
			var w pbWriter
			w.int(1, time.Now().UnixMicro())
			if e := s.send(controlChannel, msgPingRequest, w.b, false); e != nil {
				s.fail(e)
				return
			}
		}
	}
}

// Done is closed when the session ended.
func (s *Session) Done() <-chan struct{} { return s.done }

// Err is why the session ended.
func (s *Session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Stats returns counters.
func (s *Session) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats
}

// Focus reports whether the head unit shows our picture, and a counter that
// changes whenever that flips (a new focus needs a fresh key frame).
func (s *Session) Focus() (bool, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.focused, s.focusGen
}

// WaitFocus blocks until the head unit shows our picture.
func (s *Session) WaitFocus(ctx context.Context) error {
	stop := context.AfterFunc(ctx, func() { s.mu.Lock(); s.cond.Broadcast(); s.mu.Unlock() })
	defer stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	for !s.focused && !s.closed && ctx.Err() == nil {
		s.cond.Wait()
	}
	if s.closed {
		return s.err
	}
	return ctx.Err()
}

// Window reports whether another frame may be sent without waiting for acks.
func (s *Session) Window() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unacked < s.maxUnacked
}

func (s *Session) reserve(ctx context.Context) error {
	stop := context.AfterFunc(ctx, func() { s.mu.Lock(); s.cond.Broadcast(); s.mu.Unlock() })
	defer stop()
	// A head unit that never acknowledges some message type would otherwise
	// shrink the window for good; after a long stall the window is reopened.
	stalled := false
	t := time.AfterFunc(ackStall, func() { s.mu.Lock(); stalled = true; s.cond.Broadcast(); s.mu.Unlock() })
	defer t.Stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.unacked >= s.maxUnacked && !s.closed && ctx.Err() == nil {
		if stalled {
			s.unacked = 0
			s.stats.Stalls++
			break
		}
		s.cond.Wait()
	}
	if s.closed {
		return errClosed
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.unacked++
	return nil
}

// SendCodecConfig sends SPS and PPS (Annex B).
func (s *Session) SendCodecConfig(ctx context.Context, sps []byte) error {
	if e := s.reserve(ctx); e != nil {
		return e
	}
	return s.send(byte(s.HU.videoChannel), mediaCodecConfig, sps, false)
}

// SendFrame sends one H.264 access unit (Annex B), waiting for the head unit's
// flow-control window.
func (s *Session) SendFrame(ctx context.Context, ts time.Duration, au []byte) error {
	if e := s.reserve(ctx); e != nil {
		return e
	}
	body := binary.BigEndian.AppendUint64(make([]byte, 0, len(au)+8), uint64(ts.Microseconds()))
	body = append(body, au...)
	if e := s.send(byte(s.HU.videoChannel), mediaData, body, false); e != nil {
		return e
	}
	s.mu.Lock()
	s.stats.Frames++
	s.stats.Bytes += uint64(len(au))
	s.mu.Unlock()
	return nil
}

// Close says goodbye to the head unit and ends the session.
func (s *Session) Close() {
	var w pbWriter
	w.uint(1, 1) // user selection
	_ = s.send(controlChannel, msgByeByeRequest, w.b, false)
	select {
	case <-s.done:
	case <-time.After(time.Second):
	}
	s.fail(errClosed)
}
