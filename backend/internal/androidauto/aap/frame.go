package aap

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Frame header flags.
const (
	flagFirst     = 0x01
	flagLast      = 0x02
	flagControl   = 0x04 // a control-type message on a service channel
	flagEncrypted = 0x08
	flagSingle    = flagFirst | flagLast
)

// maxFragment is the largest plaintext carried by one frame. One frame then
// holds exactly one TLS record (16 KiB is the TLS plaintext limit).
const maxFragment = 16384

// Channel 0 is the control channel; the others are opened by service id.
const controlChannel = 0

// Control channel message types.
const (
	msgVersionRequest           = 1
	msgVersionResponse          = 2
	msgEncapsulatedSSL          = 3
	msgAuthComplete             = 4
	msgServiceDiscoveryRequest  = 5
	msgServiceDiscoveryResponse = 6
	msgChannelOpenRequest       = 7
	msgChannelOpenResponse      = 8
	msgChannelClose             = 9
	msgPingRequest              = 11
	msgPingResponse             = 12
	msgNavFocusRequest          = 13
	msgNavFocusNotification     = 14
	msgByeByeRequest            = 15
	msgByeByeResponse           = 16
	msgVoiceSession             = 17
	msgAudioFocusRequest        = 18
	msgAudioFocusNotification   = 19
)

// Media (video/audio) channel message types.
const (
	mediaData              = 0x0000
	mediaCodecConfig       = 0x0001
	mediaSetup             = 0x8000
	mediaStart             = 0x8001
	mediaStop              = 0x8002
	mediaConfig            = 0x8003
	mediaAck               = 0x8004
	mediaVideoFocusRequest = 0x8007
	mediaVideoFocusNotify  = 0x8008
)

// Input channel message types.
const (
	inputEvent           = 0x8001
	inputBindingRequest  = 0x8002
	inputBindingResponse = 0x8003
)

// isControlType reports whether a message type is a control-channel type,
// which carries the control flag when sent on a service channel.
func isControlType(t uint16) bool { return t >= 1 && t <= 26 }

type rawFrame struct {
	Channel byte
	Flags   byte
	Total   uint32 // declared plaintext total of a fragmented message (first fragment only)
	Payload []byte
}

func readFrame(r io.Reader) (rawFrame, error) {
	var h [4]byte
	if _, e := io.ReadFull(r, h[:]); e != nil {
		return rawFrame{}, e
	}
	f := rawFrame{Channel: h[0], Flags: h[1]}
	n := int(binary.BigEndian.Uint16(h[2:]))
	if f.Flags&flagFirst != 0 && f.Flags&flagLast == 0 {
		var t [4]byte
		if _, e := io.ReadFull(r, t[:]); e != nil {
			return rawFrame{}, e
		}
		f.Total = binary.BigEndian.Uint32(t[:])
	}
	f.Payload = make([]byte, n)
	if _, e := io.ReadFull(r, f.Payload); e != nil {
		return rawFrame{}, e
	}
	return f, nil
}

func appendFrame(dst []byte, ch, flags byte, total int, payload []byte) ([]byte, error) {
	if len(payload) > 0xffff {
		return dst, fmt.Errorf("aap: frame payload %d bytes too large", len(payload))
	}
	dst = append(dst, ch, flags, byte(len(payload)>>8), byte(len(payload)))
	if flags&flagFirst != 0 && flags&flagLast == 0 {
		if total < 0 {
			return dst, errors.New("aap: fragmented message without total")
		}
		dst = binary.BigEndian.AppendUint32(dst, uint32(total))
	}
	return append(dst, payload...), nil
}

// fragmentFlags returns the position flags of fragment i of n.
func fragmentFlags(i, n int) byte {
	var f byte
	if i == 0 {
		f |= flagFirst
	}
	if i == n-1 {
		f |= flagLast
	}
	return f
}

// assembler joins the fragments of a message per channel.
type assembler struct {
	parts map[byte][]byte
}

// add takes one decrypted fragment and returns the whole message once the last
// fragment arrived.
func (a *assembler) add(ch, flags byte, plain []byte) ([]byte, bool, error) {
	if a.parts == nil {
		a.parts = map[byte][]byte{}
	}
	first, last := flags&flagFirst != 0, flags&flagLast != 0
	switch {
	case first && last:
		delete(a.parts, ch)
		return plain, true, nil
	case first:
		a.parts[ch] = append([]byte(nil), plain...)
		return nil, false, nil
	}
	buf, ok := a.parts[ch]
	if !ok {
		return nil, false, fmt.Errorf("aap: fragment without start on channel %d", ch)
	}
	buf = append(buf, plain...)
	if len(buf) > 8<<20 {
		delete(a.parts, ch)
		return nil, false, errors.New("aap: message too large")
	}
	if last {
		delete(a.parts, ch)
		return buf, true, nil
	}
	a.parts[ch] = buf
	return nil, false, nil
}
