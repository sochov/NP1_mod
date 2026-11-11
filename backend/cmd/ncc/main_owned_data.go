package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"github.com/np1-control-center/ncc/internal/modem"
)

// Metadata only: no QMI CID is opened and no network state is changed.
func runOwnedCellularPreflight() error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	proof, err := modem.InspectOwnedCellularPreflight(ctx)
	code := ""
	if err != nil {
		code = err.Error()
		if !regexp.MustCompile(`^OWNED_DATA_[A-Z0-9_]{1,128}$`).MatchString(code) {
			code = "OWNED_DATA_PREFLIGHT_FAILED"
		}
	}
	if e := json.NewEncoder(os.Stdout).Encode(struct {
		modem.OwnedCellularPreflight
		Code string `json:"code,omitempty"`
	}{proof, code}); e != nil {
		return e
	}
	if err != nil {
		return errors.New(code)
	}
	return nil
}

// A finite, explicitly staged session experiment. EOF and SIGTERM cancel the
// hold and drain Start/Stop cleanup; the driver is never killed/restarted here.
func runOwnedCellularSessionProbe() error {
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		return errors.New("OWNED_DATA_PARENT_PIPE_REQUIRED")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	go func() {
		var b [1]byte
		for {
			if _, e := os.Stdin.Read(b[:]); e != nil {
				cancel()
				return
			}
		}
	}()
	result, probeErr := modem.ProbeOwnedCellularSession(ctx)
	code := ""
	if probeErr != nil {
		code = probeErr.Error()
		if !regexp.MustCompile(`^OWNED_DATA_[A-Z0-9_]{1,128}$`).MatchString(code) {
			code = "OWNED_DATA_SESSION_PROBE_FAILED"
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(struct {
		modem.OwnedDataSessionResult
		Code string `json:"code,omitempty"`
	}{result, code}); err != nil {
		return err
	}
	if probeErr != nil {
		return errors.New(code)
	}
	return nil
}
