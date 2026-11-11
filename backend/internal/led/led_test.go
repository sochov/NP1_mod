package led

import (
	"math"
	"testing"
)

func TestKnownRainbowAnchors(t *testing.T) {
	for _, tc := range []struct {
		h   float64
		rgb [3]int
	}{{0, [3]int{180, 0, 0}}, {1.0 / 3, [3]int{0, 180, 0}}, {2.0 / 3, [3]int{0, 0, 180}}, {1, [3]int{180, 0, 0}}} {
		if got := RGB(tc.h, 180); got != tc.rgb {
			t.Fatalf("%v: %v", tc.h, got)
		}
	}
	for i := 0; i < 1000; i++ {
		for _, v := range RGB(float64(i)/111-4, 180) {
			if v < 0 || v > 180 {
				t.Fatal("channel out of bounds")
			}
		}
	}
}
func TestRejectUnboundedLEDCommands(t *testing.T) {
	for _, mutate := range []func(*Settings){func(c *Settings) { c.FPS = 1000 }, func(c *Settings) { c.Speed = math.Inf(1) }, func(c *Settings) { c.Mode = "shell" }, func(c *Settings) { c.RGB[0] = -1 }} {
		c := Default()
		mutate(&c)
		if Validate(c) == nil {
			t.Fatal("unsafe LED input accepted")
		}
	}
}
