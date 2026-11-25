package render

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sort"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/math/f64"
)

// LatLon is a WGS84 position.
type LatLon struct{ Lat, Lon float64 }

// Plate is one row of the detection list.
type Plate struct {
	ID         string
	FirstSeen  time.Time
	Region     string
	Class      string
	Kana       string
	Number     string
	Confidence float64
	Sightings  int
	Matches    []string // rule names
	Share      string   // NumberShare category, "" when not registered
	Danger     bool
	Located    bool // Lat/Lon hold where it was read
	Lat, Lon   float64
	Estimated  bool        // position interpolated from neighbouring fixes
	Image      bool        // a detection image exists
	Thumb      *image.RGBA // the plate cut out of it, once loaded
}

// State is everything the dashboard shows apart from the camera.
type State struct {
	Position   *LatLon
	Live       bool   // position is a current fix (not the last known one)
	PosNote    string // shown on the map when there is no current fix
	Trail      [][]LatLon
	Plates     []Plate
	PlatesNote string // shown when the list is empty or failing
	Sensor     string // recognition chip text
	SensorTone int    // 0 neutral, 1 ok, 2 warn
	Recording  string // recording chip text, "" when unknown
	Signal     *Signal
	Dark       bool
}

// Layout places the regions inside the head unit's visible area.
type Layout struct {
	W, H    int
	Visible image.Rectangle
	Scale   float64
	Map     image.Rectangle
	Camera  image.Rectangle
	List    image.Rectangle
}

// NewLayout lays out a w x h picture of which the head unit hides marginW x
// marginH (split evenly on both sides).
func NewLayout(w, h, marginW, marginH int) Layout {
	vis := image.Rect(marginW/2, marginH/2, w-marginW/2, h-marginH/2)
	vis = evenRect(vis).Intersect(image.Rect(0, 0, w, h))
	s := math.Min(float64(vis.Dy())/480, float64(vis.Dx())/800)
	// The right column holds the camera and the plate list (with crops); the
	// camera takes at most half the height and is cropped to fit, so the list
	// keeps room for several rows also on wide, low screens.
	pw := int(float64(vis.Dx()) * 0.40)
	pw = max(int(300*s), min(pw, int(520*s))) &^ 1
	ch := min(pw*9/16, vis.Dy()/2) &^ 1
	l := Layout{W: w, H: h, Visible: vis, Scale: s}
	l.Map = image.Rect(vis.Min.X, vis.Min.Y, vis.Max.X-pw, vis.Max.Y)
	l.Camera = image.Rect(vis.Max.X-pw, vis.Min.Y, vis.Max.X, vis.Min.Y+ch)
	l.List = image.Rect(vis.Max.X-pw, vis.Min.Y+ch, vis.Max.X, vis.Max.Y)
	return l
}

// Dashboard is the landscape dashboard of the Android app, drawn for a head
// unit: the map on the left, the front camera and the plate list on the right.
type Dashboard struct {
	L     Layout
	Tiles *Tiles
	Zoom  int

	frame   *Frame
	state   State
	stateAt time.Time
	pal     *Palette

	mapDirty, listDirty, overlayDirty bool
	mapAt, listAt                     time.Time
	mapKey                            string
	cam                               *Scaler
	camAt                             time.Time
	camOverlay                        *image.RGBA
	arrivals                          map[string]time.Time
	seen                              map[string]int
	baseline                          bool

	mapBase       *image.RGBA // the map without overlays
	overlayRects  []image.Rectangle
	overlayKeyNow string
	popup         *popup
}

// NewDashboard prepares a dashboard for one picture size.
func NewDashboard(l Layout, tiles *Tiles, zoom int) *Dashboard {
	d := &Dashboard{L: l, Tiles: tiles, Zoom: zoom, frame: NewFrame(l.W, l.H), pal: PaletteFor(true),
		mapDirty: true, listDirty: true, overlayDirty: true, arrivals: map[string]time.Time{}, seen: map[string]int{}, baseline: true}
	d.cam = NewScaler(1920, 1080, l.Camera)
	d.frame.Fill(image.Rect(0, 0, l.W, l.H), d.pal.Bg)
	return d
}

// CameraRanges are the parts of a camera frame Camera reads.
func (d *Dashboard) CameraRanges() [][2]int { return d.cam.Ranges() }

// Frame is the current picture.
func (d *Dashboard) Frame() *Frame { return d.frame }

