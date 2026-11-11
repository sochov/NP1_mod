// Package h264tail reuses AVCC video already written by the stock recorder.
// It never opens devices, creates an encoder, or reads another process's heap.
package h264tail

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

var ErrUnsupported = errors.New("unsupported stock AVC recording structure")

type Config struct {
	AVC            []byte
	Timescale      uint32
	SampleDuration uint32
	Width          uint16
	Height         uint16
	sps            []byte
	pps            []byte
	sequence       sequenceInfo
	picture        pictureInfo
}

type box struct {
	kind         string
	payload, end int64
}

func readBox(r io.ReaderAt, at, end int64) (box, error) {
	if at < 0 || end-at < 8 {
		return box{}, io.ErrUnexpectedEOF
	}
	var b [16]byte
	if _, err := r.ReadAt(b[:8], at); err != nil {
		return box{}, err
	}
	size := uint64(binary.BigEndian.Uint32(b[:4]))
	head := int64(8)
	if size == 1 {
		if end-at < 16 {
			return box{}, io.ErrUnexpectedEOF
		}
		if _, err := r.ReadAt(b[8:], at+8); err != nil {
			return box{}, err
		}
		size = binary.BigEndian.Uint64(b[8:])
		head = 16
	}
	if size == 0 {
		size = uint64(end - at)
	}
	if size < uint64(head) || size > uint64(end-at) {
		return box{}, ErrUnsupported
	}
	return box{string(b[4:8]), at + head, at + int64(size)}, nil
}
func body(r io.ReaderAt, b box, max int) ([]byte, error) {
	n := b.end - b.payload
	if n < 0 || n > int64(max) {
		return nil, ErrUnsupported
	}
	v := make([]byte, int(n))
	_, err := r.ReadAt(v, b.payload)
	return v, err
}
func children(r io.ReaderAt, at, end int64) ([]box, error) {
	var result []box
	for at < end {
		if len(result) >= 256 {
			return nil, ErrUnsupported
		}
		b, err := readBox(r, at, end)
		if err != nil {
			return nil, err
		}
		result = append(result, b)
		at = b.end
	}
	return result, nil
}
func find(items []box, kind string) (box, bool) {
	var found box
	count := 0
	for _, b := range items {
		if b.kind == kind {
			found = b
			count++
		}
	}
	return found, count == 1
}

// MediaDataOffset locates only the existing top-level mdat payload.
func MediaDataOffset(r io.ReaderAt, size int64) (int64, error) {
	start, _, _, err := MediaDataRange(r, size)
	return start, err
}

// MediaDataRange excludes a finalized file's moov/index trailer. For a growing
// size-zero mdat, end is the observed physical size and openEnded is true.
func MediaDataRange(r io.ReaderAt, size int64) (start, end int64, openEnded bool, err error) {
	for at, n := int64(0), 0; at < size && n < 16; n++ {
		b, err := readBox(r, at, size)
		if err != nil {
			return 0, 0, false, err
		}
		if b.kind == "mdat" {
			var header [4]byte
			if _, err := r.ReadAt(header[:], at); err != nil {
				return 0, 0, false, err
			}
			return b.payload, b.end, binary.BigEndian.Uint32(header[:]) == 0, nil
		}
		at = b.end
	}
	return 0, 0, false, ErrUnsupported
}

