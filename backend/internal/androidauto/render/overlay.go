package render

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"
)

// Signal is the advanced-PICS signal shown on the map (as on the Web UI
// dashboard): the colour and "青まであとN秒" or the colour name.
type Signal struct {
	Color    string // red, green, blink-green
	Text     string
	Name     string
	Estimate bool // the seconds are an estimate
}

// popupSeconds is how long a rule-match notification stays on the map.
const popupSeconds = 8

type popup struct {
	plate Plate
	at    time.Time
	until time.Time
}

var (
	signalRed   = rgba(0xe05b54)
	signalGreen = rgba(0x328c68)
)

// notify opens the notification card for a new rule match or danger entry.
func (d *Dashboard) notify(p Plate, now time.Time) {
	d.popup = &popup{plate: p, at: now, until: now.Add(popupSeconds * time.Second)}
}

func (d *Dashboard) refreshPopup(plates []Plate, now time.Time) {
	if d.popup == nil {
		return
	}
	if now.After(d.popup.until) {
		d.popup = nil
		return
	}
	for _, p := range plates {
		if p.ID == d.popup.plate.ID {
			d.popup.plate = p // picks up the crop once loaded
		}
	}
}

func (d *Dashboard) overlayKey(now time.Time) string {
	k := ""
	if sig := d.state.Signal; sig != nil {
		k = sig.Color + sig.Text + fmt.Sprint(sig.Estimate)
		if sig.Color == "blink-green" {
			k += fmt.Sprint(now.UnixMilli() / 500 % 2)
		}
	}
	if p := d.popup; p != nil && now.Before(p.until) {
		k += "|" + p.plate.ID + fmt.Sprint(p.plate.Thumb != nil, len(p.plate.Matches))
	}
	return k
}

func (d *Dashboard) signalRect() image.Rectangle {
	sig := d.state.Signal
	if sig == nil {
		return image.Rectangle{}
	}
	s := float32(d.L.Scale)
	fl, ft, fe := face(13*d.L.Scale, true), face(15*d.L.Scale, true), face(10*d.L.Scale, false)
	w := 12*s + 12*s + 8*s + textWidth(fl, "信号") + 8*s + textWidth(ft, sig.Text) + 14*s
	if sig.Estimate {
		w += 6*s + textWidth(fe, "目安")
	}
	w = min(w, float32(d.L.Map.Dx())-24*s)
	return image.Rect(int(12*s), int(12*s), int(12*s+w)+1, int(12*s+34*s)+1)
}

func (d *Dashboard) popupRect() image.Rectangle {
	if d.popup == nil {
		return image.Rectangle{}
	}
	s := float32(d.L.Scale)
	mw := float32(d.L.Map.Dx())
	pw := min(mw-24*s, 340*s)
	y := 12 * s
	if r := d.signalRect(); !r.Empty() {
		y = float32(r.Max.Y) + 8*s
	}
	return image.Rect(int(mw-12*s-pw), int(y), int(mw-12*s)+1, int(y+108*s)+1)
}

// drawMapOverlays redraws the parts of the map under the signal and the
// notification card when they change, restoring the map from mapBase.
func (d *Dashboard) drawMapOverlays(now time.Time, force bool) bool {
	if d.mapBase == nil {
		return false
	}
	key := d.overlayKey(now)
	if !force && key == d.overlayKeyNow {
		return false
	}
	d.overlayKeyNow = key
	sig, pop := d.signalRect(), d.popupRect()
	regions := append(d.overlayRects[:0:0], d.overlayRects...)
	if !sig.Empty() {
		regions = append(regions, sig)
	}
	if !pop.Empty() {
		regions = append(regions, pop)
	}
	d.overlayRects = d.overlayRects[:0]
	if !sig.Empty() {
		d.overlayRects = append(d.overlayRects, sig)
	}
	if !pop.Empty() {
		d.overlayRects = append(d.overlayRects, pop)
	}
	bounds := d.mapBase.Bounds()
	for _, r := range regions {
		r = evenRect(r.Inset(-4)).Intersect(bounds)
		if r.Empty() {
			continue
		}
		c := newCanvas(r.Dx(), r.Dy())
		draw.Draw(c.img, c.img.Bounds(), d.mapBase, r.Min, draw.Src)
		ox, oy := float32(r.Min.X), float32(r.Min.Y)
		if !sig.Empty() && sig.Overlaps(r) {
			d.drawSignal(c, sig, ox, oy, now)
		}
		if !pop.Empty() && pop.Overlaps(r) {
			d.drawPopup(c, pop, ox, oy)
		}
		d.frame.Blit(c.img, d.L.Map.Min.X+r.Min.X, d.L.Map.Min.Y+r.Min.Y)
	}
	return true
}

