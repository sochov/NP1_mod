package platesensor

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"os"
)

const (
	FrameWidth  = 1920
	FrameHeight = 1080
	FrameBytes  = FrameWidth * FrameHeight * 3 / 2
	// The stock export appends a 16-byte producer timespec after the image.
	stampBytes = 16
)

// FrontExport is the atomic-replace NV12 export written by stock ImageRecog.
const FrontExport = "/tmp/application/imagerecog/imagedata/frontcamera/imagedata/value"

type Frame struct {
	Data  []byte // NV12, FrameBytes long
	Stamp [2]int64
}

// FrameSource returns the newest frame, or ok=false when it has not changed.
type FrameSource interface {
	Next(buf []byte) (stamp [2]int64, ok bool, err error)
}

// ExportSource reopens the export path for every read because the producer
// replaces the inode; a held descriptor would freeze on the first frame.
type ExportSource struct {
	Path string
	last [2]int64
}

func (s *ExportSource) Next(buf []byte) ([2]int64, bool, error) {
	f, e := os.Open(s.Path)
	if e != nil {
		return [2]int64{}, false, e
	}
	defer f.Close()
	if len(buf) < FrameBytes+stampBytes {
		return [2]int64{}, false, errors.New("buffer too small")
	}
	if _, e = io.ReadFull(f, buf[:FrameBytes+stampBytes]); e != nil {
		return [2]int64{}, false, e
	}
	var stamp [2]int64
	stamp[0] = int64(binary.LittleEndian.Uint64(buf[FrameBytes:]))
	stamp[1] = int64(binary.LittleEndian.Uint64(buf[FrameBytes+8:]))
	if stamp == s.last && stamp != ([2]int64{}) {
		return stamp, false, nil
	}
	s.last = stamp
	return stamp, true, nil
}

func expand(v uint8, lo, span int) uint8 {
	x := (int(v) - lo) * 255 / span
	if x < 0 {
		return 0
	}
	if x > 255 {
		return 255
	}
	return uint8(x)
}

// EncodeJPEG converts a video-range NV12 frame to a full-range JPEG without an
// RGB round trip (JPEG stores YCbCr 4:2:0 natively).
func EncodeJPEG(nv12 []byte, width, height, quality int) ([]byte, error) {
	if width <= 0 || height <= 0 || width%2 != 0 || height%2 != 0 || len(nv12) < width*height*3/2 {
		return nil, errors.New("invalid NV12 frame")
	}
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	var ylut, clut [256]uint8
	for i := range 256 {
		ylut[i] = expand(uint8(i), 16, 219)
		c := 128 + (i-128)*255/224
		clut[i] = uint8(max(0, min(255, c)))
	}
	for i, v := range nv12[:width*height] {
		img.Y[i] = ylut[v]
	}
	uv := nv12[width*height:]
	for i := 0; i < width*height/4; i++ {
		img.Cb[i] = clut[uv[2*i]]
		img.Cr[i] = clut[uv[2*i+1]]
	}
	var out bytes.Buffer
	if e := jpeg.Encode(&out, img, &jpeg.Options{Quality: quality}); e != nil {
		return nil, e
	}
	return out.Bytes(), nil
}
