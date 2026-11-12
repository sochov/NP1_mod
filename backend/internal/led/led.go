// Package led controls only the five ENE6K582 zones, following the owner's gaming-mode reference.
package led

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/np1-control-center/ncc/internal/config"
	"math"
	"os"
	"path/filepath"
	"time"
)

const Directory = "/overlay/owner-apps/np1-control/private/led"
const RuntimeState = "/run/np1-control/led-state.json"
const Sysfs = "/sys/devices/platform/soc/88c000.i2c/i2c-2/2-0066"

type Settings struct {
	Revision   int     `json:"revision"`
	Mode       string  `json:"mode"`
	Brightness int     `json:"brightness"`
	Speed      float64 `json:"speed"`
	FPS        int     `json:"fps"`
	RGB        [3]int  `json:"rgb"`
	Autostart  bool    `json:"autostart"`
}
type State struct {
	Revision         int       `json:"revision"`
	Mode             string    `json:"mode"`
	AppliedAt        time.Time `json:"appliedAt"`
	Frames           uint64    `json:"frames"`
	Running          bool      `json:"running"`
	Readback         string    `json:"hardwareReadback"`
	Error            string    `json:"error,omitempty"`
	Zones            [][3]int  `json:"zones,omitempty"`
	ScreenSequence   uint64    `json:"screenSequence,omitempty"`
	NotificationIdle bool      `json:"notificationIdle,omitempty"`
}

func Default() Settings {
	return Settings{Revision: 1, Mode: "off", Brightness: 180, Speed: 240, FPS: 15, RGB: [3]int{255, 255, 255}}
}
func Validate(c Settings) error {
	if c.Mode != "off" && c.Mode != "solid" && c.Mode != "rainbow" {
		return errors.New("invalid LED mode")
	}
	if c.Brightness < 1 || c.Brightness > 255 || c.FPS < 1 || c.FPS > 18 || math.IsNaN(c.Speed) || math.IsInf(c.Speed, 0) || c.Speed < -360 || c.Speed > 360 || c.Speed == 0 {
		return errors.New("LED parameters out of range")
	}
	for _, v := range c.RGB {
		if v < 0 || v > 255 {
			return errors.New("RGB out of range")
		}
	}
	return nil
}
func Load() (Settings, error) {
	c := Default()
	b, e := os.ReadFile(filepath.Join(Directory, "config.json"))
	if os.IsNotExist(e) {
		return c, nil
	}
	if e != nil {
		return c, e
	}
	if e = json.Unmarshal(b, &c); e != nil {
		return c, e
	}
	return c, Validate(c)
}
func RGB(hue float64, brightness int) [3]int {
	position := (hue - math.Floor(hue)) * 6
	sector := int(position) % 6
	f := position - float64(sector)
	var rgb [3]float64
	switch sector {
	case 0:
		rgb = [3]float64{1, f, 0}
	case 1:
		rgb = [3]float64{1 - f, 1, 0}
	case 2:
		rgb = [3]float64{0, 1, f}
	case 3:
		rgb = [3]float64{0, 1 - f, 1}
	case 4:
		rgb = [3]float64{f, 0, 1}
	case 5:
		rgb = [3]float64{1, 0, 1 - f}
	}
	out := [3]int{}
	for i, v := range rgb {
		out[i] = int(math.Round(math.Pow(v, 1.8) * float64(brightness)))
	}
	return out
}
func writeAttribute(path, value string) error {
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if e != nil {
		return e
	}
	_, e = f.WriteString(value)
	ce := f.Close()
	if e != nil {
		return e
	}
	return ce
}
func writeZone(base string, zone int, rgb [3]int) error {
	return writeAttribute(base+"/led_value", fmt.Sprintf("%d %d %d %d\n", zone, rgb[0], rgb[1], rgb[2]))
}
func Off(base string) error {
	first := writeAttribute(base+"/led_status", "0\n")
	for z := 1; z <= 5; z++ {
		if e := writeZone(base, z, [3]int{}); e != nil && first == nil {
			first = e
		}
	}
	return first
}
func Run(ctx context.Context, c Settings, base string) (result error) {
	if e := Validate(c); e != nil {
		return e
	}
	for _, name := range []string{"led_mode", "led_status", "led_value"} {
		if _, e := os.Stat(base + "/" + name); e != nil {
			return e
		}
	}
	state := State{Revision: c.Revision, Mode: c.Mode, AppliedAt: time.Now().UTC(), Readback: "unsupported; command state only"}
	defer func() {
		if e := Off(base); e != nil && result == nil {
			result = e
		}
		state.Mode = "off"
		state.Running = false
		state.AppliedAt = time.Now().UTC()
		if result != nil {
			state.Error = result.Error()
		}
		_ = config.Atomic(RuntimeState, state)
	}()
	if c.Mode == "off" {
		return nil
	}
	if e := writeAttribute(base+"/led_mode", "1\n"); e != nil {
		return e
	}
	if e := writeAttribute(base+"/led_status", "1\n"); e != nil {
		return e
	}
	state.Running = true
	started := time.Now()
	lastState := time.Time{}
	interval := time.Second / time.Duration(c.FPS)
	for {
		begin := time.Now()
		for zone := 1; zone <= 5; zone++ {
			rgb := RGB(time.Since(started).Seconds()*c.Speed/360+float64(zone-1)/5, c.Brightness)
			if c.Mode == "solid" {
				for i, v := range c.RGB {
					rgb[i] = int(math.Round(float64(v) * float64(c.Brightness) / 255))
				}
			}
			if e := writeZone(base, zone, rgb); e != nil {
				return e
			}
		}
		state.Frames++
		if time.Since(lastState) > time.Second {
			state.AppliedAt = time.Now().UTC()
			if e := config.Atomic(RuntimeState, state); e != nil {
				return e
			}
			lastState = time.Now()
		}
		delay := interval - time.Since(begin)
		if c.Mode == "solid" {
			delay = time.Second
		}
		if delay < time.Millisecond {
			delay = time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
