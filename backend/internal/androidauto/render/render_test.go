package render

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPlateSerial(t *testing.T) {
	for in, want := range map[string]string{"1234": "12-34", "5": "・・ ・5", "123": "・1 23", "": "", "12345": "23-45"} {
		if got := PlateSerial(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestLayoutKeepsRegionsInsideVisibleArea(t *testing.T) {
	for _, c := range [][4]int{{800, 480, 0, 0}, {1280, 720, 0, 0}, {1920, 1080, 0, 0}, {1280, 720, 160, 0}} {
		l := NewLayout(c[0], c[1], c[2], c[3])
		for _, r := range []image.Rectangle{l.Map, l.Camera, l.List} {
			if !r.In(l.Visible) || r.Empty() || r.Min.X%2 != 0 || r.Min.Y%2 != 0 {
				t.Fatalf("%v: region %v outside %v", c, r, l.Visible)
			}
		}
		if l.Camera.Dx()*9 < l.Camera.Dy()*16-32 || l.Camera.Dy() > l.Visible.Dy()/2 {
			t.Fatalf("camera taller than 16:9 or half the screen: %v", l.Camera)
		}
	}
}

func TestBlitConvertsColours(t *testing.T) {
	f := NewFrame(4, 4)
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 255, 255, 255, 255
	}
	f.Blit(img, 0, 0)
	if f.Y[0] != 235 || f.UV[0] != 128 || f.UV[1] != 128 {
		t.Fatalf("white -> Y %d U %d V %d", f.Y[0], f.UV[0], f.UV[1])
	}
	// Half-transparent black over white halves the luma above black.
	over := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for i := 0; i < len(over.Pix); i += 4 {
		over.Pix[i+3] = 128
	}
	f.Blend(over, 0, 0)
	if f.Y[0] < 120 || f.Y[0] > 130 {
		t.Fatalf("blend luma %d", f.Y[0])
	}
}

func TestScalerFillsCameraRegion(t *testing.T) {
	src := make([]byte, 1920*1080*3/2)
	for i := range src[:1920*1080] {
		src[i] = 200
	}
	for i := 1920 * 1080; i < len(src); i++ {
		src[i] = 90
	}
	f := NewFrame(800, 480)
	r := image.Rect(512, 0, 800, 162)
	NewScaler(1920, 1080, r).Draw(f, src)
	if f.Y[0*800+512] != 200 || f.Y[160*800+799] != 200 || f.Y[0] != 0 {
		t.Fatal("camera not drawn where expected")
	}
	if f.UV[(80)*800+600] != 90 {
		t.Fatal("chroma not drawn")
	}
}

// TestPreview writes PNG previews when AA_PREVIEW_DIR is set (network is used
// for map tiles). Positions and plates are fictional.
func TestPreview(t *testing.T) {
	dir := os.Getenv("AA_PREVIEW_DIR")
	if dir == "" {
		t.Skip("set AA_PREVIEW_DIR to write previews")
	}
	cam := make([]byte, 1920*1080*3/2)
	if b, e := os.ReadFile(filepath.Join(dir, "camera.nv12")); e == nil && len(b) >= len(cam) {
		copy(cam, b)
	} else {
		for y := 0; y < 1080; y++ {
			for x := 0; x < 1920; x++ {
				cam[y*1920+x] = byte(40 + y*150/1080)
			}
		}
		for i := 1920 * 1080; i < len(cam); i++ {
			cam[i] = 128
		}
	}
	now := time.Date(2026, 11, 26, 10, 30, 0, 0, jst)
	pos := LatLon{35.6812, 139.7671}
	trail := [][]LatLon{{{35.6745, 139.7630}, {35.6770, 139.7650}, {35.6790, 139.7662}, {35.6812, 139.7671}}}
	plates := []Plate{
		{ID: "a", FirstSeen: now.Add(-40 * time.Second), Region: "品川", Class: "300", Kana: "あ", Number: "1234", Confidence: 0.93, Sightings: 12, Located: true, Lat: 35.6800, Lon: 139.7666},
		{ID: "b", FirstSeen: now.Add(-5 * time.Minute), Region: "練馬", Class: "500", Kana: "さ", Number: "88", Confidence: 0.81, Sightings: 4, Matches: []string{"家族の車"}, Located: true, Lat: 35.6788, Lon: 139.7659},
		{ID: "c", FirstSeen: now.Add(-20 * time.Minute), Number: "7007", Confidence: 0.7, Sightings: 2, Share: "注意", Located: true, Lat: 35.6770, Lon: 139.7650, Estimated: true},
		{ID: "d", FirstSeen: now.Add(-26 * time.Hour), Region: "横浜", Class: "33", Kana: "ほ", Number: "4", Confidence: 0.66, Sightings: 3},
	}
	tiles := &Tiles{Dir: filepath.Join(dir, "tiles")}
	for _, c := range []struct {
		name     string
		w, h, mh int
		dark     bool
	}{{"800x480-dark", 800, 480, 0, true}, {"800x480-light", 800, 480, 0, false}, {"1280x720-dark", 1280, 720, 0, true}, {"1920x1080-light", 1920, 1080, 0, false}, {"1600x600-as-1280x720-dark", 1280, 720, 240, true}} {
		d := NewDashboard(NewLayout(c.w, c.h, 0, c.mh), tiles, 16)
		st := State{Position: &pos, Live: true, Trail: trail, Plates: plates, Sensor: "認識中", SensorTone: 1, Recording: "録画中", Dark: c.dark}
		d.SetState(st, now)
		// A new rule match arrives: it opens the notification card.
		hit := Plate{ID: "n", FirstSeen: now, Region: "多摩", Class: "330", Kana: "な", Number: "2525", Confidence: 0.9, Sightings: 3, Matches: []string{"家族の車"}, Located: true, Lat: 35.6805, Lon: 139.7690}
		st.Plates = append([]Plate{hit}, st.Plates...)
		st.Signal = &Signal{Color: "red", Text: "青まであと27秒", Estimate: true}
		d.SetState(st, now)
		for i := 0; i < 40; i++ { // let tiles arrive
			d.MarkMapDirty()
			d.Compose(context.Background(), now)
			time.Sleep(100 * time.Millisecond)
		}
		d.Camera(cam, now)
		writePNG(t, filepath.Join(dir, c.name+".png"), d.Frame())
	}
}

func writePNG(t *testing.T, path string, f *Frame) {
	img := image.NewRGBA(image.Rect(0, 0, f.W, f.H))
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			Y := float64(f.Y[y*f.W+x]) - 16
			U := float64(f.UV[(y/2)*f.W+(x&^1)]) - 128
			V := float64(f.UV[(y/2)*f.W+(x&^1)+1]) - 128
			r := 1.164*Y + 1.596*V
			g := 1.164*Y - 0.392*U - 0.813*V
			b := 1.164*Y + 2.017*U
			img.Set(x, y, color.RGBA{c8(r), c8(g), c8(b), 255})
		}
	}
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if err = png.Encode(out, img); err != nil {
		t.Fatal(err)
	}
}

