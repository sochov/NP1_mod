package adapters

import (
	"image"
	"testing"
)

func TestVideoRangeNV12(t *testing.T) {
	for _, tc := range []struct {
		y, u, v uint8
		r, g, b uint8
	}{{16, 128, 128, 0, 0, 0}, {235, 128, 128, 255, 255, 255}, {81, 90, 240, 255, 0, 0}} {
		r, g, b := limitedRGB(tc.y, tc.u, tc.v)
		if abs(int(r)-int(tc.r)) > 1 || abs(int(g)-int(tc.g)) > 1 || abs(int(b)-int(tc.b)) > 1 {
			t.Fatalf("%+v => %d,%d,%d", tc, r, g, b)
		}
	}
}
func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
func TestHighlightBalanceOnlyWithReferencePixels(t *testing.T) {
	out := image.NewRGBA(image.Rect(0, 0, 80, 80))
	for p := 0; p < len(out.Pix); p += 4 {
		out.Pix[p] = 210
		out.Pix[p+1] = 170
		out.Pix[p+2] = 200
		out.Pix[p+3] = 255
	}
	balanceHighlights(out)
	if abs(int(out.Pix[0])-int(out.Pix[1])) > 1 || abs(int(out.Pix[1])-int(out.Pix[2])) > 1 {
		t.Fatal("neutral highlight cast not corrected")
	}
	dark := image.NewRGBA(image.Rect(0, 0, 80, 80))
	for p := 0; p < len(dark.Pix); p += 4 {
		dark.Pix[p] = 40
		dark.Pix[p+1] = 20
		dark.Pix[p+2] = 30
	}
	balanceHighlights(dark)
	if dark.Pix[0] != 40 || dark.Pix[1] != 20 || dark.Pix[2] != 30 {
		t.Fatal("invented balance without highlights")
	}
}
