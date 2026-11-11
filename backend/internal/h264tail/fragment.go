package h264tail

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// Codec returns the RFC 6381 AVC codec string for the confirmed configuration.
func (c Config) Codec() (string, error) {
	confirmed, err := c.validated()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("avc1.%02x%02x%02x", confirmed.AVC[1], confirmed.AVC[2], confirmed.AVC[3]), nil
}

func be32(v uint32) []byte { b := make([]byte, 4); binary.BigEndian.PutUint32(b, v); return b }
func join(parts ...[]byte) []byte {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	b := make([]byte, 0, n)
	for _, p := range parts {
		b = append(b, p...)
	}
	return b
}
func atom(kind string, parts ...[]byte) []byte {
	payload := join(parts...)
	return join(be32(uint32(len(payload)+8)), []byte(kind), payload)
}
func full(kind string, version byte, flags uint32, parts ...[]byte) []byte {
	return atom(kind, append([]byte{version, byte(flags >> 16), byte(flags >> 8), byte(flags)}, join(parts...)...))
}
func zeros(n int) []byte { return make([]byte, n) }
func matrix() []byte {
	return join(be32(0x10000), be32(0), be32(0), be32(0), be32(0x10000), be32(0), be32(0), be32(0), be32(0x40000000))
}

// InitSegment generates a video-only MSE initialization segment. SPS/PPS are
// copied from the finalized stock recording, with no decoding or encoding.
func (c Config) InitSegment() ([]byte, error) {
	c, err := c.validated()
	if err != nil {
		return nil, err
	}
	ftyp := atom("ftyp", []byte("iso6"), be32(1), []byte("iso6isomavc1mp41"))
	mvhd := full("mvhd", 0, 0, zeros(8), be32(c.Timescale), be32(0), be32(0x10000), []byte{1, 0, 0, 0}, zeros(8), matrix(), zeros(24), be32(2))
	tkhd := full("tkhd", 0, 7, zeros(8), be32(1), be32(0), be32(0), zeros(8), zeros(8), matrix(), be32(uint32(c.Width)<<16), be32(uint32(c.Height)<<16))
	mdhd := full("mdhd", 0, 0, zeros(8), be32(c.Timescale), be32(0), []byte{0x55, 0xc4, 0, 0})
	hdlr := full("hdlr", 0, 0, be32(0), []byte("vide"), zeros(12), []byte("Stock AVC video\x00"))
	visual := make([]byte, 78)
	binary.BigEndian.PutUint16(visual[6:8], 1)
	binary.BigEndian.PutUint16(visual[24:26], c.Width)
	binary.BigEndian.PutUint16(visual[26:28], c.Height)
	binary.BigEndian.PutUint32(visual[28:32], 0x480000)
	binary.BigEndian.PutUint32(visual[32:36], 0x480000)
	binary.BigEndian.PutUint16(visual[40:42], 1)
	binary.BigEndian.PutUint16(visual[74:76], 24)
	binary.BigEndian.PutUint16(visual[76:78], 0xffff)
	stsd := full("stsd", 0, 0, be32(1), atom("avc1", visual, atom("avcC", c.AVC)))
	stbl := atom("stbl", stsd, full("stts", 0, 0, be32(0)), full("stsc", 0, 0, be32(0)), full("stsz", 0, 0, be32(0), be32(0)), full("stco", 0, 0, be32(0)))
	dinf := atom("dinf", full("dref", 0, 0, be32(1), full("url ", 0, 1)))
	minf := atom("minf", full("vmhd", 0, 1, zeros(8)), dinf, stbl)
	trak := atom("trak", tkhd, atom("mdia", mdhd, hdlr, minf))
	trex := full("trex", 0, 0, be32(1), be32(1), be32(c.SampleDuration), be32(0), be32(0x01010000))
	return join(ftyp, atom("moov", mvhd, trak, atom("mvex", trex))), nil
}

// Fragment remuxes complete validated access units. baseDTS advances by
// SampleDuration per sample; B pictures/composition offsets are unsupported.
func (c Config) Fragment(sequence uint32, baseDTS uint64, samples []Sample) ([]byte, error) {
	confirmed, err := c.validated()
	if err != nil {
		return nil, err
	}
	c = confirmed
	if sequence == 0 || len(samples) == 0 || len(samples) > 240 || c.Timescale == 0 || c.SampleDuration == 0 {
		return nil, errors.New("invalid AVC fragment request")
	}
	if uint64(len(samples)) > (^uint64(0)-baseDTS)/uint64(c.SampleDuration) {
		return nil, errors.New("AVC decode time overflow")
	}
	var payload, rows []byte
	for _, s := range samples {
		if len(s.Data) < 6 || len(s.Data) > MaxNAL+65536 || len(payload)+len(s.Data) > 8<<20 {
			return nil, errors.New("invalid AVC sample size")
		}
		checked, _, valid := unit(s.Data, c)
		if !valid || !bytes.Equal(checked.Data, s.Data) || checked.Keyframe != s.Keyframe || checked.FrameNumber != s.FrameNumber {
			return nil, errors.New("unvalidated AVC sample in fragment")
		}
		flags := uint32(0x01010000)
		if s.Keyframe {
			flags = 0x02000000
		}
		rows = append(rows, join(be32(c.SampleDuration), be32(uint32(len(s.Data))), be32(flags))...)
		payload = append(payload, s.Data...)
	}
	time := make([]byte, 8)
	binary.BigEndian.PutUint64(time, baseDTS)
	makeMoof := func(offset uint32) []byte {
		return atom("moof", full("mfhd", 0, 0, be32(sequence)), atom("traf", full("tfhd", 0, 0x020000, be32(1)), full("tfdt", 1, 0, time), full("trun", 0, 0x701, be32(uint32(len(samples))), be32(offset), rows)))
	}
	moof := makeMoof(0)
	moof = makeMoof(uint32(len(moof) + 8))
	return join(moof, atom("mdat", payload)), nil
}
