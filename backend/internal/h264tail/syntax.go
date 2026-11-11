package h264tail

import "errors"

var errSyntax = errors.New("invalid or unsupported AVC syntax")

type bits struct {
	data   []byte
	at     int
	failed bool
}

func (b *bits) read(n int) uint32 {
	if n < 0 || n > 32 || b.at+n > len(b.data)*8 {
		b.failed = true
		return 0
	}
	var out uint32
	for i := 0; i < n; i++ {
		out = out<<1 | uint32((b.data[b.at/8]>>uint(7-b.at%8))&1)
		b.at++
	}
	return out
}
func (b *bits) ue() uint32 {
	zeros := 0
	for !b.failed && b.read(1) == 0 {
		zeros++
		if zeros > 24 {
			b.failed = true
			return 0
		}
	}
	if b.failed {
		return 0
	}
	return (1 << uint(zeros)) - 1 + b.read(zeros)
}
func (b *bits) se() int32 {
	n := b.ue()
	if n&1 != 0 {
		return int32(n/2 + 1)
	}
	return -int32(n / 2)
}
func rbsp(nal []byte) ([]byte, error) {
	// The stock writer retains a few Annex-B trailing_zero_8bits in its final
	// AVCC slice. They are outside rbsp_trailing_bits, not forbidden interior
	// start-code bytes. Validate the syntax without them, preserve original Data.
	padding := 0
	for len(nal) > 1 && nal[len(nal)-1] == 0 {
		padding++
		if padding > 32 {
			return nil, errSyntax
		}
		nal = nal[:len(nal)-1]
	}
	if len(nal) < 2 || nal[0]&0x80 != 0 {
		return nil, errSyntax
	}
	out := make([]byte, 0, len(nal)-1)
	zero := 0
	for i := 1; i < len(nal); i++ {
		v := nal[i]
		if zero >= 2 {
			if v == 3 {
				if i+1 >= len(nal) || nal[i+1] > 3 {
					return nil, errSyntax
				}
				zero = 0
				continue
			}
			if v <= 2 {
				return nil, errSyntax
			}
		}
		out = append(out, v)
		if v == 0 {
			zero++
		} else {
			zero = 0
		}
	}
	return out, nil
}

type sequenceInfo struct{ id, frameBits, pocBits, pocType, width, height, mbs uint32 }
type pictureInfo struct {
	id                uint32
	bottom, redundant bool
}

func parseSequence(nal []byte) (sequenceInfo, error) {
	var s sequenceInfo
	if len(nal) < 5 || nal[0] != 0x67 {
		return s, errSyntax
	}
	r, err := rbsp(nal)
	if err != nil {
		return s, err
	}
	b := bits{data: r}
	profile := b.read(8)
	constraints := b.read(8)
	level := b.read(8)
	s.id = b.ue()
	if profile != 66 || constraints&3 != 0 || level > 52 || s.id > 31 {
		return s, errSyntax
	}
	s.frameBits = b.ue() + 4
	if s.frameBits > 16 {
		return s, errSyntax
	}
	s.pocType = b.ue()
	switch s.pocType {
	case 0:
		s.pocBits = b.ue() + 4
		if s.pocBits > 16 {
			return s, errSyntax
		}
	case 2:
	default:
		return s, errSyntax
	}
	if b.ue() > 16 {
		return s, errSyntax
	}
	b.read(1)
	w, h := b.ue()+1, b.ue()+1
	if w > 512 || h > 512 || b.read(1) != 1 {
		return s, errSyntax
	}
	b.read(1)
	var left, right, top, bottom uint32
	if b.read(1) != 0 {
		left = b.ue()
		right = b.ue()
		top = b.ue()
		bottom = b.ue()
	}
	if b.failed || left+right >= w*8 || top+bottom >= h*8 {
		return s, errSyntax
	}
	s.width = w*16 - 2*(left+right)
	s.height = h*16 - 2*(top+bottom)
	s.mbs = w * h
	return s, nil
}
func parsePicture(nal []byte, s sequenceInfo) (pictureInfo, error) {
	var p pictureInfo
	if len(nal) < 2 || nal[0] != 0x68 {
		return p, errSyntax
	}
	r, err := rbsp(nal)
	if err != nil {
		return p, err
	}
	b := bits{data: r}
	p.id = b.ue()
	sid := b.ue()
	if p.id > 255 || sid != s.id || b.read(1) != 0 {
		return p, errSyntax
	}
	p.bottom = b.read(1) != 0
	if b.ue() != 0 || b.ue() > 31 || b.ue() > 31 {
		return p, errSyntax
	}
	b.read(1)
	b.read(2)
	b.se()
	b.se()
	b.se()
	b.read(1)
	b.read(1)
	p.redundant = b.read(1) != 0
	if b.failed {
		return p, errSyntax
	}
	return p, nil
}

type sliceInfo struct {
	first, frame, poc, idrID uint32
	key                      bool
}

func parseSlice(nal []byte, c Config) (sliceInfo, error) {
	var s sliceInfo
	kind := nal[0] & 31
	if (kind != 1 && kind != 5) || nal[0]&0x60 == 0 {
		return s, errSyntax
	}
	r, err := rbsp(nal)
	if err != nil {
		return s, err
	}
	b := bits{data: r}
	s.first = b.ue()
	typ := b.ue()
	if s.first >= c.sequence.mbs || typ > 9 || (typ%5 != 0 && typ%5 != 2) || b.ue() != c.picture.id {
		return s, errSyntax
	}
	s.frame = b.read(int(c.sequence.frameBits))
	s.key = kind == 5
	if s.key {
		if typ%5 != 2 || s.frame != 0 {
			return s, errSyntax
		}
		s.idrID = b.ue()
		if s.idrID > 65535 {
			return s, errSyntax
		}
	}
	if c.sequence.pocType == 0 {
		s.poc = b.read(int(c.sequence.pocBits))
		if c.picture.bottom {
			b.se()
		}
	}
	if c.picture.redundant && b.ue() != 0 {
		return s, errSyntax
	}
	if b.failed {
		return s, errSyntax
	}
	return s, nil
}