// SetState replaces what is shown; regions redraw on the next Compose.
func (d *Dashboard) SetState(s State, now time.Time) {
	if s.Dark != d.state.Dark || d.stateAt.IsZero() {
		d.pal = PaletteFor(s.Dark)
		d.mapDirty, d.listDirty, d.overlayDirty = true, true, true
		d.frame.Fill(image.Rect(0, 0, d.L.W, d.L.H), d.pal.Bg)
	}
	if !samePlates(s.Plates, d.state.Plates) || s.PlatesNote != d.state.PlatesNote {
		d.listDirty = true
		for _, p := range s.Plates {
			before, ok := d.seen[p.ID]
			if !d.baseline && (!ok || len(p.Matches) > before) {
				d.arrivals[p.ID] = now
				if len(p.Matches) > 0 || p.Danger {
					d.notify(p, now)
				}
			}
			d.seen[p.ID] = len(p.Matches)
		}
		d.baseline = false
		if len(d.seen) > 400 {
			keep := map[string]int{}
			for _, p := range s.Plates {
				keep[p.ID] = d.seen[p.ID]
			}
			d.seen = keep
		}
	}
	if s.Sensor != d.state.Sensor || s.SensorTone != d.state.SensorTone || s.Recording != d.state.Recording {
		d.overlayDirty = true
	}
	if d.mapKeyFor(s) != d.mapKey {
		d.mapDirty = true
	}
	d.state, d.stateAt = s, now
	d.refreshPopup(s.Plates, now)
}

func trailLen(t [][]LatLon) int {
	n := 0
	for _, s := range t {
		n += len(s)
	}
	return n
}

func samePlates(a, b []Plate) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || len(a[i].Matches) != len(b[i].Matches) || a[i].Share != b[i].Share || a[i].Sightings != b[i].Sightings || (a[i].Thumb == nil) != (b[i].Thumb == nil) {
			return false
		}
	}
	return true
}

// MarkMapDirty asks for the map to be redrawn (a tile arrived).
func (d *Dashboard) MarkMapDirty() { d.mapDirty = true }

// Camera draws a camera frame (1920x1080 NV12) or, with nil, the placeholder.
func (d *Dashboard) Camera(nv12 []byte, now time.Time) {
	if nv12 != nil {
		d.cam.Draw(d.frame, nv12)
		d.camAt = now
	} else if now.Sub(d.camAt) > 3*time.Second {
		c := newCanvas(d.L.Camera.Dx(), d.L.Camera.Dy())
		c.clear(d.pal.Paper)
		s := float32(d.L.Scale)
		c.textBox(face(13*float64(s), false), "前方カメラの映像を待っています", 0, 0, float32(c.img.Bounds().Dx()), float32(c.img.Bounds().Dy()), 1, d.pal.Muted)
		d.frame.Blit(c.img, d.L.Camera.Min.X, d.L.Camera.Min.Y)
		d.camAt = now
	} else {
		return
	}
	if d.overlayDirty || d.camOverlay == nil {
		d.camOverlay = d.cameraOverlay()
		d.overlayDirty = false
	}
	d.frame.Blend(d.camOverlay, d.L.Camera.Min.X, d.L.Camera.Min.Y)
}

// Compose redraws the regions whose data changed. It reports whether the
// picture changed.
func (d *Dashboard) Compose(ctx context.Context, now time.Time) bool {
	changed := false
	if d.mapDirty || now.Sub(d.mapAt) > 30*time.Second {
		d.drawMap(ctx)
		d.mapDirty, d.mapAt, changed = false, now, true
	}
	if d.drawMapOverlays(now, false) {
		changed = true
	}
	// Relative times ("3分前") and arrival highlights age.
	if d.listDirty || now.Sub(d.listAt) > 5*time.Second || d.hasFreshArrival(now) {
		d.drawList(now)
		d.listDirty, d.listAt, changed = false, now, true
	}
	return changed
}

func (d *Dashboard) hasFreshArrival(now time.Time) bool {
	for id, at := range d.arrivals {
		age := now.Sub(at)
		if age > time.Minute {
			delete(d.arrivals, id)
			continue
		}
		if age < 4500*time.Millisecond && now.Sub(d.listAt) > 500*time.Millisecond {
			return true
		}
	}
	return false
}

// ── Map ───────────────────────────────────────────────────────────────────

