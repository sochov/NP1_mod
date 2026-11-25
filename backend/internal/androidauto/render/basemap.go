package render

import (
	"image"
	"image/color"
	"math"
)

// The basemap follows the Web UI's OpenFreeMap style (frontend/src/services/
// vector-style.ts): the "mist" palette in light mode and "night" in dark mode.

type mapColors struct {
	land, water, green, forest, sand, building, edge, road, major, highway, text, muted, halo, poi color.RGBA
}

var (
	mistColors  = mapColors{land: rgba(0xf3f5f9), water: rgba(0xd4e4f1), green: rgba(0xe4eee8), forest: rgba(0xd9e8df), sand: rgba(0xf0ebde), building: rgba(0xe0e5ee), edge: rgba(0xcbd3df), road: rgba(0xffffff), major: rgba(0xffffff), highway: rgba(0xf1dfbb), text: rgba(0x34465f), muted: rgba(0x71839a), halo: rgba(0xf8fafc), poi: rgba(0x617ea9)}
	nightColors = mapColors{land: rgba(0x171d27), water: rgba(0x202d3a), green: rgba(0x202d28), forest: rgba(0x25332c), sand: rgba(0x302f2a), building: rgba(0x222b39), edge: rgba(0x303a4a), road: rgba(0x3a4659), major: rgba(0x4c5970), highway: rgba(0x625b50), text: rgba(0x8996a8), muted: rgba(0x5e6c80), halo: rgba(0x171d27), poi: rgba(0x627b98)}
)

func basemapColors(dark bool) *mapColors {
	if dark {
		return &nightColors
	}
	return &mistColors
}

// mapLibre zoom for a 256 px raster tile zoom.
func styleZoom(z int) float64 { return float64(z - 1) }

func linear(z, z0, v0, z1, v1 float64) float64 {
	if z <= z0 {
		return v0
	}
	if z >= z1 {
		return v1
	}
	return v0 + (v1-v0)*(z-z0)/(z1-z0)
}

func exponential(z, base, z0, v0, z1, v1 float64) float64 {
	if z <= z0 {
		return v0
	}
	if z >= z1 {
		return v1
	}
	t := (math.Pow(base, z-z0) - 1) / (math.Pow(base, z1-z0) - 1)
	return v0 + (v1-v0)*t
}

type roadClass struct {
	classes  []string
	min      float64
	width    float64
	color    func(*mapColors) color.RGBA
	priority int
}

var roadClasses = []roadClass{
	{[]string{"minor", "service"}, 13, 8, func(c *mapColors) color.RGBA { return c.road }, 1},
	{[]string{"tertiary"}, 11, 10, func(c *mapColors) color.RGBA { return c.road }, 2},
	{[]string{"secondary"}, 9, 12, func(c *mapColors) color.RGBA { return c.major }, 3},
	{[]string{"primary", "trunk"}, 7, 14, func(c *mapColors) color.RGBA { return c.major }, 4},
	{[]string{"motorway"}, 4, 16, func(c *mapColors) color.RGBA { return c.highway }, 5},
}

var basemapLayers = map[string]bool{"landcover": true, "landuse": true, "water": true, "waterway": true, "building": true, "transportation": true, "transportation_name": true, "place": true, "poi": true}

// subTile maps extent coordinates of a (possibly coarser) vector tile onto a
// 256 px raster tile.
type subTile struct {
	offX, offY, scale float32 // pixel = (extent - off) * scale
}

func newSubTile(extent float32, z, x, y, vz int) subTile {
	d := z - vz
	n := 1 << d
	sub := extent / float32(n)
	return subTile{offX: float32(x%n) * sub, offY: float32(y%n) * sub, scale: 256 / sub}
}

func (s subTile) pt(p [2]float32) [2]float32 {
	return [2]float32{(p[0] - s.offX) * s.scale, (p[1] - s.offY) * s.scale}
}

func (s subTile) visible(f *mvtFeature, margin float32) bool {
	lo, hi := s.offX-margin/s.scale, s.offX+(256+margin)/s.scale
	top, bottom := s.offY-margin/s.scale, s.offY+(256+margin)/s.scale
	return f.maxX >= lo && f.minX <= hi && f.maxY >= top && f.minY <= bottom
}

