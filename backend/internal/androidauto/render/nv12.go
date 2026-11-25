// Package render draws the projected dashboard straight into NV12 frames.
//
// Only what changed is redrawn: the map and the plate list are drawn as RGBA
// layers when their data changes and converted once; the camera is scaled from
// the stock NV12 export without a colour round trip.
package render

import (
	"image"
	"image/color"
	"math"
)

// Frame is a video-range BT.601 NV12 picture.
type Frame struct {
	W, H int
	Y    []byte
	UV   []byte
}

// NewFrame allocates a frame; width and height must be even.
func NewFrame(w, h int) *Frame {
	w, h = w&^1, h&^1
	buf := make([]byte, w*h*3/2)
	return &Frame{W: w, H: h, Y: buf[:w*h], UV: buf[w*h:]}
}

// Bytes returns the frame in the layout encoders take (Y plane, then UV).
func (f *Frame) Bytes() []byte { return f.Y[:len(f.Y)+len(f.UV)] }

func rgbToYUV(r, g, b int32) (y, u, v byte) {
	y = clamp8(((66*r + 129*g + 25*b + 128) >> 8) + 16)
	u = clamp8(((-38*r - 74*g + 112*b + 128) >> 8) + 128)
	v = clamp8(((112*r - 94*g - 18*b + 128) >> 8) + 128)
	return
}

func clamp8(v int32) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

// Fill paints a rectangle with one colour.
func (f *Frame) Fill(r image.Rectangle, c color.RGBA) {
	r = evenRect(r).Intersect(image.Rect(0, 0, f.W, f.H))
	y, u, v := rgbToYUV(int32(c.R), int32(c.G), int32(c.B))
	for row := r.Min.Y; row < r.Max.Y; row++ {
		line := f.Y[row*f.W+r.Min.X : row*f.W+r.Max.X]
		for i := range line {
			line[i] = y
		}
	}
	for row := r.Min.Y / 2; row < r.Max.Y/2; row++ {
		line := f.UV[row*f.W+r.Min.X : row*f.W+r.Max.X]
		for i := 0; i < len(line); i += 2 {
			line[i], line[i+1] = u, v
		}
	}
}

func evenRect(r image.Rectangle) image.Rectangle {
	return image.Rect(r.Min.X&^1, r.Min.Y&^1, (r.Max.X+1)&^1, (r.Max.Y+1)&^1)
}

// Blit converts an opaque RGBA image into the frame at (x0, y0). Both must be
// even; the image is clipped to the frame.
func (f *Frame) Blit(img *image.RGBA, x0, y0 int) {
	b := img.Bounds()
	w, h := b.Dx()&^1, b.Dy()&^1
	x0, y0 = x0&^1, y0&^1
	if x0+w > f.W {
		w = (f.W - x0) &^ 1
	}
	if y0+h > f.H {
		h = (f.H - y0) &^ 1
	}
	if w <= 0 || h <= 0 {
		return
	}
	for row := 0; row < h; row += 2 {
		s0 := img.Pix[(b.Min.Y+row-img.Rect.Min.Y)*img.Stride+(b.Min.X-img.Rect.Min.X)*4:]
		s1 := img.Pix[(b.Min.Y+row+1-img.Rect.Min.Y)*img.Stride+(b.Min.X-img.Rect.Min.X)*4:]
		d0 := f.Y[(y0+row)*f.W+x0:]
		d1 := f.Y[(y0+row+1)*f.W+x0:]
		uv := f.UV[((y0+row)/2)*f.W+x0:]
		for col := 0; col < w; col += 2 {
			i := col * 4
			r0, g0, b0 := int32(s0[i]), int32(s0[i+1]), int32(s0[i+2])
			r1, g1, b1 := int32(s0[i+4]), int32(s0[i+5]), int32(s0[i+6])
			r2, g2, b2 := int32(s1[i]), int32(s1[i+1]), int32(s1[i+2])
			r3, g3, b3 := int32(s1[i+4]), int32(s1[i+5]), int32(s1[i+6])
			d0[col] = clamp8(((66*r0 + 129*g0 + 25*b0 + 128) >> 8) + 16)
			d0[col+1] = clamp8(((66*r1 + 129*g1 + 25*b1 + 128) >> 8) + 16)
			d1[col] = clamp8(((66*r2 + 129*g2 + 25*b2 + 128) >> 8) + 16)
			d1[col+1] = clamp8(((66*r3 + 129*g3 + 25*b3 + 128) >> 8) + 16)
			r, g, bb := (r0+r1+r2+r3)>>2, (g0+g1+g2+g3)>>2, (b0+b1+b2+b3)>>2
			uv[col] = clamp8(((-38*r - 74*g + 112*bb + 128) >> 8) + 128)
			uv[col+1] = clamp8(((112*r - 94*g - 18*bb + 128) >> 8) + 128)
		}
	}
}