func worldPixel(p LatLon, z int) (float64, float64) {
	n := 256 * math.Exp2(float64(z))
	lat := math.Max(-85, math.Min(85, p.Lat)) * math.Pi / 180
	x := (p.Lon + 180) / 360 * n
	y := (1 - math.Log(math.Tan(lat)+1/math.Cos(lat))/math.Pi) / 2 * n
	return x, y
}

func (d *Dashboard) mapKeyFor(st State) string {
	key := fmt.Sprint(st.Position, st.Live, st.PosNote, len(st.Trail), trailLen(st.Trail))
	for _, p := range st.Plates {
		if p.Located {
			key += fmt.Sprint(p.ID, len(p.Matches), p.Danger)
		}
	}
	return key
}

// Web UI map colours (frontend vector-map.ts / PlateRecognition.vue).
var (
	trailColor     = rgba(0x2f5bd8)
	trailHalo      = rgba(0xf5f8ff)
	markerColor    = rgba(0x397fd7)
	markerStale    = rgba(0x8a94a3)
	platePoint     = rgba(0x397fd7)
	platePointEdge = rgba(0xf8fafc)
	plateHitPoint  = rgba(0xe8590c)
	plateHitEdge   = rgba(0x8f2d14)
	plateDanger    = rgba(0xb42318)
)

func (d *Dashboard) drawMap(ctx context.Context) {
	st := d.state
	d.mapKey = d.mapKeyFor(st)
	w, h := d.L.Map.Dx(), d.L.Map.Dy()
	c := newCanvas(w, h)
	col := basemapColors(d.pal.Dark)
	c.clear(col.land)
	s := d.L.Scale
	fs := float32(s)
	// Tiles are drawn one zoom level deeper on larger screens so that they are
	// close to 1:1.
	extra := int(math.Round(math.Log2(math.Max(s, 1))))
	z := d.Zoom + extra
	ts := s / math.Exp2(float64(extra)) // tile pixel -> screen pixel
	center := st.Position
	if center == nil && len(st.Trail) > 0 {
		last := st.Trail[len(st.Trail)-1]
		if len(last) > 0 {
			center = &last[len(last)-1]
		}
	}
	if center != nil {
		cx, cy := worldPixel(*center, z)
		screen := func(wx, wy float64) (float32, float32) {
			return float32((wx-cx)*ts + float64(w)/2), float32((wy-cy)*ts + float64(h)/2)
		}
		toScreen := func(p LatLon) [2]float32 {
			x, y := worldPixel(p, z)
			sx, sy := screen(x, y)
			return [2]float32{sx, sy}
		}
		x0 := int(math.Floor((cx - float64(w)/2/ts) / 256))
		x1 := int(math.Floor((cx + float64(w)/2/ts) / 256))
		y0 := int(math.Floor((cy - float64(h)/2/ts) / 256))
		y1 := int(math.Floor((cy + float64(h)/2/ts) / 256))
		var labels []mapLabel
		seen := map[vecKey]bool{}
		if d.Tiles != nil {
			size := 256 * ts
			for ty := y0; ty <= y1; ty++ {
				for tx := x0; tx <= x1; tx++ {
					img := d.Tiles.Get(ctx, z, tx, ty, d.pal.Dark)
					if img != nil {
						px := (float64(tx*256)-cx)*ts + float64(w)/2
						py := (float64(ty*256)-cy)*ts + float64(h)/2
						r := image.Rect(int(math.Floor(px)), int(math.Floor(py)), int(math.Ceil(px+size)), int(math.Ceil(py+size)))
						if math.Abs(ts-1) < 0.02 {
							draw.Draw(c.img, r, img, image.Point{}, draw.Src)
						} else {
							xdraw.ApproxBiLinear.Scale(c.img, r, img, img.Bounds(), draw.Src, nil)
						}
					}
					if l, vk := d.Tiles.Labels(ctx, z, tx, ty, d.pal.Dark); !seen[vk] {
						seen[vk] = true
						labels = append(labels, l...)
					}
				}
			}
		}
		// The route of the last 24 hours, as on the Web dashboard.
		for _, seg := range st.Trail {
			pts := make([][2]float32, 0, len(seg))
			for _, p := range seg {
				pts = append(pts, toScreen(p))
			}
			c.polyline(pts, float32(6*s), withAlpha(trailHalo, 178))
			c.polyline(pts, float32(4*s), withAlpha(trailColor, 230))
		}
		d.drawLabels(c, labels, screen)
		// Where plates were read: blue points, rule matches larger in orange.
		for pass := 0; pass < 2; pass++ {
			for i := len(st.Plates) - 1; i >= 0; i-- {
				p := st.Plates[i]
				hit := len(p.Matches) > 0 || p.Danger
				if !p.Located || (pass == 0) == hit {
					continue
				}
				q := toScreen(LatLon{p.Lat, p.Lon})
				alpha := byte(217)
				if p.Estimated {
					alpha = 89
				}
				if hit {
					fill := plateHitPoint
					if p.Danger {
						fill = plateDanger
					}
					c.circle(q[0], q[1], float32(10.5*s), withAlpha(plateHitEdge, alpha))
					c.circle(q[0], q[1], float32(9*s), withAlpha(fill, alpha))
				} else {
					c.circle(q[0], q[1], float32(6.5*s), withAlpha(platePointEdge, alpha))
					c.circle(q[0], q[1], float32(5*s), withAlpha(platePoint, alpha))
				}
			}
		}
		// The current position; without a fix the last known one in grey.
		if marker := st.Position; marker != nil || center != nil {
			if marker == nil {
				marker = center
			}
			p := toScreen(*marker)
			mc := markerColor
			if !st.Live || st.Position == nil {
				mc = markerStale
			}
			c.circle(p[0], p[1], float32(14*s), withAlpha(markerColor, 26))
			c.circle(p[0], p[1], float32(10*s), color.RGBA{0, 0, 0, 30})
			c.circle(p[0], p[1], float32(9*s), color.RGBA{255, 255, 255, 255})
			c.circle(p[0], p[1], float32(6*s), mc)
		}
	}
	// Position problems only, bottom left (the top holds the signal and
	// notifications).
	if note := st.PosNote; note != "" && (!st.Live || st.Position == nil) {
		f := face(12*float64(s), false)
		tw := textWidth(f, note)
		bw, bh := min(tw+20*fs, float32(w)*0.62), 26*fs
		ny := float32(h) - 12*fs - bh
		c.roundRect(12*fs, ny, bw, bh, 12*fs, withAlpha(d.pal.Glass, 235))
		c.circle(24*fs, ny+bh/2, 4*fs, d.pal.Warn)
		c.textBox(f, note, 32*fs, ny, bw-38*fs, bh, 0, d.pal.Text)
	}
	// Source of the map, bottom right.
	f := face(9*float64(s), false)
	tw := textWidth(f, TileAttribution)
	c.roundRect(float32(w)-tw-16*fs, float32(h)-22*fs, tw+10*fs, 16*fs, 6*fs, withAlpha(d.pal.Glass, 190))
	c.textBox(f, TileAttribution, float32(w)-tw-11*fs, float32(h)-22*fs, tw, 16*fs, 0, d.pal.Muted)
	d.mapBase = c.img
	d.frame.Blit(c.img, d.L.Map.Min.X, d.L.Map.Min.Y)
	d.overlayRects = nil
	d.drawMapOverlays(time.Now(), true)
}

