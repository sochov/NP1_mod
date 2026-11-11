package api

import (
	"github.com/np1-control-center/ncc/internal/config"
	"github.com/np1-control-center/ncc/internal/radio"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoggerEnabledWithoutConsentAndPersistsUniqueMeasuredSamples(t *testing.T) {
	directory := t.TempDir()
	server := &Server{cfg: config.Config{StateDir: directory}, radioStore: radio.NewHistory(directory)}
	server.loadLogger()
	if !server.logger.Enabled {
		t.Fatal("owner logging instruction not applied")
	}
	at := time.Now().UTC()
	rsrp := float64(-85)
	sample := radio.Observation{At: at, Source: "synthetic-unit-measurement", RSRP: &rsrp}
	if e := server.storeRadio(sample, at); e != nil {
		t.Fatal(e)
	}
	if e := server.storeRadio(sample, at.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	if server.logger.Stored != 1 || server.logger.Located != 0 {
		t.Fatal(server.logger)
	}
	if _, e := os.Stat(filepath.Join(directory, "radio-observations.jsonl")); e != nil {
		t.Fatal(e)
	}
	reloaded := &Server{cfg: config.Config{StateDir: directory}, radioStore: radio.NewHistory(directory)}
	reloaded.loadLogger()
	if e := reloaded.storeRadio(sample, at.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	if reloaded.logger.Stored != 1 {
		t.Fatal("sample duplicated after restart")
	}
	sample.At = at.Add(-3 * time.Minute)
	_ = reloaded.storeRadio(sample, at)
	sample.At = at.Add(3 * time.Minute)
	_ = reloaded.storeRadio(sample, at)
	if reloaded.logger.Stored != 1 {
		t.Fatal("invalid measurement time logged")
	}
}
