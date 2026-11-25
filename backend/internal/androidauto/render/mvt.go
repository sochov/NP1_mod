package render

import (
	"encoding/binary"
	"errors"
	"math"
)

// Mapbox Vector Tile decoding (OpenMapTiles schema as served by OpenFreeMap).

type mvtFeature struct {
	typ   int // 1 point, 2 line, 3 polygon
	tags  map[string]any
	geom  [][][2]float32 // rings/lines/points in tile extent units
	minX  float32
	minY  float32
	maxX  float32
	maxY  float32
	label string
}

type mvtLayer struct {
	name     string
	extent   float32
	features []mvtFeature
}

type mvtTile map[string]*mvtLayer

var errMVT = errors.New("render: malformed vector tile")

type pbReader struct{ b []byte }

func (r *pbReader) varint() (uint64, error) {
	v, n := binary.Uvarint(r.b)
	if n <= 0 {
		return 0, errMVT
	}
	r.b = r.b[n:]
	return v, nil
}

// next returns the next field: number, wire type, varint value or bytes.
func (r *pbReader) next() (int, int, uint64, []byte, error) {
	k, err := r.varint()
	if err != nil {
		return 0, 0, 0, nil, err
	}
	num, wire := int(k>>3), int(k&7)
	switch wire {
	case 0:
		v, err := r.varint()
		return num, wire, v, nil, err
	case 1:
		if len(r.b) < 8 {
			return 0, 0, 0, nil, errMVT
		}
		v := binary.LittleEndian.Uint64(r.b)
		r.b = r.b[8:]
		return num, wire, v, nil, nil
	case 2:
		l, err := r.varint()
		if err != nil || l > uint64(len(r.b)) {
			return 0, 0, 0, nil, errMVT
		}
		b := r.b[:l]
		r.b = r.b[l:]
		return num, wire, 0, b, nil
	case 5:
		if len(r.b) < 4 {
			return 0, 0, 0, nil, errMVT
		}
		v := uint64(binary.LittleEndian.Uint32(r.b))
		r.b = r.b[4:]
		return num, wire, v, nil, nil
	}
	return 0, 0, 0, nil, errMVT
}

func packed(b []byte) ([]uint32, error) {
	var out []uint32
	r := pbReader{b}
	for len(r.b) > 0 {
		v, err := r.varint()
		if err != nil {
			return nil, err
		}
		out = append(out, uint32(v))
	}
	return out, nil
}

// decodeMVT decodes the layers named in want (nil: all).
func decodeMVT(data []byte, want map[string]bool) (mvtTile, error) {
	tile := mvtTile{}
	r := pbReader{data}
	for len(r.b) > 0 {
		num, wire, _, b, err := r.next()
		if err != nil {
			return nil, err
		}
		if num != 3 || wire != 2 {
			continue
		}
		layer, err := decodeLayer(b, want)
		if err != nil {
			return nil, err
		}
		if layer != nil {
			tile[layer.name] = layer
		}
	}
	return tile, nil
}

func decodeLayer(b []byte, want map[string]bool) (*mvtLayer, error) {
	l := &mvtLayer{extent: 4096}
	var keys []string
	var values []any
	var raw [][]byte
	r := pbReader{b}
	for len(r.b) > 0 {
		num, _, v, fb, err := r.next()
		if err != nil {
			return nil, err
		}
		switch num {
		case 1:
			l.name = string(fb)
			if want != nil && !want[l.name] {
				return nil, nil
			}
		case 2:
			raw = append(raw, fb)
		case 3:
			keys = append(keys, string(fb))
		case 4:
			values = append(values, decodeValue(fb))
		case 5:
			l.extent = float32(v)
		}
	}
	for _, fb := range raw {
		f, err := decodeFeature(fb, keys, values)
		if err != nil {
			return nil, err
		}
		if len(f.geom) > 0 {
			l.features = append(l.features, f)
		}
	}
	return l, nil
}

func decodeValue(b []byte) any {
	r := pbReader{b}
	for len(r.b) > 0 {
		num, _, v, vb, err := r.next()
		if err != nil {
			return nil
		}
		switch num {
		case 1:
			return string(vb)
		case 2:
			return float64(math.Float32frombits(uint32(v)))
		case 3:
			return math.Float64frombits(v)
		case 4, 5:
			return float64(int64(v))
		case 6:
			return float64(int64(v>>1) ^ -int64(v&1))
		case 7:
			return v != 0
		}
	}
	return nil
}

func decodeFeature(b []byte, keys []string, values []any) (mvtFeature, error) {
	f := mvtFeature{tags: map[string]any{}}
	var geom []uint32
	r := pbReader{b}
	for len(r.b) > 0 {
		num, wire, v, fb, err := r.next()
		if err != nil {
			return f, err
		}
		switch num {
		case 2:
			tags, err := packed(fb)
			if err != nil {
				return f, err
			}
			for i := 0; i+1 < len(tags); i += 2 {
				if int(tags[i]) < len(keys) && int(tags[i+1]) < len(values) {
					f.tags[keys[tags[i]]] = values[tags[i+1]]
				}
			}
		case 3:
			f.typ = int(v)
		case 4:
			if wire == 2 {
				if geom, err = packed(fb); err != nil {
					return f, err
				}
			}
		}
	}
	var x, y int32
	var cur [][2]float32
	f.minX, f.minY, f.maxX, f.maxY = math.MaxFloat32, math.MaxFloat32, -math.MaxFloat32, -math.MaxFloat32
	flush := func() {
		if len(cur) > 0 {
			f.geom = append(f.geom, cur)
			cur = nil
		}
	}
	for i := 0; i < len(geom); {
		cmd, count := geom[i]&7, int(geom[i]>>3)
		i++
		switch cmd {
		case 1, 2:
			for c := 0; c < count && i+1 < len(geom); c++ {
				x += int32(geom[i]>>1) ^ -int32(geom[i]&1)
				y += int32(geom[i+1]>>1) ^ -int32(geom[i+1]&1)
				i += 2
				if cmd == 1 && f.typ != 1 {
					flush()
				}
				p := [2]float32{float32(x), float32(y)}
				cur = append(cur, p)
				f.minX, f.minY, f.maxX, f.maxY = min(f.minX, p[0]), min(f.minY, p[1]), max(f.maxX, p[0]), max(f.maxY, p[1])
			}
		case 7:
			if len(cur) > 0 {
				cur = append(cur, cur[0])
			}
		default:
			return f, errMVT
		}
	}
	flush()
	return f, nil
}

func (f *mvtFeature) str(k string) string {
	if s, ok := f.tags[k].(string); ok {
		return s
	}
	return ""
}

func (f *mvtFeature) num(k string, fallback float64) float64 {
	if v, ok := f.tags[k].(float64); ok {
		return v
	}
	return fallback
}
