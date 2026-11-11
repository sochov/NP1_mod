package adapters

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

type MP4Metadata struct {
	DurationSeconds float64 `json:"durationSeconds"`
	VideoCodec      string  `json:"videoCodec,omitempty"`
	AudioCodec      string  `json:"audioCodec,omitempty"`
	Width           int     `json:"width,omitempty"`
	Height          int     `json:"height,omitempty"`
	AudioChannels   int     `json:"audioChannels,omitempty"`
	AudioSampleRate int     `json:"audioSampleRate,omitempty"`
}
type mp4Box struct {
	kind                string
	start, payload, end int64
}
type mp4Track struct {
	kind, codec                         string
	width, height, channels, sampleRate int
}

func readMP4Box(r io.ReaderAt, offset, limit int64) (mp4Box, error) {
	if offset < 0 || limit-offset < 8 {
		return mp4Box{}, io.ErrUnexpectedEOF
	}
	var h [16]byte
	if _, e := r.ReadAt(h[:8], offset); e != nil {
		return mp4Box{}, e
	}
	size := uint64(binary.BigEndian.Uint32(h[:4]))
	header := int64(8)
	if size == 1 {
		if limit-offset < 16 {
			return mp4Box{}, io.ErrUnexpectedEOF
		}
		if _, e := r.ReadAt(h[8:], offset+8); e != nil {
			return mp4Box{}, e
		}
		size = binary.BigEndian.Uint64(h[8:])
		header = 16
	}
	if size == 0 {
		size = uint64(limit - offset)
	}
	if size < uint64(header) || size > uint64(limit-offset) || size > math.MaxInt64 {
		return mp4Box{}, errors.New("invalid MP4 box boundary")
	}
	return mp4Box{string(h[4:8]), offset, offset + header, offset + int64(size)}, nil
}
func readMP4Bytes(r io.ReaderAt, b mp4Box, n int) ([]byte, error) {
	if n < 0 || int64(n) > b.end-b.payload {
		return nil, io.ErrUnexpectedEOF
	}
	v := make([]byte, n)
	_, e := r.ReadAt(v, b.payload)
	return v, e
}
func mp4MovieDuration(r io.ReaderAt, b mp4Box) (float64, error) {
	h, e := readMP4Bytes(r, b, 4)
	if e != nil {
		return 0, e
	}
	var scale uint32
	var duration uint64
	switch h[0] {
	case 0:
		v, e := readMP4Bytes(r, b, 20)
		if e != nil {
			return 0, e
		}
		scale = binary.BigEndian.Uint32(v[12:16])
		duration = uint64(binary.BigEndian.Uint32(v[16:20]))
	case 1:
		v, e := readMP4Bytes(r, b, 32)
		if e != nil {
			return 0, e
		}
		scale = binary.BigEndian.Uint32(v[20:24])
		duration = binary.BigEndian.Uint64(v[24:32])
	default:
		return 0, errors.New("unknown mvhd version")
	}
	if scale == 0 {
		return 0, errors.New("invalid MP4 timescale")
	}
	seconds := float64(duration) / float64(scale)
	if !math.IsInf(seconds, 0) && seconds > 0 && seconds <= 86400 {
		return seconds, nil
	}
	return 0, errors.New("invalid recording duration")
}
func parseMP4Container(r io.ReaderAt, start, end int64, depth int, track *mp4Track, meta *MP4Metadata, budget *int) error {
	if depth > 6 {
		return errors.New("MP4 nesting limit")
	}
	for offset := start; offset < end; {
		*budget--
		if *budget < 0 {
			return errors.New("MP4 box count limit")
		}
		b, e := readMP4Box(r, offset, end)
		if e != nil {
			return e
		}
		offset = b.end
		switch b.kind {
		case "mvhd":
			if depth == 1 {
				meta.DurationSeconds, e = mp4MovieDuration(r, b)
				if e != nil {
					return e
				}
			}
		case "trak":
			if depth == 1 {
				t := &mp4Track{}
				if e = parseMP4Container(r, b.payload, b.end, depth+1, t, meta, budget); e != nil {
					return e
				}
				switch t.kind {
				case "vide":
					if meta.VideoCodec == "" {
						meta.VideoCodec = t.codec
						meta.Width = t.width
						meta.Height = t.height
					}
				case "soun":
					if meta.AudioCodec == "" {
						meta.AudioCodec = t.codec
						meta.AudioChannels = t.channels
						meta.AudioSampleRate = t.sampleRate
					}
				}
			}
		case "mdia", "minf", "stbl":
			if track != nil {
				if e = parseMP4Container(r, b.payload, b.end, depth+1, track, meta, budget); e != nil {
					return e
				}
			}
		case "hdlr":
			if track != nil {
				v, e := readMP4Bytes(r, b, 12)
				if e != nil {
					return e
				}
				track.kind = string(v[8:12])
			}
		case "stsd":
			if track != nil {
				v, e := readMP4Bytes(r, b, 8)
				if e != nil {
					return e
				}
				count := binary.BigEndian.Uint32(v[4:])
				if count == 0 || count > 32 {
					return errors.New("invalid sample-description count")
				}
				sample, e := readMP4Box(r, b.payload+8, b.end)
				if e != nil {
					return e
				}
				track.codec = sample.kind
				switch sample.kind {
				case "avc1", "avc3", "hvc1", "hev1", "mp4v":
					v, e := readMP4Bytes(r, mp4Box{payload: sample.start, end: sample.end}, 36)
					if e != nil {
						return e
					}
					track.width = int(binary.BigEndian.Uint16(v[32:34]))
					track.height = int(binary.BigEndian.Uint16(v[34:36]))
					if track.width < 1 || track.width > 8192 || track.height < 1 || track.height > 8192 {
						return errors.New("invalid video dimensions")
					}
				case "mp4a", "ac-3", "ec-3", "Opus", "alac":
					v, e := readMP4Bytes(r, mp4Box{payload: sample.start, end: sample.end}, 36)
					if e != nil {
						return e
					}
					track.channels = int(binary.BigEndian.Uint16(v[24:26]))
					track.sampleRate = int(binary.BigEndian.Uint32(v[32:36]) >> 16)
				}
			}
		}
	}
	return nil
}

// ReadRecordingMetadata examines bounded MP4 box headers and metadata, never decodes video.
// A finalized moov, finite movie duration and video sample description are required.
func ReadRecordingMetadata(r io.ReaderAt, size int64) (MP4Metadata, error) {
	var meta MP4Metadata
	if size < 24 {
		return meta, errors.New("recording not finalized")
	}
	hasFTYP := false
	hasMOOV := false
	budget := 2048
	for offset := int64(0); offset < size; {
		budget--
		if budget < 0 {
			return meta, errors.New("MP4 box count limit")
		}
		b, e := readMP4Box(r, offset, size)
		if e != nil {
			return meta, e
		}
		offset = b.end
		switch b.kind {
		case "ftyp":
			hasFTYP = true
		case "moov":
			hasMOOV = true
			if e = parseMP4Container(r, b.payload, b.end, 1, nil, &meta, &budget); e != nil {
				return meta, e
			}
		}
	}
	if !hasFTYP || !hasMOOV || meta.DurationSeconds <= 0 || meta.VideoCodec == "" {
		return meta, fmt.Errorf("recording not finalized or unsupported metadata")
	}
	return meta, nil
}