func (d *Dashboard) drawSignal(c *canvas, r image.Rectangle, ox, oy float32, now time.Time) {
	sig := d.state.Signal
	s := float32(d.L.Scale)
	x, y := float32(r.Min.X)-ox, float32(r.Min.Y)-oy
	w, h := float32(r.Dx()-1), float32(r.Dy()-1)
	c.roundRect(x, y, w, h, h/2, withAlpha(d.pal.Glass, 240))
	c.border(x, y, w, h, h/2, s, d.pal.Line)
	dot := d.pal.Muted
	switch sig.Color {
	case "red":
		dot = signalRed
	case "green":
		dot = signalGreen
	case "blink-green":
		dot = signalGreen
		if now.UnixMilli()/500%2 == 1 {
			dot = withAlpha(signalGreen, 70)
		}
	}
	c.circle(x+18*s, y+h/2, 6*s, dot)
	fl, ft, fe := face(13*d.L.Scale, true), face(15*d.L.Scale, true), face(10*d.L.Scale, false)
	tx := x + 30*s
	c.textBox(fl, "信号", tx, y, 40*s, h, 0, d.pal.Text)
	tx += textWidth(fl, "信号") + 8*s
	right := x + w - 14*s
	if sig.Estimate {
		right -= 6*s + textWidth(fe, "目安")
		c.textBox(fe, "目安", right+6*s, y+2*s, 40*s, h, 0, d.pal.Muted)
	}
	c.textBox(ft, sig.Text, tx, y, right-tx, h, 0, d.pal.Text)
}

func (d *Dashboard) drawPopup(c *canvas, r image.Rectangle, ox, oy float32) {
	p := d.popup.plate
	s := float32(d.L.Scale)
	x, y := float32(r.Min.X)-ox, float32(r.Min.Y)-oy
	w, h := float32(r.Dx()-1), float32(r.Dy()-1)
	accent, bg, title := d.pal.PlateHit, d.pal.PlateHitSoft, "条件に一致するナンバー"
	if p.Danger {
		accent, bg, title = d.pal.Danger, d.pal.DangerSoft, "注意が必要な車両（NumberShare）"
	}
	rad := 14 * s
	c.roundRect(x+2*s, y+3*s, w, h, rad, color.RGBA{0, 0, 0, 40}) // shadow
	c.roundRect(x, y, w, h, rad, accent)
	c.roundRect(x+2*s, y+2*s, w-4*s, h-4*s, rad-2*s, bg)
	// Header strip.
	strip := withAlpha(accent, 36)
	if d.pal.Dark {
		strip = withAlpha(accent, 64)
	}
	c.roundRect(x+2*s, y+2*s, w-4*s, 26*s, rad-2*s, strip)
	fillRect(c.img, image.Rect(int(x+2*s), int(y+16*s), int(x+w-2*s), int(y+28*s)), blendOver(bg, strip))
	fh, fm := face(12*d.L.Scale, true), face(11*d.L.Scale, false)
	clock := p.FirstSeen.In(jst).Format("15:04:05")
	cw := textWidth(fm, clock)
	c.textBox(fh, title, x+12*s, y+2*s, w-36*s-cw, 26*s, 0, accent)
	c.textBox(fm, clock, x+w-12*s-cw, y+2*s, cw, 26*s, 0, d.pal.Muted)
	// Body: a large plate, the rule names and the crop.
	by := y + 28*s
	bw := 120 * s
	d.plateBadge(c, x+12*s, by+(h-28*s-bw/2)/2, bw, p, true)
	rx := x + 12*s + bw + 12*s
	rw := x + w - 12*s - rx
	if rw < 20*s {
		return
	}
	names := make([]string, 0, len(p.Matches))
	for _, m := range p.Matches {
		if m == "" {
			m = "条件に一致"
		}
		names = append(names, m)
	}
	if len(names) == 0 && p.Share != "" {
		names = append(names, "NumberShare: "+p.Share)
	}
	ty := by + 10*s
	if len(names) > 0 {
		f := face(11*d.L.Scale, true)
		tag := strings.Join(names, "・")
		tw := min(textWidth(f, tag)+12*s, rw)
		c.roundRect(rx, ty, tw, 19*s, 6*s, accent)
		c.textBox(f, tag, rx+6*s, ty, tw-12*s, 19*s, 0, color.RGBA{255, 255, 255, 255})
		ty += 25 * s
	}
	if p.Thumb != nil {
		tb := p.Thumb.Bounds()
		th := min(y+h-10*s-ty, 40*s)
		tw := th * float32(tb.Dx()) / float32(tb.Dy())
		if tw > rw {
			tw, th = rw, rw*float32(tb.Dy())/float32(tb.Dx())
		}
		c.roundRect(rx-s, ty-s, tw+2*s, th+2*s, 3*s, d.pal.Line)
		xdraw.ApproxBiLinear.Scale(c.img, image.Rect(int(rx), int(ty), int(rx+tw), int(ty+th)), p.Thumb, tb, draw.Src, nil)
	}
}

// blendOver composes a premultiplied colour over an opaque one.
func blendOver(base, over color.RGBA) color.RGBA {
	a := uint16(255 - over.A)
	return color.RGBA{byte(uint16(over.R) + uint16(base.R)*a/255), byte(uint16(over.G) + uint16(base.G)*a/255), byte(uint16(over.B) + uint16(base.B)*a/255), 255}
}