// drawLabels places labels by priority without overlaps, with a halo as in
// the Web UI style.
func (d *Dashboard) drawLabels(c *canvas, labels []mapLabel, screen func(float64, float64) (float32, float32)) {
	sort.SliceStable(labels, func(i, j int) bool { return labels[i].priority < labels[j].priority })
	col := basemapColors(d.pal.Dark)
	var placed []image.Rectangle
	bounds := c.img.Bounds()
	scale := d.L.Scale
	halo := withAlpha(col.halo, 235)
	for _, l := range labels {
		sx, sy := screen(l.x, l.y)
		if sx < -50 || sy < -20 || sx > float32(bounds.Dx())+50 || sy > float32(bounds.Dy())+20 {
			continue
		}
		f := face(l.size*scale, false)
		if f == nil || !hasGlyphs(l.text) {
			continue
		}
		tw := textWidth(f, l.text)
		th := float32(l.size * scale * 1.3)
		var box image.Rectangle
		switch {
		case l.angle != 0:
			cosA, sinA := float32(math.Abs(math.Cos(l.angle))), float32(math.Abs(math.Sin(l.angle)))
			bw, bh := tw*cosA+th*sinA, tw*sinA+th*cosA
			box = image.Rect(int(sx-bw/2), int(sy-bh/2), int(sx+bw/2), int(sy+bh/2))
		case l.dot:
			box = image.Rect(int(sx-tw/2), int(sy-3), int(sx+tw/2), int(sy+th+4))
		default:
			box = image.Rect(int(sx-tw/2), int(sy-th/2), int(sx+tw/2), int(sy+th/2))
		}
		if !box.Inset(-3).In(bounds) {
			continue
		}
		overlap := false
		for _, b := range placed {
			if b.Overlaps(box.Inset(-4)) {
				overlap = true
				break
			}
		}
		if overlap {
			continue
		}
		placed = append(placed, box)
		switch {
		case l.angle != 0:
			drawRotatedText(c, f, l.text, sx, sy, tw, th, l.angle, l.col, halo)
		case l.dot:
			c.circle(sx, sy, float32(2.5*scale), withAlpha(col.halo, 255))
			c.circle(sx, sy, float32(1.8*scale), l.col)
			haloText(c, f, l.text, sx-tw/2, sy+float32(3*scale), th, l.col, halo)
		default:
			haloText(c, f, l.text, sx-tw/2, sy-th/2, th, l.col, halo)
		}
	}
}