// renderBasemap draws the geometry of one raster tile (labels are placed later
// over the whole map so they are not cut at tile edges).
func renderBasemap(t mvtTile, z, x, y, vz int, dark bool) *image.RGBA {
	c := newCanvas(256, 256)
	col := basemapColors(dark)
	c.clear(col.land)
	mz := styleZoom(z)
	fillLayer := func(name string, pick func(f *mvtFeature) (color.RGBA, bool)) {
		l := t[name]
		if l == nil {
			return
		}
		st := newSubTile(l.extent, z, x, y, vz)
		groups := map[color.RGBA][]*mvtFeature{}
		var order []color.RGBA
		for i := range l.features {
			f := &l.features[i]
			if f.typ != 3 || !st.visible(f, 2) {
				continue
			}
			cc, ok := pick(f)
			if !ok {
				continue
			}
			if _, seen := groups[cc]; !seen {
				order = append(order, cc)
			}
			groups[cc] = append(groups[cc], f)
		}
		for _, cc := range order {
			for _, f := range groups[cc] {
				for _, ring := range f.geom {
					if len(ring) < 3 {
						continue
					}
					p := st.pt(ring[0])
					c.r.MoveTo(p[0], p[1])
					for _, q := range ring[1:] {
						p = st.pt(q)
						c.r.LineTo(p[0], p[1])
					}
					c.r.ClosePath()
				}
			}
			c.fill(cc)
		}
	}
	lineLayer := func(name string, pick func(f *mvtFeature) bool, width float32, cc color.Color, dash []float32) {
		l := t[name]
		if l == nil || width <= 0 {
			return
		}
		st := newSubTile(l.extent, z, x, y, vz)
		for i := range l.features {
			f := &l.features[i]
			if f.typ != 2 || !st.visible(f, width+2) || !pick(f) {
				continue
			}
			for _, line := range f.geom {
				pts := make([][2]float32, len(line))
				for j, q := range line {
					pts[j] = st.pt(q)
				}
				if dash != nil {
					for _, seg := range dashed(pts, dash[0]*max(width, 1), dash[1]*max(width, 1)) {
						c.addPolyline(seg, width)
					}
				} else {
					c.addPolyline(pts, width)
				}
			}
		}
		c.fill(cc)
	}
	class := func(f *mvtFeature) string { return f.str("class") }
	// Land cover and parks.
	fillLayer("landcover", func(f *mvtFeature) (color.RGBA, bool) {
		switch class(f) {
		case "wood", "forest":
			return withAlpha(col.forest, 191), true
		case "farmland", "grass":
			return withAlpha(col.green, 191), true
		case "sand", "bare_rock":
			return withAlpha(col.sand, 191), true
		}
		return color.RGBA{}, false
	})
	if mz >= 7 {
		fillLayer("landuse", func(f *mvtFeature) (color.RGBA, bool) {
			switch class(f) {
			case "park", "cemetery", "recreation_ground", "pitch", "village_green":
				return withAlpha(col.green, 217), true
			}
			return color.RGBA{}, false
		})
	}
	fillLayer("water", func(f *mvtFeature) (color.RGBA, bool) { return col.water, true })
	if mz >= 10 {
		lineLayer("waterway", func(*mvtFeature) bool { return true }, float32(linear(mz, 10, .8, 18, 5)), col.water, nil)
	}
	if mz >= 14 {
		a := byte(255 * linear(mz, 14, .4, 16, .9))
		fillLayer("building", func(f *mvtFeature) (color.RGBA, bool) { return withAlpha(col.building, a), true })
		if l := t["building"]; l != nil {
			st := newSubTile(l.extent, z, x, y, vz)
			for i := range l.features {
				f := &l.features[i]
				if f.typ != 3 || !st.visible(f, 2) {
					continue
				}
				for _, ring := range f.geom {
					pts := make([][2]float32, len(ring))
					for j, q := range ring {
						pts[j] = st.pt(q)
					}
					c.addPolyline(pts, .8)
				}
			}
			c.fill(withAlpha(col.edge, a))
		}
	}
	in := func(cls []string) func(f *mvtFeature) bool {
		return func(f *mvtFeature) bool {
			k := class(f)
			for _, c := range cls {
				if c == k {
					return true
				}
			}
			return false
		}
	}
	for _, rc := range roadClasses {
		if mz < rc.min {
			continue
		}
		lineLayer("transportation", in(rc.classes), float32(exponential(mz, 1.5, rc.min, .5, 18, rc.width+2)), withAlpha(col.edge, 217), nil)
		lineLayer("transportation", in(rc.classes), float32(exponential(mz, 1.5, rc.min, .35, 18, rc.width)), rc.color(col), nil)
	}
	if mz >= 15 {
		lineLayer("transportation", in([]string{"path", "track"}), float32(linear(mz, 15, .6, 19, 2)), withAlpha(col.muted, 128), []float32{2, 2})
	}
	if mz >= 10 {
		lineLayer("transportation", in([]string{"rail", "transit"}), float32(linear(mz, 10, .7, 18, 2)), withAlpha(col.muted, 166), []float32{3, 2})
	}
	return c.img
}

