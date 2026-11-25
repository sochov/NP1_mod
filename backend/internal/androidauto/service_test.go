package androidauto

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSettingsNormalize(t *testing.T) {
	s := Settings{Enabled: true, MaxResolution: "4k", FPS: 7, BitrateKbps: 1, Theme: "x", MapZoom: 30}.Normalize()
	if !s.Enabled || s.MaxResolution != "800x480" || s.FPS != 15 || s.BitrateKbps != 500 || s.Theme != "auto" || s.MapZoom != 18 {
		t.Fatalf("%+v", s)
	}
	if d := DefaultSettings(); d.Normalize() != d {
		t.Fatal("defaults are not normal")
	}
}

func TestReadSettingsFallsBack(t *testing.T) {
	dir := t.TempDir()
	if s := ReadSettings(dir); s != DefaultSettings() {
		t.Fatalf("%+v", s)
	}
	os.MkdirAll(filepath.Join(dir, "androidauto"), 0700)
	os.WriteFile(filepath.Join(dir, "androidauto", "settings.json"), []byte(`{"enabled":true,"maxResolution":"1280x720","fps":30,"bitrateKbps":3000,"theme":"dark","mapZoom":15,"revision":4}`), 0600)
	s := ReadSettings(dir)
	if !s.Enabled || s.MaxResolution != "1280x720" || s.Revision != 4 {
		t.Fatalf("%+v", s)
	}
	if w, h := s.maxSize(); w != 1280 || h != 720 {
		t.Fatal(w, h)
	}
}

func TestSplitConfig(t *testing.T) {
	au := []byte{0, 0, 0, 1, 0x09, 0x10, 0, 0, 0, 1, 0x67, 1, 2, 0, 0, 0, 1, 0x68, 3, 0, 0, 1, 0x65, 9, 9}
	cfg, rest := splitConfig(au)
	if !bytes.Equal(cfg, []byte{0, 0, 0, 1, 0x67, 1, 2, 0, 0, 0, 1, 0x68, 3}) {
		t.Fatalf("config %x", cfg)
	}
	if !bytes.Equal(rest, []byte{0, 0, 0, 1, 0x67, 1, 2, 0, 0, 0, 1, 0x68, 3, 0, 0, 0, 1, 0x65, 9, 9}) {
		t.Fatalf("rest %x", rest)
	}
	if !hasNAL(au, 5) || hasNAL(au, 1) {
		t.Fatal("NAL detection")
	}
}

func TestEncoderArgs(t *testing.T) {
	args := EncoderConfig{Width: 800, Height: 480, FPS: 15, BitrateKbps: 2500, IDRSeconds: 2}.Args()
	joined := ""
	for _, a := range args {
		joined += a + " "
	}
	for _, want := range []string{"blocksize=576000", "width=800", "framerate=15/1", "target-bitrate=2500000", "interval-intraframes=30", "profile=baseline", "gdppay"} {
		if !bytes.Contains([]byte(joined), []byte(want)) {
			t.Fatalf("missing %s in %s", want, joined)
		}
	}
}

func TestDarkThemeFollowsTheSun(t *testing.T) {
	tokyo := struct{ lat, lon float64 }{35.68, 139.77}
	noon := time.Date(2026, 6, 21, 3, 0, 0, 0, time.UTC)      // 12:00 JST
	midnight := time.Date(2026, 6, 21, 15, 0, 0, 0, time.UTC) // 00:00 JST
	if e := sunElevation(tokyo.lat, tokyo.lon, noon); e < 70 {
		t.Fatalf("noon elevation %.1f", e)
	}
	if e := sunElevation(tokyo.lat, tokyo.lon, midnight); e > -20 {
		t.Fatalf("midnight elevation %.1f", e)
	}
	if darkTheme("light", nil, midnight) || !darkTheme("dark", nil, noon) {
		t.Fatal("fixed themes")
	}
}

func TestDefaultAcceptOnlyHotspotAndLoopback(t *testing.T) {
	for addr, want := range map[string]bool{"192.168.226.20": true, "127.0.0.1": true, "192.168.225.5": false, "100.100.1.2": false} {
		if got := DefaultAccept(&net.TCPAddr{IP: net.ParseIP(addr), Port: 1}); got != want {
			t.Errorf("%s: %v", addr, got)
		}
	}
}
