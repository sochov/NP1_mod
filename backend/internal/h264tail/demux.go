package h264tail

import (
	"bytes"
	"encoding/binary"
	"errors"
)

const MaxNAL = 2 << 20
const maxBuffered = 4 << 20

type Sample struct {
	Data        []byte
	Keyframe    bool
	FrameNumber uint32
}
type Demuxer struct {
	config   Config
	buffered []byte
	locked   bool
	previous sliceInfo
	Dropped  uint64
}

func NewDemuxer(c Config) (*Demuxer, error) {
	c, err := c.validated()
	if err != nil {
		return nil, err
	}
	return &Demuxer{config: c}, nil
}
func audAt(b []byte, from int) int {
	needle := []byte{0, 0, 0, 2, 9}
	for from+6 <= len(b) {
		at := bytes.Index(b[from:], needle)
		if at < 0 {
			return -1
		}
		at += from
		if at+6 > len(b) {
			return -1
		}
		if b[at+5]&31 == 16 {
			return at
		}
		from = at + 1
	}
	return -1
}
func seiValid(nal []byte) bool {
	r, err := rbsp(nal)
	if err != nil || len(r) < 1 {
		return false
	}
	at := 0
	for at < len(r)-1 {
		for at < len(r) && r[at] == 255 {
			at++
		}
		if at >= len(r)-1 {
			return false
		}
		at++
		size := 0
		for at < len(r) && r[at] == 255 {
			size += 255
			at++
			if size > 65536 {
				return false
			}
		}
		if at >= len(r)-1 {
			return false
		}
		size += int(r[at])
		at++
		if size > len(r)-at-1 {
			return false
		}
		at += size
	}
	return at == len(r)-1 && r[at] == 128
}

// unit accepts only a coherent AVC access-unit prefix. Bytes after the final
// NAL may be AAC samples; they are never copied into the video sample.
func unit(b []byte, c Config) (Sample, sliceInfo, bool) {
	var sample Sample
	var picture sliceInfo
	if len(b) < 6 || audAt(b, 0) != 0 {
		return sample, picture, false
	}
	sample.Data = append(sample.Data, b[:6]...)
	at := 6
	sawSlice, sawSPS, sawPPS := false, false, false
	for at+5 <= len(b) {
		n := int(binary.BigEndian.Uint32(b[at : at+4]))
		if n < 1 || n > MaxNAL {
			break
		}
		nalType := b[at+4] & 31
		if b[at+4]&0x80 != 0 || (nalType != 1 && nalType != 5 && nalType != 6 && nalType != 7 && nalType != 8) {
			break
		}
		if n > len(b)-at-4 {
			return Sample{}, picture, false
		}
		nal := b[at+4 : at+4+n]
		switch nalType {
		case 7:
			if sawSlice || sawSPS || !bytes.Equal(nal, c.sps) {
				return Sample{}, picture, false
			}
			sawSPS = true
		case 8:
			if sawSlice || sawPPS || !bytes.Equal(nal, c.pps) {
				return Sample{}, picture, false
			}
			sawPPS = true
		case 6:
			if n > 65536 || !seiValid(nal) {
				return Sample{}, picture, false
			}
		case 1, 5:
			s, err := parseSlice(nal, c)
			if err != nil {
				return Sample{}, picture, false
			}
			if !sawSlice {
				if s.first != 0 {
					return Sample{}, picture, false
				}
				picture = s
				sawSlice = true
			} else {
				if s.first <= picture.first || s.frame != picture.frame || s.poc != picture.poc || s.key != picture.key || s.idrID != picture.idrID {
					return Sample{}, picture, false
				}
				picture.first = s.first
			}
		}
		sample.Data = append(sample.Data, b[at:at+4+n]...)
		at += 4 + n
	}
	if !sawSlice || (picture.key && (!sawSPS || !sawPPS)) {
		return Sample{}, picture, false
	}
	sample.Keyframe = picture.key
	sample.FrameNumber = picture.frame
	return sample, picture, true
}

// Feed is incremental: the trailing access unit is retained until the next
// verified AUD delimiter. Bad candidates/AAC-looking bytes cause resync, not
// length-only packet acceptance. A discontinuity requires a fresh SPS/PPS+IDR.
func (d *Demuxer) Feed(data []byte) ([]Sample, error) {
	if len(data) > maxBuffered {
		return nil, errors.New("AVC tail input exceeds bounded chunk")
	}
	if len(d.buffered)+len(data) > maxBuffered {
		d.buffered = nil
		d.locked = false
		d.Dropped++
	}
	d.buffered = append(d.buffered, data...)
	var samples []Sample
	for {
		start := audAt(d.buffered, 0)
		if start < 0 {
			if len(d.buffered) > 5 {
				d.buffered = bytes.Clone(d.buffered[len(d.buffered)-5:])
			}
			break
		}
		if start > 0 {
			d.buffered = d.buffered[start:]
		}
		end := audAt(d.buffered, 6)
		if end < 0 {
			break
		}
		sample, picture, ok := unit(d.buffered[:end], d.config)
		if ok && !picture.key {
			mask := uint32(1<<d.config.sequence.frameBits) - 1
			ok = d.locked && picture.frame == (d.previous.frame+1)&mask
			if ok && d.config.sequence.pocType == 0 {
				pocMask := uint32(1<<d.config.sequence.pocBits) - 1
				ok = picture.poc == (d.previous.poc+2)&pocMask
			}
		}
		if ok {
			d.locked = true
			d.previous = picture
			samples = append(samples, sample)
		} else {
			d.locked = false
			d.Dropped++
		}
		d.buffered = d.buffered[end:]
	}
	// Release large consumed backing arrays instead of retaining a file chunk.
	if cap(d.buffered) > len(d.buffered)+65536 {
		d.buffered = bytes.Clone(d.buffered)
	}
	return samples, nil
}
