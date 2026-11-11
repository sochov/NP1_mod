package led

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"time"

	"github.com/np1-control-center/ncc/internal/config"
)

type managedDependencies struct {
	load    func() (Settings, error)
	write   func(string, string) error
	publish func(State) error
	now     func() time.Time
	screen  func(time.Time) (ScreenFrame, bool, error)
}

// RunManaged keeps one worker alive, including while dark. Atomic configuration
// changes do not start a new systemd service or reset its failure protection.
func RunManaged(ctx context.Context, base string) error {
	return runManaged(ctx, managedDependencies{load: Load, write: func(name, value string) error { return writeAttribute(filepath.Join(base, name), value) }, publish: func(state State) error { return config.Atomic(RuntimeState, state) }, now: time.Now, screen: func(now time.Time) (ScreenFrame, bool, error) { return ReadScreenFrame(ScreenFramePath, now) }})
}

func runManaged(ctx context.Context, d managedDependencies) (result error) {
	if d.load == nil || d.write == nil || d.publish == nil {
		return errors.New("LED controller dependencies unavailable")
	}
	if d.now == nil {
		d.now = time.Now
	}
	c, e := d.load()
	if e != nil {
		return e
	}
	if e = Validate(c); e != nil {
		return e
	}
	state := State{Revision: c.Revision, Mode: c.Mode, Readback: "unsupported; command state only"}
	off := func() error {
		first := d.write("led_status", "0\n")
		for zone := 1; zone <= 5; zone++ {
			if e := d.write("led_value", fmt.Sprintf("%d 0 0 0\n", zone)); e != nil && first == nil {
				first = e
			}
		}
		return first
	}
	defer func() {
		if e := off(); e != nil && result == nil {
			result = e
		}
		state.Mode = "off"
		state.Running = false
		state.Zones = nil
		state.ScreenSequence = 0
		state.AppliedAt = d.now().UTC()
		if result != nil {
			state.Error = result.Error()
		}
		_ = d.publish(state)
	}()
	apply := func(next Settings) error {
		if next.Mode == "off" {
			if e := off(); e != nil {
				return e
			}
		} else {
			if e := d.write("led_mode", "1\n"); e != nil {
				return e
			}
			if e := d.write("led_status", "1\n"); e != nil {
				return e
			}
		}
		c = next
		state.Revision = c.Revision
		state.Mode = c.Mode
		state.Running = c.Mode != "off"
		state.Zones = nil
		state.ScreenSequence = 0
		state.Error = ""
		return nil
	}
	if e = apply(c); e != nil {
		return e
	}
	started := d.now()
	nextLoad := time.Time{}
	nextFrame := time.Time{}
	lastPublish := time.Time{}
	dirty := true
	var screen ScreenFrame
	screenActive := false
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		now := d.now()
		if !now.Before(nextLoad) {
			next, loadError := d.load()
			if loadError == nil {
				loadError = Validate(next)
			}
			if loadError != nil {
				if state.Error != "LED configuration unavailable" {
					state.Error = "LED configuration unavailable"
					dirty = true
				}
			} else if next != c {
				if e = apply(next); e != nil {
					return e
				}
				started = now
				nextFrame = time.Time{}
				dirty = true
			} else if state.Error != "" {
				state.Error = ""
				dirty = true
			}
			nextLoad = now.Add(100 * time.Millisecond)
			if d.screen != nil {
				frame, active, readError := d.screen(now)
				active = active && readError == nil
				if active {
					if !screenActive || state.Mode != "screen" {
						if e = d.write("led_mode", "1\n"); e != nil {
							return e
						}
						if e = d.write("led_status", "1\n"); e != nil {
							return e
						}
						nextFrame = time.Time{}
					}
					if frame.Sequence != screen.Sequence || frame.UpdatedAt != screen.UpdatedAt {
						nextFrame = time.Time{}
						dirty = true
					}
					screen = frame
					state.Mode, state.Running = "screen", true
				} else if screenActive {
					if e = apply(c); e != nil {
						return e
					}
					started = now
					nextFrame = time.Time{}
					dirty = true
				}
				screenActive = active
			}
		}
		if (c.Mode != "off" || screenActive) && !now.Before(nextFrame) {
			for zone := 1; zone <= 5; zone++ {
				rgb := RGB(now.Sub(started).Seconds()*c.Speed/360+float64(zone-1)/5, c.Brightness)
				if c.Mode == "solid" {
					for index, value := range c.RGB {
						rgb[index] = int(math.Round(float64(value) * float64(c.Brightness) / 255))
					}
				}
				if screenActive {
					rgb = screen.Zones[zone-1]
				}
				if e = d.write("led_value", fmt.Sprintf("%d %d %d %d\n", zone, rgb[0], rgb[1], rgb[2])); e != nil {
					return e
				}
			}
			state.Frames++
			if screenActive {
				state.Zones = append([][3]int(nil), screen.Zones[:]...)
				state.ScreenSequence = screen.Sequence
			}
			delay := time.Second / time.Duration(c.FPS)
			if c.Mode == "solid" {
				delay = time.Second
			}
			if screenActive {
				delay = ScreenFrameInterval
			}
			nextFrame = now.Add(delay)
		}
		if dirty || now.Sub(lastPublish) >= time.Second {
			state.AppliedAt = now.UTC()
			if e = d.publish(state); e != nil {
				return e
			}
			lastPublish = now
			dirty = false
		}
		delay := nextLoad.Sub(d.now())
		if c.Mode != "off" || screenActive {
			if frameDelay := nextFrame.Sub(d.now()); frameDelay < delay {
				delay = frameDelay
			}
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
