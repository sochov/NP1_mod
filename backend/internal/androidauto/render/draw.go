package render

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"golang.org/x/image/vector"
)

// canvas wraps an RGBA layer with anti-aliased shape helpers. Paths are
// recorded and rasterized only over their bounding box: rasterizing the whole
// layer for every small shape was the renderer's main cost.
type canvas struct {
	img *image.RGBA
	r   *pathRecorder
	ras *vector.Rasterizer
}

func newCanvas(w, h int) *canvas {
	return &canvas{img: image.NewRGBA(image.Rect(0, 0, w, h)), r: &pathRecorder{}, ras: vector.NewRasterizer(1, 1)}
}

type pathOp struct {
	kind byte // 'M', 'L', 'C', 'Z'
	p    [6]float32
}

type pathRecorder struct {
	ops                    []pathOp
	minX, minY, maxX, maxY float32
}

func (r *pathRecorder) grow(x, y float32) {
	if len(r.ops) == 0 {
		r.minX, r.minY, r.maxX, r.maxY = x, y, x, y
		return
	}
	r.minX, r.minY, r.maxX, r.maxY = min(r.minX, x), min(r.minY, y), max(r.maxX, x), max(r.maxY, y)
}

func (r *pathRecorder) MoveTo(x, y float32) {
	r.grow(x, y)
	r.ops = append(r.ops, pathOp{'M', [6]float32{x, y}})
}
func (r *pathRecorder) LineTo(x, y float32) {
	r.grow(x, y)
	r.ops = append(r.ops, pathOp{'L', [6]float32{x, y}})
}
func (r *pathRecorder) CubeTo(a, b, c, d, x, y float32) {
	r.grow(a, b)
	r.grow(c, d)
	r.grow(x, y)
	r.ops = append(r.ops, pathOp{'C', [6]float32{a, b, c, d, x, y}})
}
func (r *pathRecorder) ClosePath() { r.ops = append(r.ops, pathOp{kind: 'Z'}) }

func (c *canvas) clear(col color.RGBA) {
	draw.Draw(c.img, c.img.Bounds(), image.NewUniform(col), image.Point{}, draw.Src)
}

func (c *canvas) fill(col color.Color) {
	p := c.r
	defer func() { p.ops = p.ops[:0] }()
	if len(p.ops) == 0 {
		return
	}
	box := image.Rect(int(math.Floor(float64(p.minX))), int(math.Floor(float64(p.minY))), int(math.Ceil(float64(p.maxX)))+1, int(math.Ceil(float64(p.maxY)))+1).Intersect(c.img.Bounds())
	if box.Empty() {
		return
	}
	ox, oy := float32(box.Min.X), float32(box.Min.Y)
	c.ras.Reset(box.Dx(), box.Dy())
	for _, op := range p.ops {
		switch op.kind {
		case 'M':
			c.ras.MoveTo(op.p[0]-ox, op.p[1]-oy)
		case 'L':
			c.ras.LineTo(op.p[0]-ox, op.p[1]-oy)
		case 'C':
			c.ras.CubeTo(op.p[0]-ox, op.p[1]-oy, op.p[2]-ox, op.p[3]-oy, op.p[4]-ox, op.p[5]-oy)
		case 'Z':
			c.ras.ClosePath()
		}
	}
	c.ras.Draw(c.img, box, image.NewUniform(col), image.Point{})
}

func (c *canvas) roundRect(x, y, w, h, rad float32, col color.Color) {
	c.roundRectPath(x, y, w, h, rad)
	c.fill(col)
}

func (c *canvas) roundRectPath(x, y, w, h, rad float32) {
	rad = min(rad, w/2, h/2)
	const k = 0.5523 // cubic approximation of a quarter circle
	r := c.r
	r.MoveTo(x+rad, y)
	r.LineTo(x+w-rad, y)
	r.CubeTo(x+w-rad+rad*k, y, x+w, y+rad-rad*k, x+w, y+rad)
	r.LineTo(x+w, y+h-rad)
	r.CubeTo(x+w, y+h-rad+rad*k, x+w-rad+rad*k, y+h, x+w-rad, y+h)
	r.LineTo(x+rad, y+h)
	r.CubeTo(x+rad-rad*k, y+h, x, y+h-rad+rad*k, x, y+h-rad)
	r.LineTo(x, y+rad)
	r.CubeTo(x, y+rad-rad*k, x+rad-rad*k, y, x+rad, y)
	r.ClosePath()
}

