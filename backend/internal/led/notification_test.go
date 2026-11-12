package led

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestNotificationEnvelopesAndValidation(t *testing.T) {
	n := Notification{Mode: "solid", Effect: "breathe", RGB: [3]int{255, 80, 0}, Brightness: 200}
	low, high := NotificationZones(n, 0), NotificationZones(n, time.Second)
	if low[0][0] >= high[0][0] || high[0][0] != 200 {
		t.Fatalf("breathing %v / %v", low, high)
	}
	n.Effect = "flow"
	a, b := NotificationZones(n, 0), NotificationZones(n, 400*time.Millisecond)
	if a[0][0] <= a[1][0] || b[1][0] <= b[0][0] {
		t.Fatalf("flow does not move: %v / %v", a, b)
	}
	n.Mode = "rainbow"
	if a, b = NotificationZones(n, 0), NotificationZones(n, time.Second); a == b || a[0] == a[1] {
		t.Fatal("rainbow is static")
	}
	for _, bad := range []Notification{{Mode: "shell"}, {Mode: "solid", Effect: "strobe"}, {Brightness: 256}, {RGB: [3]int{-1, 0, 0}}} {
		if _, e := bad.Normalize(); e == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
}

func TestNotificationRestoresNewestScreenFrameAndHonorsExpiry(t *testing.T) {
	s := NewScreenStore(filepath.Join(t.TempDir(), "frame.json"))
	now := time.Now()
	s.now = func() time.Time { return now }
	session, e := s.Begin()
	if e != nil {
		t.Fatal(e)
	}
	first := ScreenUpdate{Sequence: 1, Zones: [5][3]int{{20, 40, 60}}}
	if e = s.Update(context.Background(), session.ID, first); e != nil {
		t.Fatal(e)
	}
	alert := [5][3]int{{255, 0, 0}}
	if e = s.SetNotification(alert); e != nil {
		t.Fatal(e)
	}
	now = now.Add(ScreenFrameInterval)
	second := ScreenUpdate{Sequence: 2, Zones: [5][3]int{{60, 40, 20}}}
	if e = s.Update(context.Background(), session.ID, second); e != nil {
		t.Fatal(e)
	}
	frame, _, _ := ReadScreenFrame(s.path, now)
	if frame.Zones != alert {
		t.Fatal("normal update overwrote notification")
	}
	if e = s.EndNotification(); e != nil {
		t.Fatal(e)
	}
	frame, active, e := ReadScreenFrame(s.path, now)
	if e != nil || !active || frame.Zones != second.Zones {
		t.Fatalf("latest frame not restored: %+v %v", frame, e)
	}
	s.SetNotification(alert)
	now = now.Add(2 * time.Second)
	s.EndNotification()
	if _, active, e = ReadScreenFrame(s.path, now); e != nil || active {
		t.Fatal("expired screen frame was replayed")
	}
}
