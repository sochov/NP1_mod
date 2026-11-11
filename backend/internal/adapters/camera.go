package adapters

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"os"
	"time"
)

func Snapshot(source, mode string) ([]byte, error) {
	p, e := CameraPath(source)
	if e != nil {
		return nil, e
	}
	f, e := os.Open(p)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if time.Since(st.ModTime()) > 15*time.Second {
		return nil, errors.New("STALE camera export")
	}
	raw := make([]byte, 1920*1080*3/2)
	if _, e = io.ReadFull(f, raw); e != nil {
		return nil, e
	}
	// Always reopen the atomically replaced export. Do not acquire a camera device.
	out := image.NewRGBA(image.Rect(0, 0, 640, 360))
	for y := 0; y < 360; y++ {
		for x := 0; x < 640; x++ {
			sx, sy := x*3, y*3
			i := 1920*1080 + (sy/2)*1920 + (sx/2)*2
			r, g, b := limitedRGB(raw[sy*1920+sx], raw[i], raw[i+1])
			p := y*out.Stride + x*4
			out.Pix[p] = r
			out.Pix[p+1] = g
			out.Pix[p+2] = b
			out.Pix[p+3] = 255
		}
	}
	if mode == "auto" {
		balanceHighlights(out)
	}
	var buf bytes.Buffer
	e = jpeg.Encode(&buf, out, &jpeg.Options{Quality: 72})
	return buf.Bytes(), e
}
func clip(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// Camera NV12 is video-range YCbCr. Full-range JPEG conversion lifts blacks and mis-scales chroma.
func limitedRGB(y, u, v uint8) (uint8, uint8, uint8) {
	c, d, e := int(y)-16, int(u)-128, int(v)-128
	return clip((298*c + 409*e + 128) >> 8), clip((298*c - 100*d - 208*e + 128) >> 8), clip((298*c + 516*d + 128) >> 8)
}

// Conservative highlight gray-world correction, preview only. Never modifies source/SD video.
func balanceHighlights(out *image.RGBA) {
	var sums [3]float64
	count := 0
	for p := 0; p < len(out.Pix); p += 16 {
		r, g, b := int(out.Pix[p]), int(out.Pix[p+1]), int(out.Pix[p+2])
		hi, lo := max(r, g, b), min(r, g, b)
		if hi < 160 || hi > 248 || lo < 80 || hi-lo > 90 {
			continue
		}
		sums[0] += float64(r)
		sums[1] += float64(g)
		sums[2] += float64(b)
		count++
	}
	if count < 100 {
		return
	}
	target := (sums[0] + sums[1] + sums[2]) / 3
	gains := [3]float64{}
	for i := range gains {
		gains[i] = max(.8, min(1.3, target/sums[i]))
	}
	for p := 0; p < len(out.Pix); p += 4 {
		for i := 0; i < 3; i++ {
			out.Pix[p+i] = clip(int(float64(out.Pix[p+i])*gains[i] + .5))
		}
	}
}
