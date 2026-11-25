package aap

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// SimHeadUnit is a minimal head unit used to test the phone side, in unit
// tests and from a PC against the device. It offers one H.264 screen and a
// touch screen and acknowledges every video message.
type SimHeadUnit struct {
	Modes      [][3]int // width, height, fps
	MaxUnacked int
	// OnVideo receives codec configuration (config=true) and frames.
	OnVideo func(config bool, ts time.Duration, data []byte)
	// AckDelay delays acknowledgements (to exercise flow control).
	AckDelay time.Duration
	Logf     func(string, ...any)
	// Phone is what the phone said in service discovery.
	Phone string
	// Chosen is the configuration index the phone started.
	Chosen int
}

const (
	simVideoChannel = 2
	simInputChannel = 3
)

func (h *SimHeadUnit) logf(f string, a ...any) {
	if h.Logf != nil {
		h.Logf(f, a...)
	}
}

// Run drives one session on rw until the phone leaves or ctx ends.
func (h *SimHeadUnit) Run(ctx context.Context, rw io.ReadWriter) error {
	stop := context.AfterFunc(ctx, func() {
		if c, ok := rw.(io.Closer); ok {
			c.Close()
		}
	})
	defer stop()
	l := &link{rw: rw}
	if e := l.sendPlain(msgVersionRequest, []byte{0, 1, 0, 1}); e != nil {
		return e
	}
	ch, t, body, e := l.readMessage()
	if e != nil {
		return e
	}
	if ch != 0 || t != msgVersionResponse || len(body) < 6 || binary.BigEndian.Uint16(body[4:]) != 0 {
		return fmt.Errorf("sim: bad version response %d/%d", ch, t)
	}
	next := func() ([]byte, error) {
		ch, t, body, e := l.readMessage()
		if e != nil {
			return nil, e
		}
		if ch != 0 || t != msgEncapsulatedSSL {
			return nil, fmt.Errorf("sim: expected TLS, got %d/%d", ch, t)
		}
		return body, nil
	}
	send := func(b []byte) error { return l.sendPlain(msgEncapsulatedSSL, b) }
	cfg := &tls.Config{InsecureSkipVerify: true, MaxVersion: tls.VersionTLS12}
	if l.tls, e = handshakeTLS(func(c net.Conn) *tls.Conn { return tls.Client(c, cfg) }, next, send); e != nil {
		return e
	}
	if e = l.sendPlain(msgAuthComplete, []byte{8, 0}); e != nil {
		return e
	}
	h.logf("sim: TLS up")
	acks := make(chan struct{}, 4096)
	ackErr := make(chan error, 1)
	go func() {
		for range acks {
			if h.AckDelay > 0 {
				time.Sleep(h.AckDelay)
			}
			var w pbWriter
			w.int(1, 0)
			w.uint(2, 1)
			if e := l.send(simVideoChannel, mediaAck, w.b, false); e != nil {
				ackErr <- e
				return
			}
		}
	}()
	defer close(acks)
	for {
		select {
		case e := <-ackErr:
			return e
		default:
		}
		ch, t, body, e := l.readMessage()
		if e != nil {
			if errors.Is(e, io.EOF) {
				return nil
			}
			return e
		}
		switch {
		case ch == 0 && t == msgServiceDiscoveryRequest:
			m, _ := pbDecode(body)
			h.Phone = m.str(4) + " / " + m.str(5)
			if e = l.send(0, msgServiceDiscoveryResponse, h.discovery(), false); e != nil {
				return e
			}
		case t == msgChannelOpenRequest && ch != 0:
			var w pbWriter
			w.int(1, 0)
			if e = l.send(ch, msgChannelOpenResponse, w.b, true); e != nil {
				return e
			}
		case ch == simVideoChannel && t == mediaSetup:
			var w pbWriter
			w.uint(1, 2)
			w.uint(2, uint64(max(1, h.MaxUnacked)))
			w.uint(3, 0)
			if e = l.send(ch, mediaConfig, w.b, false); e != nil {
				return e
			}
		case ch == simVideoChannel && t == mediaStart:
			m, _ := pbDecode(body)
			idx, _ := m.uint(2)
			h.Chosen = int(idx)
		case ch == simVideoChannel && t == mediaVideoFocusRequest:
			var w pbWriter
			w.uint(1, 1)
			w.bool(2, false)
			if e = l.send(ch, mediaVideoFocusNotify, w.b, false); e != nil {
				return e
			}
		case ch == simVideoChannel && (t == mediaData || t == mediaCodecConfig):
			if h.OnVideo != nil {
				if t == mediaData && len(body) >= 8 {
					h.OnVideo(false, time.Duration(binary.BigEndian.Uint64(body))*time.Microsecond, body[8:])
				} else {
					h.OnVideo(true, 0, body)
				}
			}
			acks <- struct{}{}
		case ch == simInputChannel && t == inputBindingRequest:
			var w pbWriter
			w.int(1, 0)
			if e = l.send(ch, inputBindingResponse, w.b, false); e != nil {
				return e
			}
		case ch == 0 && t == msgPingRequest:
			m, _ := pbDecode(body)
			ts, _ := m.uint(1)
			var w pbWriter
			w.int(1, int64(ts))
			if e = l.send(0, msgPingResponse, w.b, false); e != nil {
				return e
			}
		case ch == 0 && t == msgByeByeRequest:
			_ = l.send(0, msgByeByeResponse, nil, false)
			return nil
		}
	}
}