// Blend draws a premultiplied RGBA image over the frame at (x0, y0), honouring
// its alpha. Used for small overlays on the camera picture.
func (f *Frame) Blend(img *image.RGBA, x0, y0 int) {
	b := img.Bounds()
	w, h := b.Dx()&^1, b.Dy()&^1
	x0, y0 = x0&^1, y0&^1
	if x0+w > f.W {
		w = (f.W - x0) &^ 1
	}
	if y0+h > f.H {
		h = (f.H - y0) &^ 1
	}
	for row := 0; row < h; row++ {
		s := img.Pix[(b.Min.Y+row-img.Rect.Min.Y)*img.Stride+(b.Min.X-img.Rect.Min.X)*4:]
		d := f.Y[(y0+row)*f.W+x0:]
		for col := 0; col < w; col++ {
			a := int32(s[col*4+3])
			if a == 0 {
				continue
			}
			r, g, bb := int32(s[col*4]), int32(s[col*4+1]), int32(s[col*4+2])
			// Premultiplied: the colour's own luma contribution is scaled by alpha.
			yc := ((66*r + 129*g + 25*bb + 128) >> 8) + (16*a+127)/255
			d[col] = clamp8(yc + int32(d[col])*(255-a)/255)
		}
	}
	for row := 0; row < h; row += 2 {
		s0 := img.Pix[(b.Min.Y+row-img.Rect.Min.Y)*img.Stride+(b.Min.X-img.Rect.Min.X)*4:]
		uv := f.UV[((y0+row)/2)*f.W+x0:]
		for col := 0; col < w; col += 2 {
			a := int32(s0[col*4+3])
			if a == 0 {
				continue
			}
			r, g, bb := int32(s0[col*4]), int32(s0[col*4+1]), int32(s0[col*4+2])
			uc := ((-38*r - 74*g + 112*bb + 128) >> 8) + (128*a+127)/255
			vc := ((112*r - 94*g - 18*bb + 128) >> 8) + (128*a+127)/255
			uv[col] = clamp8(uc + int32(uv[col])*(255-a)/255)
			uv[col+1] = clamp8(vc + int32(uv[col+1])*(255-a)/255)
		}
	}
}

// Scaler copies a fixed-size NV12 source into a rectangle of the frame,
// scaling with a 2x2 box filter. Index tables are built once per size.
type Scaler struct {
	sw, sh  int
	dst     image.Rectangle
	xs, ys  []int
	uxs     []int
	uys     []int
	twoRows bool
}

// NewScaler prepares scaling sw x sh into dst (the aspect ratio is cropped to
// fill dst, centred).
func NewScaler(sw, sh int, dst image.Rectangle) *Scaler {
	dst = evenRect(dst)
	dw, dh := dst.Dx(), dst.Dy()
	// Crop the source to the destination aspect ratio.
	cw, ch := sw, sw*dh/dw
	if ch > sh {
		ch, cw = sh, sh*dw/dh
	}
	ox, oy := (sw-cw)/2&^1, (sh-ch)/2&^1
	s := &Scaler{sw: sw, sh: sh, dst: dst, twoRows: ch >= dh*2}
	s.xs = make([]int, dw)
	for i := range s.xs {
		s.xs[i] = min(ox+i*cw/dw, sw-2)
	}
	s.ys = make([]int, dh)
	for i := range s.ys {
		s.ys[i] = min(oy+i*ch/dh, sh-2)
	}
	s.uxs = make([]int, dw/2)
	for i := range s.uxs {
		s.uxs[i] = (s.xs[i*2] / 2) * 2
	}
	s.uys = make([]int, dh/2)
	for i := range s.uys {
		s.uys[i] = s.ys[i*2] / 2
	}
	return s
}

// Draw scales src (NV12, sw x sh) into the frame.
func (s *Scaler) Draw(f *Frame, src []byte) {
	if len(src) < s.sw*s.sh*3/2 {
		return
	}
	dw := min(s.dst.Dx(), f.W-s.dst.Min.X)
	xs := s.xs[:dw]
	for row, sy := range s.ys {
		dy := s.dst.Min.Y + row
		if dy >= f.H {
			break
		}
		d := f.Y[dy*f.W+s.dst.Min.X:][:dw]
		a := src[sy*s.sw:][:s.sw]
		if s.twoRows {
			b := src[(sy+1)*s.sw:][:s.sw]
			for col, x := range xs {
				if x+1 >= len(a) || x+1 >= len(b) {
					break
				}
				d[col] = byte((uint16(a[x]) + uint16(a[x+1]) + uint16(b[x]) + uint16(b[x+1]) + 2) >> 2)
			}
		} else {
			for col, x := range xs {
				d[col] = a[x]
			}
		}
	}
	uvSrc := src[s.sw*s.sh:]
	uxs := s.uxs[:dw/2]
	for row, sy := range s.uys {
		dy := s.dst.Min.Y/2 + row
		if dy >= f.H/2 {
			break
		}
		d := f.UV[dy*f.W+s.dst.Min.X:][:dw&^1]
		a := uvSrc[sy*s.sw:][:s.sw]
		for col, x := range uxs {
			if x+1 >= len(a) {
				break
			}
			d[col*2], d[col*2+1] = a[x], a[x+1]
		}
	}
}