func haloText(c *canvas, f font.Face, s string, x, top, h float32, fg, halo color.Color) {
	for _, o := range [][2]float32{{-1.4, 0}, {1.4, 0}, {0, -1.4}, {0, 1.4}, {-1, -1}, {1, 1}, {-1, 1}, {1, -1}} {
		c.textBox(f, s, x+o[0], top+o[1], 1e6, h, 0, halo)
	}
	c.textBox(f, s, x, top, 1e6, h, 0, fg)
}

func drawRotatedText(c *canvas, f font.Face, s string, cx, cy, tw, th float32, angle float64, fg, halo color.Color) {
	pad := float32(4)
	tmp := newCanvas(int(tw+2*pad)+1, int(th+2*pad)+1)
	haloText(tmp, f, s, pad, pad, th, fg, halo)
	cos, sin := math.Cos(angle), math.Sin(angle)
	w2, h2 := float64(tmp.img.Bounds().Dx())/2, float64(tmp.img.Bounds().Dy())/2
	// dst = R * (src - centre) + (cx, cy)
	m := f64.Aff3{cos, -sin, float64(cx) - cos*w2 + sin*h2, sin, cos, float64(cy) - sin*w2 - cos*h2}
	xdraw.ApproxBiLinear.Transform(c.img, m, tmp.img, tmp.img.Bounds(), xdraw.Over, nil)
}

// ── Camera overlay ────────────────────────────────────────────────────────

func (d *Dashboard) chip(c *canvas, x, y float32, label string, tone int) float32 {
	s := float32(d.L.Scale)
	f := face(11*float64(s), true)
	w := textWidth(f, label) + 22*s
	h := 22 * s
	c.roundRect(x, y, w, h, h/2, withAlpha(d.pal.Glass, 225))
	dot := d.pal.Faint
	switch tone {
	case 1:
		dot = d.pal.OK
	case 2:
		dot = d.pal.Warn
	case 3:
		dot = d.pal.Danger
	}
	c.circle(x+10*s, y+h/2, 3.5*s, dot)
	c.textBox(f, label, x+17*s, y, w-20*s, h, 0, d.pal.Text)
	return w
}

func (d *Dashboard) cameraOverlay() *image.RGBA {
	c := newCanvas(d.L.Camera.Dx(), d.L.Camera.Dy())
	s := float32(d.L.Scale)
	x := 8 * s
	if d.state.Recording != "" {
		x += d.chip(c, x, 8*s, d.state.Recording, 3) + 6*s
	}
	if d.state.Sensor != "" {
		d.chip(c, x, 8*s, d.state.Sensor, d.state.SensorTone)
	}
	return c.img
}

// ── Plate list ────────────────────────────────────────────────────────────

var jst = time.FixedZone("JST", 9*3600)

func ago(t, now time.Time) string {
	sec := int(now.Sub(t).Seconds())
	switch {
	case sec < 0:
		sec = 0
		fallthrough
	case sec < 60:
		return fmt.Sprintf("%d秒前", sec)
	case sec < 3600:
		return fmt.Sprintf("%d分前", sec/60)
	case sec < 86400:
		return fmt.Sprintf("%d時間前", sec/3600)
	}
	return fmt.Sprintf("%d日前", sec/86400)
}

