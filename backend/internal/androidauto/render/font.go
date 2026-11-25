package render

import (
	_ "embed"
	"image"
	"image/color"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// BIZ UDPGothic (SIL OFL 1.1), subset by tools/androidauto-font-subset.py to
// the characters the dashboard can show. See fonts/OFL.txt.
var (
	//go:embed fonts/BIZUDPGothic-Regular.subset.ttf
	regularTTF []byte
	//go:embed fonts/BIZUDPGothic-Bold.subset.ttf
	boldTTF []byte
)

type fontKey struct {
	bold bool
	size int // 1/4 px
}

var (
	fontOnce  sync.Once
	fontReg   *opentype.Font
	fontBold  *opentype.Font
	fontErr   error
	faceMu    sync.Mutex
	faceCache = map[fontKey]font.Face{}
)

func loadFonts() {
	fontReg, fontErr = opentype.Parse(regularTTF)
	if fontErr == nil {
		fontBold, fontErr = opentype.Parse(boldTTF)
	}
}

// face returns a face of the given pixel size.
func face(px float64, bold bool) font.Face {
	fontOnce.Do(loadFonts)
	if fontErr != nil {
		return nil
	}
	k := fontKey{bold: bold, size: int(px*4 + 0.5)}
	faceMu.Lock()
	defer faceMu.Unlock()
	if f, ok := faceCache[k]; ok {
		return f
	}
	src := fontReg
	if bold {
		src = fontBold
	}
	f, e := opentype.NewFace(src, &opentype.FaceOptions{Size: float64(k.size) / 4, DPI: 72, Hinting: font.HintingNone})
	if e != nil {
		return nil
	}
	faceCache[k] = f
	return f
}

// textWidth measures s in pixels.
func textWidth(f font.Face, s string) float32 {
	if f == nil {
		return 0
	}
	return float32(font.MeasureString(f, s)) / 64
}

// text draws s with its baseline at (x, y).
func (c *canvas) text(f font.Face, s string, x, y float32, col color.Color) {
	if f == nil || s == "" {
		return
	}
	d := font.Drawer{Dst: c.img, Src: image.NewUniform(col), Face: f, Dot: fixed.Point26_6{X: fixed.Int26_6(x * 64), Y: fixed.Int26_6(y * 64)}}
	d.DrawString(s)
}

// textBox draws s vertically centred in a box of height h starting at top,
// shortened with an ellipsis to fit maxW. align: 0 left, 1 centre, 2 right.
func (c *canvas) textBox(f font.Face, s string, x, top, w, h float32, align int, col color.Color) {
	if f == nil {
		return
	}
	s = ellipsize(f, s, w)
	m := f.Metrics()
	asc, desc := float32(m.Ascent)/64, float32(m.Descent)/64
	y := top + (h+asc-desc)/2
	tw := textWidth(f, s)
	switch align {
	case 1:
		x += (w - tw) / 2
	case 2:
		x += w - tw
	}
	c.text(f, s, x, y, col)
}

func ellipsize(f font.Face, s string, maxW float32) string {
	if textWidth(f, s) <= maxW {
		return s
	}
	r := []rune(s)
	for len(r) > 0 {
		r = r[:len(r)-1]
		t := string(r) + "…"
		if textWidth(f, t) <= maxW {
			return t
		}
	}
	return ""
}

// fitFace returns a face of at most px pixels in which s fits maxW.
func fitFace(s string, px float64, bold bool, maxW float32) font.Face {
	f := face(px, bold)
	if w := textWidth(f, s); w > maxW && w > 0 {
		f = face(px*float64(maxW/w)*0.98, bold)
	}
	return f
}

// hasGlyphs reports whether the regular face can draw every rune of s (the
// font is a subset; labels with other characters are left out).
func hasGlyphs(s string) bool {
	fontOnce.Do(loadFonts)
	if fontErr != nil {
		return false
	}
	var buf sfnt.Buffer
	for _, r := range s {
		if r == ' ' || r == '\u3000' {
			continue
		}
		if i, err := fontReg.GlyphIndex(&buf, r); err != nil || i == 0 {
			return false
		}
	}
	return true
}