// Stretcher resamples a logical frame to the output size along one axis
// (bilinear), for head units with non-square displayed pixels.
type Stretcher struct {
	src, dst *Frame
	// per destination row (or column): source index and weight of the next one
	idx      []int
	wt       []uint16
	vertical bool
}

// NewStretcher maps src onto a w x h frame; exactly one dimension differs.
func NewStretcher(src *Frame, w, h int) *Stretcher {
	s := &Stretcher{src: src, dst: NewFrame(w, h), vertical: h != src.H}
	n, m := w, src.W
	if s.vertical {
		n, m = h, src.H
	}
	s.idx, s.wt = make([]int, n), make([]uint16, n)
	for i := range n {
		f := (float64(i)+0.5)*float64(m)/float64(n) - 0.5
		f = math.Max(0, math.Min(f, float64(m-1)))
		k := int(f)
		if k >= m-1 {
			k = m - 2
		}
		s.idx[i], s.wt[i] = k, uint16((f-float64(k))*256)
	}
	return s
}

// Frame returns the stretched picture of the current source.
func (s *Stretcher) Frame() *Frame {
	src, dst := s.src, s.dst
	if s.vertical {
		blend := func(r0, r1, d []byte, a uint32) {
			for x := range d {
				d[x] = byte((uint32(r0[x])*(256-a) + uint32(r1[x])*a + 128) >> 8)
			}
		}
		w := src.W
		for y := 0; y < dst.H; y++ {
			k := s.idx[y]
			blend(src.Y[k*w:(k+1)*w], src.Y[(k+1)*w:(k+2)*w], dst.Y[y*w:(y+1)*w], uint32(s.wt[y]))
		}
		for y := 0; y < dst.H/2; y++ {
			k := min(s.idx[2*y]/2, src.H/2-2)
			blend(src.UV[k*w:(k+1)*w], src.UV[(k+1)*w:(k+2)*w], dst.UV[y*w:(y+1)*w], uint32(s.wt[2*y]))
		}
		return dst
	}
	for y := 0; y < src.H; y++ {
		sr, dr := src.Y[y*src.W:(y+1)*src.W], dst.Y[y*dst.W:(y+1)*dst.W]
		for x := range dr {
			k, a := s.idx[x], uint32(s.wt[x])
			dr[x] = byte((uint32(sr[k])*(256-a) + uint32(sr[k+1])*a + 128) >> 8)
		}
	}
	for y := 0; y < src.H/2; y++ {
		sr, dr := src.UV[y*src.W:(y+1)*src.W], dst.UV[y*dst.W:(y+1)*dst.W]
		for x := 0; x+1 < len(dr); x += 2 {
			k := (s.idx[x] / 2) * 2
			if k+3 >= len(sr) {
				k = len(sr) - 4
			}
			a := uint32(s.wt[x])
			dr[x] = byte((uint32(sr[k])*(256-a) + uint32(sr[k+2])*a + 128) >> 8)
			dr[x+1] = byte((uint32(sr[k+1])*(256-a) + uint32(sr[k+3])*a + 128) >> 8)
		}
	}
	return dst
}

// LogicalSize is the size to lay out for a w x h picture whose pixels are
// shown parE4/10000 times as wide as tall: the stretched axis is shrunk so the
// layout keeps its proportions on the screen (and rendering stays cheap).
func LogicalSize(w, h, parE4 int) (int, int) {
	par := float64(parE4) / 10000
	if parE4 <= 0 || math.Abs(par-1) < 0.02 {
		return w, h
	}
	if par > 1 {
		return w, int(math.Round(float64(h)/par)) &^ 1
	}
	return int(math.Round(float64(w)*par)) &^ 1, h
}

// Ranges returns the byte ranges of the source frame Draw reads, merged and in
// order, so a reader can skip the rest of the 3 MB export.
func (s *Scaler) Ranges() [][2]int {
	var out [][2]int
	add := func(a, b int) {
		if n := len(out); n > 0 && out[n-1][1] >= a {
			out[n-1][1] = max(out[n-1][1], b)
			return
		}
		out = append(out, [2]int{a, b})
	}
	rows := 1
	if s.twoRows {
		rows = 2
	}
	for _, y := range s.ys {
		add(y*s.sw, (y+rows)*s.sw)
	}
	base := s.sw * s.sh
	for _, y := range s.uys {
		add(base+y*s.sw, base+(y+1)*s.sw)
	}
	return out
}