// PlateSerial formats the serial like a plate: 12-34, or "・1 23" when short.
func PlateSerial(number string) string {
	var digits []rune
	for _, r := range number {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		}
	}
	if len(digits) > 4 {
		digits = digits[len(digits)-4:]
	}
	if len(digits) == 0 {
		return ""
	}
	padded := strings.Repeat("・", 4-len(digits)) + string(digits)
	r := []rune(padded)
	if len(digits) == 4 {
		return string(r[:2]) + "-" + string(r[2:])
	}
	return string(r[:2]) + " " + string(r[2:])
}

func (d *Dashboard) drawList(now time.Time) {
	w, h := d.L.List.Dx(), d.L.List.Dy()
	c := newCanvas(w, h)
	c.clear(d.pal.Paper)
	s := float32(d.L.Scale)
	st := d.state
	if len(st.Plates) == 0 {
		title := "今日はまだ検知していません"
		if st.PlatesNote != "" {
			title = st.PlatesNote
		}
		c.textBox(face(13*float64(s), false), title, 8*s, 24*s, float32(w)-16*s, 20*s, 1, d.pal.Muted)
		d.frame.Blit(c.img, d.L.List.Min.X, d.L.List.Min.Y)
		return
	}
	pad, gap := 8*s, 5*s
	rowH := 54 * s
	y := pad
	today := now.In(jst).Format("2006-01-02")
	for _, p := range st.Plates {
		if y+rowH > float32(h)+rowH*0.6 {
			break
		}
		matched := len(p.Matches) > 0 || p.Share != ""
		// As on the Web UI: a rule match gets a soft orange row, an orange bar
		// on its left edge and a glowing plate; danger uses the red tones.
		border, base, bar := d.pal.Line, d.pal.Bg, color.RGBA{}
		switch {
		case p.Danger:
			border, base, bar = d.pal.DangerBorder, d.pal.DangerSoft, d.pal.Danger
		case matched:
			border, base, bar = d.pal.PlateHitBorder, d.pal.PlateHitSoft, d.pal.PlateHit
		}
		if at, ok := d.arrivals[p.ID]; ok && now.Sub(at) < 4*time.Second && !matched && !p.Danger {
			// A new detection flashes briefly so the eye catches it.
			border, base = d.pal.Accent, d.pal.Soft
		}
		rw := float32(w) - 2*pad
		rad := 10 * s
		c.roundRect(pad, y, rw, rowH, rad, border)
		c.roundRect(pad+s, y+s, rw-2*s, rowH-2*s, rad-s, base)
		if bar.A != 0 {
			barW := 5 * s
			c.roundRect(pad, y, rad*2, rowH, rad, bar)
			fillRect(c.img, image.Rect(int(pad+barW), int(y+s), int(pad+rad*2)+1, int(y+rowH-s)), base)
		}
		x := pad + 10*s
		// Plate badge.
		bw := float32(84) * s
		d.plateBadge(c, x, y+(rowH-bw/2)/2, bw, p, matched)
		x += bw + 6*s
		// The detected plate cut out of the camera frame.
		// A fixed slot keeps the text column aligned; the crop is fitted inside.
		th, slot := bw/2, bw
		if p.Thumb != nil || p.Image {
			ty := y + (rowH-th)/2
			if p.Thumb != nil {
				tb := p.Thumb.Bounds()
				tw, tht := th*float32(tb.Dx())/float32(tb.Dy()), th
				if tw > slot {
					tw, tht = slot, slot*float32(tb.Dy())/float32(tb.Dx())
				}
				tx, tyy := x+(slot-tw)/2, ty+(th-tht)/2
				c.roundRect(tx-s, tyy-s, tw+2*s, tht+2*s, 3*s, d.pal.Line)
				xdraw.ApproxBiLinear.Scale(c.img, image.Rect(int(tx), int(tyy), int(tx+tw), int(tyy+tht)), p.Thumb, tb, draw.Src, nil)
			} else {
				// Still loading.
				c.roundRect(x, ty, slot, th, 4*s, d.pal.Line)
			}
			x += slot + 8*s
		}
		// Time, age and either the rule names or confidence and sightings.
		infoW := float32(w) - pad - 8*s - x
		if infoW > 24*s {
			fs := p.FirstSeen.In(jst)
			age := "昨日"
			if fs.Format("2006-01-02") == today {
				age = ago(p.FirstSeen, now)
			}
			clock := fs.Format("15:04:05")
			ft := fitFace(clock, 12.5*float64(s), true, infoW)
			c.textBox(ft, clock, x, y+7*s, infoW, 17*s, 0, d.pal.Text)
			tag, tagBg, tagFg := "", d.pal.PlateHit, color.RGBA{255, 255, 255, 255}
			if len(p.Matches) > 0 {
				names := make([]string, len(p.Matches))
				for i, m := range p.Matches {
					names[i] = m
					if m == "" {
						names[i] = "条件に一致"
					}
				}
				tag = strings.Join(names, "・")
				if p.Danger {
					tagBg = d.pal.Danger
				}
			} else if p.Share != "" {
				tag = "共有: " + p.Share
				tagBg, tagFg = d.pal.Soft, d.pal.Accent
				if p.Danger {
					tagBg, tagFg = d.pal.Danger, color.RGBA{255, 255, 255, 255}
				}
			}
			fm := face(10*float64(s), false)
			if tag != "" {
				if cw := textWidth(ft, clock) + 5*s; infoW-cw > 20*s {
					c.textBox(fm, age, x+cw, y+7*s, infoW-cw, 17*s, 0, d.pal.Muted)
				}
				f := face(10*float64(s), true)
				tw := min(textWidth(f, tag)+10*s, infoW)
				c.roundRect(x, y+28*s, tw, 16*s, 5*s, tagBg)
				c.textBox(f, tag, x+5*s, y+28*s, tw-10*s, 16*s, 0, tagFg)
			} else {
				c.textBox(fm, fmt.Sprintf("%s · %d%% · %d回", age, int(p.Confidence*100), p.Sightings), x, y+28*s, infoW, 16*s, 0, d.pal.Muted)
			}
		}
		y += rowH + gap
	}
	d.frame.Blit(c.img, d.L.List.Min.X, d.L.List.Min.Y)
}