func c8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func TestScalerRangesCoverEverythingDrawReads(t *testing.T) {
	full := make([]byte, 1920*1080*3/2)
	for i := range full {
		full[i] = byte(i*7 + i/1920)
	}
	for _, r := range []image.Rectangle{image.Rect(512, 0, 800, 162), image.Rect(1228, 0, 1920, 388), image.Rect(768, 0, 1280, 240), image.Rect(0, 0, 1920, 1080)} {
		s := NewScaler(1920, 1080, r)
		sparse := make([]byte, len(full))
		for _, rg := range s.Ranges() {
			copy(sparse[rg[0]:rg[1]], full[rg[0]:rg[1]])
		}
		a, b := NewFrame(1920, 1080), NewFrame(1920, 1080)
		s.Draw(a, full)
		s.Draw(b, sparse)
		if string(a.Bytes()) != string(b.Bytes()) {
			t.Fatalf("%v: ranges miss bytes Draw reads", r)
		}
	}
}

func BenchmarkScaler800(b *testing.B) {
	src := make([]byte, 1920*1080*3/2)
	f := NewFrame(800, 480)
	s := NewScaler(1920, 1080, image.Rect(512, 0, 800, 162))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Draw(f, src)
	}
}

func TestLogicalSizeAndStretch(t *testing.T) {
	if w, h := LogicalSize(1280, 720, 15000); w != 1280 || h != 480 {
		t.Fatalf("wide pixels: %dx%d", w, h)
	}
	if w, h := LogicalSize(800, 480, 7500); w != 600 || h != 480 {
		t.Fatalf("tall pixels: %dx%d", w, h)
	}
	if w, h := LogicalSize(800, 480, 10000); w != 800 || h != 480 {
		t.Fatalf("square: %dx%d", w, h)
	}
	src := NewFrame(1280, 480)
	for y := 0; y < 480; y++ {
		for x := 0; x < 1280; x++ {
			src.Y[y*1280+x] = byte(y / 2)
		}
	}
	out := NewStretcher(src, 1280, 720).Frame()
	if out.W != 1280 || out.H != 720 || out.Y[0] != 0 || out.Y[719*1280] < 235 || out.Y[360*1280] < 115 || out.Y[360*1280] > 125 {
		t.Fatalf("vertical stretch %d %d %d", out.Y[0], out.Y[360*1280], out.Y[719*1280])
	}
	hsrc := NewFrame(600, 480)
	for x := 0; x < 600; x++ {
		hsrc.Y[x] = byte(x * 255 / 599)
	}
	hout := NewStretcher(hsrc, 800, 480).Frame()
	if hout.Y[0] > 2 || hout.Y[799] < 250 {
		t.Fatalf("horizontal stretch %d %d", hout.Y[0], hout.Y[799])
	}
}