// dashed splits a polyline into dashes.
func dashed(pts [][2]float32, on, off float32) [][][2]float32 {
	var out [][][2]float32
	var cur [][2]float32
	drawing, left := true, on
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		seg := float32(math.Hypot(float64(b[0]-a[0]), float64(b[1]-a[1])))
		pos := float32(0)
		if drawing && len(cur) == 0 {
			cur = append(cur, a)
		}
		for seg-pos > left {
			pos += left
			t := pos / seg
			p := [2]float32{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t}
			if drawing {
				cur = append(cur, p)
				out = append(out, cur)
				cur = nil
				left = off
			} else {
				cur = [][2]float32{p}
				left = on
			}
			drawing = !drawing
		}
		left -= seg - pos
		if drawing {
			cur = append(cur, b)
		}
	}
	if len(cur) > 1 {
		out = append(out, cur)
	}
	return out
}

// mapLabel is a text label in world pixels at the raster zoom.
type mapLabel struct {
	text     string
	x, y     float64 // world pixel position
	angle    float64 // radians, for road names
	size     float64
	col      color.RGBA
	dot      bool
	priority float64 // lower is placed first
}

// tileLabels collects label candidates of one vector tile for raster zoom z.
func tileLabels(t mvtTile, z, vz, vx, vy int, dark bool) []mapLabel {
	col := basemapColors(dark)
	mz := styleZoom(z)
	var out []mapLabel
	scale := math.Exp2(float64(z - vz)) // world pixels per vector tile pixel unit at 256/extent
	name := func(f *mvtFeature) string {
		if s := f.str("name:ja"); s != "" {
			return s
		}
		if s := f.str("name"); s != "" {
			return s
		}
		return f.str("name:latin")
	}
	world := func(l *mvtLayer, p [2]float32) (float64, float64) {
		k := 256 * scale / float64(l.extent)
		return (float64(vx)*256*scale + float64(p[0])*k), (float64(vy)*256*scale + float64(p[1])*k)
	}
	if l := t["place"]; l != nil {
		for i := range l.features {
			f := &l.features[i]
			if f.typ != 1 || len(f.geom) == 0 || len(f.geom[0]) == 0 {
				continue
			}
			n := name(f)
			if n == "" {
				continue
			}
			x, y := world(l, f.geom[0][0])
			rank := f.num("rank", 99)
			switch f.str("class") {
			case "country", "state", "city":
				if mz >= 3 {
					out = append(out, mapLabel{text: n, x: x, y: y, size: linear(mz, 9, 15, 15, 18), col: col.text, priority: rank})
				}
			case "town":
				if mz >= 9 {
					out = append(out, mapLabel{text: n, x: x, y: y, size: linear(mz, 9, 11, 15, 14), col: col.muted, priority: 100 + rank})
				}
			case "village", "suburb", "quarter", "neighbourhood", "hamlet":
				if mz >= 13 {
					out = append(out, mapLabel{text: n, x: x, y: y, size: linear(mz, 13, 10, 18, 13), col: col.muted, priority: 200 + rank})
				}
			}
		}
	}
	if l := t["poi"]; l != nil && mz >= 15 {
		for i := range l.features {
			f := &l.features[i]
			rank := f.num("rank", 99)
			if f.typ != 1 || len(f.geom) == 0 || len(f.geom[0]) == 0 || (mz < 17 && rank > 3) {
				continue
			}
			n := name(f)
			if n == "" {
				continue
			}
			x, y := world(l, f.geom[0][0])
			out = append(out, mapLabel{text: n, x: x, y: y, size: linear(mz, 15, 10, 19, 12), col: col.poi, dot: true, priority: 400 + rank})
		}
	}
	if l := t["transportation_name"]; l != nil && mz >= 13 {
		for i := range l.features {
			f := &l.features[i]
			k := f.str("class")
			if f.typ != 2 || k == "path" || k == "track" || k == "rail" || k == "transit" {
				continue
			}
			n := name(f)
			if n == "" {
				continue
			}
			// Longest straight segment carries the name.
			best, ba, bb := 0.0, [2]float32{}, [2]float32{}
			for _, line := range f.geom {
				for j := 1; j < len(line); j++ {
					d := math.Hypot(float64(line[j][0]-line[j-1][0]), float64(line[j][1]-line[j-1][1]))
					if d > best {
						best, ba, bb = d, line[j-1], line[j]
					}
				}
			}
			ax, ay := world(l, ba)
			bx, by := world(l, bb)
			ang := math.Atan2(by-ay, bx-ax)
			if ang > math.Pi/2 {
				ang -= math.Pi
			} else if ang < -math.Pi/2 {
				ang += math.Pi
			}
			pri := 300.0
			switch k {
			case "motorway", "trunk", "primary":
				pri = 250
			}
			out = append(out, mapLabel{text: n, x: (ax + bx) / 2, y: (ay + by) / 2, angle: ang, size: linear(mz, 13, 10, 18, 13), col: col.text, priority: pri + 1/(1+math.Hypot(bx-ax, by-ay))})
		}
	}
	return out
}