// SendTouch sends one tap to the phone. (Used by tests through a second link.)
func touchReport(action, x, y int) []byte {
	var p, te, r pbWriter
	p.uint(1, uint64(x))
	p.uint(2, uint64(y))
	p.uint(3, 0)
	te.bytes(1, p.b)
	te.uint(2, 0)
	te.uint(3, uint64(action))
	r.uint(1, uint64(time.Now().UnixMicro()))
	r.bytes(3, te.b)
	return r.b
}

func (h *SimHeadUnit) discovery() []byte {
	codes := map[[2]int]uint64{{800, 480}: 1, {1280, 720}: 2, {1920, 1080}: 3}
	var sink pbWriter
	sink.uint(1, 3)
	sink.uint(2, 0) // real head units send audio_type NONE on the video sink
	modes := h.Modes
	if len(modes) == 0 {
		modes = [][3]int{{800, 480, 30}}
	}
	for _, m := range modes {
		var vc pbWriter
		vc.uint(1, codes[[2]int{m[0], m[1]}])
		if m[2] == 60 {
			vc.uint(2, 1)
		} else {
			vc.uint(2, 2)
		}
		vc.uint(3, 0)
		vc.uint(4, 0)
		vc.uint(5, 160)
		sink.bytes(4, vc.b)
	}
	var video, input, ts pbWriter
	video.uint(1, simVideoChannel)
	video.bytes(3, sink.b)
	ts.uint(1, uint64(modes[0][0]))
	ts.uint(2, uint64(modes[0][1]))
	var in pbWriter
	in.bytes(2, ts.b)
	input.uint(1, simInputChannel)
	input.bytes(4, in.b)
	// An audio sink listed before the video one, as cars do.
	var audioCfg, audioSink, audio pbWriter
	audioCfg.uint(1, 48000)
	audioCfg.uint(2, 16)
	audioCfg.uint(3, 2)
	audioSink.uint(1, 1)
	audioSink.uint(2, 3)
	audioSink.bytes(3, audioCfg.b)
	audio.uint(1, 4)
	audio.bytes(3, audioSink.b)
	var r pbWriter
	r.bytes(1, audio.b)
	r.bytes(1, video.b)
	r.bytes(1, input.b)
	r.string(2, "Sim")
	r.string(3, "Bench")
	r.string(4, "2026")
	r.string(5, "sim-1")
	r.uint(6, 0)
	r.string(7, "NP1CC")
	r.string(8, "SimHeadUnit")
	r.string(9, "1")
	r.string(10, "1.0")
	r.bool(11, false)
	return r.b
}