// BenchmarkMapRedraw measures one map redraw with tiles already in memory
// (as while driving: the position moves every 2 s). Needs AA_PREVIEW_DIR for tiles.
func BenchmarkMapRedraw(b *testing.B) {
	dir := os.Getenv("AA_PREVIEW_DIR")
	if dir == "" {
		b.Skip("set AA_PREVIEW_DIR")
	}
	tiles := &Tiles{Dir: filepath.Join(dir, "tiles")}
	d := NewDashboard(NewLayout(800, 480, 0, 0), tiles, 16)
	pos := LatLon{35.6812, 139.7671}
	d.SetState(State{Position: &pos, Live: true, Dark: true}, time.Now())
	for i := 0; i < 50; i++ {
		d.drawMap(context.Background())
		time.Sleep(50 * time.Millisecond)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pos.Lat += 0.00002
		d.state.Position = &pos
		d.drawMap(context.Background())
	}
}

func TestNotificationOnlyForNewMatches(t *testing.T) {
	d := NewDashboard(NewLayout(800, 480, 0, 0), nil, 16)
	now := time.Now()
	old := Plate{ID: "old", FirstSeen: now, Number: "1", Matches: []string{"既存"}}
	d.SetState(State{Plates: []Plate{old}}, now)
	if d.popup != nil {
		t.Fatal("a match already listed at start must not notify")
	}
	plain := Plate{ID: "p", FirstSeen: now, Number: "2"}
	d.SetState(State{Plates: []Plate{plain, old}}, now.Add(time.Second))
	if d.popup != nil {
		t.Fatal("a detection without a match must not notify")
	}
	plain.Matches = []string{"家族の車"}
	d.SetState(State{Plates: []Plate{plain, old}}, now.Add(2*time.Second))
	if d.popup == nil || d.popup.plate.ID != "p" {
		t.Fatal("a new rule match must notify")
	}
	d.SetState(State{Plates: []Plate{plain, old}}, now.Add(2*time.Second+popupSeconds*time.Second+time.Millisecond))
	if d.popup != nil {
		t.Fatal("the notification must close after popupSeconds")
	}
}