// ConfigFromMP4 reads bounded metadata from a finalized recording, skipping mdat.
// Only one AVC video track, constant frame duration and no composition offsets
// are accepted; AAC and all audio metadata are intentionally excluded.
func ConfigFromMP4(r io.ReaderAt, size int64) (Config, error) {
	var result Config
	top, err := children(r, 0, size)
	if err != nil {
		return result, err
	}
	moov, ok := find(top, "moov")
	if !ok || moov.end-moov.payload > 4<<20 {
		return result, ErrUnsupported
	}
	items, err := children(r, moov.payload, moov.end)
	if err != nil {
		return result, err
	}
	found := false
	for _, track := range items {
		if track.kind != "trak" {
			continue
		}
		t, err := children(r, track.payload, track.end)
		if err != nil {
			return result, err
		}
		mdia, ok := find(t, "mdia")
		if !ok {
			continue
		}
		m, err := children(r, mdia.payload, mdia.end)
		if err != nil {
			return result, err
		}
		handler, ok := find(m, "hdlr")
		if !ok {
			return result, ErrUnsupported
		}
		h, err := body(r, handler, 256)
		if err != nil || len(h) < 12 {
			return result, ErrUnsupported
		}
		if string(h[8:12]) != "vide" {
			continue
		}
		if found {
			return result, ErrUnsupported
		}
		found = true
		mdhd, ok := find(m, "mdhd")
		if !ok {
			return result, ErrUnsupported
		}
		clock, err := body(r, mdhd, 64)
		if err != nil || len(clock) < 20 {
			return result, ErrUnsupported
		}
		switch clock[0] {
		case 0:
			result.Timescale = binary.BigEndian.Uint32(clock[12:16])
		case 1:
			if len(clock) < 32 {
				return result, ErrUnsupported
			}
			result.Timescale = binary.BigEndian.Uint32(clock[20:24])
		default:
			return result, ErrUnsupported
		}
		minf, ok := find(m, "minf")
		if !ok {
			return result, ErrUnsupported
		}
		mi, err := children(r, minf.payload, minf.end)
		if err != nil {
			return result, err
		}
		stbl, ok := find(mi, "stbl")
		if !ok {
			return result, ErrUnsupported
		}
		st, err := children(r, stbl.payload, stbl.end)
		if err != nil {
			return result, err
		}
		for _, b := range st {
			if b.kind == "ctts" {
				return result, ErrUnsupported
			}
		}
		stts, ok := find(st, "stts")
		if !ok {
			return result, ErrUnsupported
		}
		timing, err := body(r, stts, 8192)
		if err != nil || len(timing) < 16 || binary.BigEndian.Uint32(timing[:4]) != 0 {
			return result, ErrUnsupported
		}
		count := binary.BigEndian.Uint32(timing[4:8])
		if count == 0 || count > 1023 || len(timing) != 8+int(count)*8 {
			return result, ErrUnsupported
		}
		for i := 0; i < int(count); i++ {
			n := binary.BigEndian.Uint32(timing[8+i*8:])
			delta := binary.BigEndian.Uint32(timing[12+i*8:])
			if n == 0 || delta == 0 || (result.SampleDuration != 0 && delta != result.SampleDuration) {
				return result, ErrUnsupported
			}
			result.SampleDuration = delta
		}
		stsd, ok := find(st, "stsd")
		if !ok {
			return result, ErrUnsupported
		}
		entryHeader, err := body(r, stsd, 16384)
		if err != nil || len(entryHeader) < 8 || binary.BigEndian.Uint32(entryHeader[:4]) != 0 || binary.BigEndian.Uint32(entryHeader[4:8]) != 1 {
			return result, ErrUnsupported
		}
		entry, err := readBox(r, stsd.payload+8, stsd.end)
		if err != nil || entry.kind != "avc1" || entry.end != stsd.end || entry.end-entry.payload < 78 {
			return result, ErrUnsupported
		}
		visual := entryHeader[16:]
		result.Width = binary.BigEndian.Uint16(visual[24:26])
		result.Height = binary.BigEndian.Uint16(visual[26:28])
		sampleChildren, err := children(r, entry.payload+78, entry.end)
		if err != nil {
			return result, err
		}
		avc, ok := find(sampleChildren, "avcC")
		if !ok {
			return result, ErrUnsupported
		}
		result.AVC, err = body(r, avc, 4096)
		if err != nil {
			return result, err
		}
	}
	if !found {
		return result, ErrUnsupported
	}
	return result.validated()
}

func (c Config) validated() (Config, error) {
	if c.Timescale == 0 || c.Timescale > 10000000 || c.SampleDuration == 0 || uint64(c.Timescale) < uint64(c.SampleDuration) || uint64(c.Timescale) > uint64(c.SampleDuration)*120 {
		return Config{}, ErrUnsupported
	}
	b := c.AVC
	if len(b) < 11 || b[0] != 1 || b[1] != 66 || b[4] != 0xff || b[5] != 0xe1 {
		return Config{}, ErrUnsupported
	}
	n := int(binary.BigEndian.Uint16(b[6:8]))
	if n < 4 || n > 1024 || len(b) < 8+n+3 || b[8+n] != 1 {
		return Config{}, ErrUnsupported
	}
	c.sps = append([]byte(nil), b[8:8+n]...)
	if !bytes.Equal(b[1:4], c.sps[1:4]) {
		return Config{}, ErrUnsupported
	}
	at := 9 + n
	pn := int(binary.BigEndian.Uint16(b[at : at+2]))
	at += 2
	if pn < 2 || pn > 1024 || at+pn != len(b) {
		return Config{}, ErrUnsupported
	}
	c.pps = append([]byte(nil), b[at:]...)
	sequence, err := parseSequence(c.sps)
	if err != nil {
		return Config{}, err
	}
	picture, err := parsePicture(c.pps, sequence)
	if err != nil {
		return Config{}, err
	}
	if sequence.width != uint32(c.Width) || sequence.height != uint32(c.Height) || c.Width == 0 || c.Height == 0 {
		return Config{}, ErrUnsupported
	}
	c.sequence = sequence
	c.picture = picture
	c.AVC = bytes.Clone(c.AVC)
	return c, nil
}