// plateBadge draws a Japanese licence plate (2:1) like the Web UI badge.
func (d *Dashboard) plateBadge(c *canvas, x, y, w float32, p Plate, matched bool) {
	sc := w / 136
	h := w / 2
	border := d.pal.PlateBorder
	bw := max(1.8*sc, 1.5)
	if p.Danger {
		border, bw = d.pal.Danger, max(2.5*sc, 2)
	} else if matched {
		border, bw = d.pal.PlateHitLine, max(2.5*sc, 2)
	}
	rad := 7 * sc
	if p.Danger || matched {
		// Glow around a matched plate (box-shadow 0 0 0 3px on the Web UI).
		c.roundRect(x-3*sc*1.4, y-3*sc*1.4, w+6*sc*1.4, h+6*sc*1.4, rad+4*sc, withAlpha(border, 110))
	}
	c.roundRect(x, y, w, h, rad, border)
	c.roundRect(x+bw, y+bw, w-2*bw, h-2*bw, max(rad-bw, 0), d.pal.PlateBg)
	ghost := withAlpha(d.pal.PlateText, 70)
	c.circle(x+w*0.14, y+h*0.16, w*0.022, ghost)
	c.circle(x+w*0.86, y+h*0.16, w*0.022, ghost)
	text := d.pal.PlateText
	pick := func(v, empty string) (string, color.Color) {
		if strings.TrimSpace(v) == "" {
			return empty, ghost
		}
		return v, text
	}
	region, rc := pick(p.Region, "？？")
	class, cc := pick(p.Class, "？？？")
	f13 := fitFace(region+" "+class, float64(13*sc), true, w-30*sc)
	rw, cw := textWidth(f13, region), textWidth(f13, class)
	gap := 4 * sc
	tx := x + (w-rw-gap-cw)/2
	top := y + 3*sc + 1*sc
	c.textBox(f13, region, tx, top, rw, 14*sc, 0, rc)
	c.textBox(f13, class, tx+rw+gap, top, cw, 14*sc, 0, cc)
	kana, kc := pick(p.Kana, "？")
	fk := face(float64(14.5*sc), true)
	lowTop, lowH := y+h*0.42, h*0.52
	kx := x + 7*sc + 2*sc
	c.textBox(fk, kana, kx, lowTop, 16*sc, lowH, 0, kc)
	serial := PlateSerial(p.Number)
	if serial == "" {
		serial = "・・・・"
	}
	sx := kx + 16*sc + 2*sc
	sw := x + w - 6*sc - sx
	c.textBox(fitFace(serial, float64(31*sc), true, sw), serial, sx, lowTop, sw, lowH, 1, text)
}
