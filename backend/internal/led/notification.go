package led

import (
	"encoding/json"
	"errors"
	"github.com/np1-control-center/ncc/internal/config"
	"math"
	"os"
	"time"
)

const NotificationIdlePath = "/run/np1-control/led-notification-idle.json"

// Quiet the ordinary animation after an announcement, until the owner next
// changes LED settings. Only tmpfs is touched; the saved preset is preserved.
func QuietAfterNotification() error {
	c, e := Load()
	if e != nil {
		return e
	}
	return config.Atomic(NotificationIdlePath, map[string]int{"revision": c.Revision})
}
func ReadNotificationIdle(path string, revision int) (bool, error) {
	if e := screenPath(path, false); e != nil {
		return true, e
	}
	b, e := os.ReadFile(path)
	if os.IsNotExist(e) {
		return false, nil
	}
	if e != nil {
		return true, e
	}
	var v struct {
		Revision int `json:"revision"`
	}
	if e = json.Unmarshal(b, &v); e != nil {
		return true, e
	}
	return v.Revision == revision, nil
}

// Notification is a temporary overlay; it never changes the saved LED settings.
type Notification struct {
	Mode       string `json:"mode"`   // off, solid, rainbow
	Effect     string `json:"effect"` // steady, breathe, flow (solid only)
	RGB        [3]int `json:"rgb"`
	Brightness int    `json:"brightness"`
}

func (n Notification) Enabled() bool { return n.Mode != "" && n.Mode != "off" }
func (n Notification) Normalize() (Notification, error) {
	if n.Mode == "" {
		n.Mode = "off"
		if n.RGB == [3]int{} {
			n.RGB = [3]int{64, 160, 255}
		}
	}
	if n.Effect == "" {
		n.Effect = "breathe"
	}
	if n.Brightness == 0 {
		n.Brightness = 180
	}
	if n.Mode != "off" && n.Mode != "solid" && n.Mode != "rainbow" || n.Effect != "steady" && n.Effect != "breathe" && n.Effect != "flow" || n.Brightness < 1 || n.Brightness > 255 {
		return n, errors.New("LED通知の色・動作・明るさを確認してください")
	}
	for _, v := range n.RGB {
		if v < 0 || v > 255 {
			return n, errors.New("LED通知のRGB値が不正です")
		}
	}
	return n, nil
}

// NotificationZones uses a continuous envelope and a wrapped Gaussian trail,
// rather than switching individual LEDs abruptly on/off.
func NotificationZones(n Notification, elapsed time.Duration) (zones [5][3]int) {
	t := elapsed.Seconds()
	for z := range zones {
		if n.Mode == "rainbow" {
			zones[z] = RGB(t/3+float64(z)/5, n.Brightness)
			continue
		}
		level := 1.0
		switch n.Effect {
		case "breathe":
			level = .08 + .92*(1-math.Cos(t*math.Pi))/2
		case "flow":
			position := math.Mod(t*2.5, 5)
			d := math.Abs(float64(z) - position)
			d = math.Min(d, 5-d)
			level = .04 + .96*math.Exp(-d*d/1.1)
		}
		for c, v := range n.RGB {
			zones[z][c] = int(math.Round(float64(v) * float64(n.Brightness) / 255 * level))
		}
	}
	return
}

// SetNotification shares the worker's existing frame channel. Normal screen
// frames continue to be accepted while hidden, so the newest one is restored.
func (s *ScreenStore) SetNotification(zones [5][3]int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	frame := ScreenFrame{ScreenUpdate{Sequence: 1, Zones: zones}, now.UTC(), now.Add(ScreenIdleTimeout).UTC()}
	if e := screenPath(s.path, true); e != nil {
		return e
	}
	if e := s.save(s.path, frame); e != nil {
		return e
	}
	s.notification = true
	return nil
}

func (s *ScreenStore) EndNotification() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notification = false
	if s.latest != nil && s.now().Before(s.latest.ExpiresAt) {
		return s.save(s.path, *s.latest)
	}
	if e := os.Remove(s.path); e != nil && !os.IsNotExist(e) {
		return e
	}
	return nil
}