// border draws a rounded outline of width bw inside the rectangle.
func (c *canvas) border(x, y, w, h, rad, bw float32, col color.Color) {
	// Outer path clockwise, inner path counter-clockwise: the non-zero rule
	// leaves the ring.
	c.roundRectPath(x, y, w, h, rad)
	ix, iy, iw, ih, ir := x+bw, y+bw, w-2*bw, h-2*bw, max(rad-bw, 0)
	const k = 0.5523
	r := c.r
	r.MoveTo(ix+ir, iy)
	r.CubeTo(ix+ir-ir*k, iy, ix, iy+ir-ir*k, ix, iy+ir)
	r.LineTo(ix, iy+ih-ir)
	r.CubeTo(ix, iy+ih-ir+ir*k, ix+ir-ir*k, iy+ih, ix+ir, iy+ih)
	r.LineTo(ix+iw-ir, iy+ih)
	r.CubeTo(ix+iw-ir+ir*k, iy+ih, ix+iw, iy+ih-ir+ir*k, ix+iw, iy+ih-ir)
	r.LineTo(ix+iw, iy+ir)
	r.CubeTo(ix+iw, iy+ir-ir*k, ix+iw-ir+ir*k, iy, ix+iw-ir, iy)
	r.ClosePath()
	c.fill(col)
}

func (c *canvas) circle(cx, cy, rad float32, col color.Color) {
	c.circlePath(cx, cy, rad)
	c.fill(col)
}

func (c *canvas) circlePath(cx, cy, rad float32) {
	const k = 0.5523
	r := c.r
	r.MoveTo(cx+rad, cy)
	r.CubeTo(cx+rad, cy+rad*k, cx+rad*k, cy+rad, cx, cy+rad)
	r.CubeTo(cx-rad*k, cy+rad, cx-rad, cy+rad*k, cx-rad, cy)
	r.CubeTo(cx-rad, cy-rad*k, cx-rad*k, cy-rad, cx, cy-rad)
	r.CubeTo(cx+rad*k, cy-rad, cx+rad, cy-rad*k, cx+rad, cy)
	r.ClosePath()
}

// polyline strokes connected points with round joins.
func (c *canvas) polyline(pts [][2]float32, width float32, col color.Color) {
	c.addPolyline(pts, width)
	c.fill(col)
}

// addPolyline adds a stroked line to the current path without filling it.
func (c *canvas) addPolyline(pts [][2]float32, width float32) {
	if len(pts) < 2 {
		return
	}
	hw := width / 2
	b := c.img.Bounds()
	lo, hiX, hiY := -width*4, float32(b.Dx())+width*4, float32(b.Dy())+width*4
	inside := func(p [2]float32) bool { return p[0] > lo && p[1] > lo && p[0] < hiX && p[1] < hiY }
	for i := 1; i < len(pts); i++ {
		a, e := pts[i-1], pts[i]
		if !inside(a) && !inside(e) {
			continue
		}
		dx, dy := e[0]-a[0], e[1]-a[1]
		l := float32(math.Hypot(float64(dx), float64(dy)))
		if l < 0.01 {
			continue
		}
		nx, ny := -dy/l*hw, dx/l*hw
		r := c.r
		// Same winding as circlePath, so overlaps add up instead of cancelling.
		r.MoveTo(a[0]+nx, a[1]+ny)
		r.LineTo(a[0]-nx, a[1]-ny)
		r.LineTo(e[0]-nx, e[1]-ny)
		r.LineTo(e[0]+nx, e[1]+ny)
		r.ClosePath()
		c.circlePath(e[0], e[1], hw)
	}
	c.circlePath(pts[0][0], pts[0][1], hw)
}

func rgba(hex uint32) color.RGBA {
	return color.RGBA{R: byte(hex >> 16), G: byte(hex >> 8), B: byte(hex), A: 255}
}

// withAlpha returns a premultiplied colour with alpha a.
func withAlpha(c color.RGBA, a byte) color.RGBA {
	return color.RGBA{R: byte(uint16(c.R) * uint16(a) / 255), G: byte(uint16(c.G) * uint16(a) / 255), B: byte(uint16(c.B) * uint16(a) / 255), A: a}
}

// fillRect paints an axis-aligned rectangle.
func fillRect(img *image.RGBA, r image.Rectangle, col color.RGBA) {
	draw.Draw(img, r.Intersect(img.Bounds()), image.NewUniform(col), image.Point{}, draw.Src)
}
