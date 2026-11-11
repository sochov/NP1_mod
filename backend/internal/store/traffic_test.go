package store

import (
	"testing"
	"time"
)

func TestTrafficResetWrapAndPersistence(t *testing.T) {
	if Delta(100, 150) != 50 || Delta(100, 3) != 3 || Delta(^uint64(0)-4, 3) != 8 {
		t.Fatal("counter correction failed")
	}
	dir := t.TempDir()
	at := time.Date(2026, 11, 11, 1, 0, 0, 0, time.UTC)
	feed := func(tstore *Traffic, at time.Time, boot, rx, tx string) {
		tstore.Feed(at, boot, []map[string]string{{"interface": "wlan0", "rxBytes": rx, "txBytes": tx}})
	}
	s := NewTraffic(dir)
	feed(s, at, "a", "1000", "2000")
	feed(s, at.Add(10*time.Second), "a", "1100", "2040")
	if e := s.Save(); e != nil {
		t.Fatal(e)
	}
	s = NewTraffic(dir)
	feed(s, at.Add(20*time.Second), "b", "20", "10")
	v := s.Summary("wlan0", at.Add(20*time.Second))
	if v["todayRX"] != "120" || v["todayTX"] != "50" {
		t.Fatal(v)
	}
	feed(s, at.Add(30*time.Second), "b", "invalid", "10")
	v = s.Summary("wlan0", at.Add(30*time.Second))
	if v["todayRX"] != "120" {
		t.Fatal("invalid counter counted")
	}
}
