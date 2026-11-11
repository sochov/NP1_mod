package main

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/np1-control-center/ncc/internal/modem"
)

// Separate opt-in mode; never enabled by a page read or background worker.
func runNativeAPNStatusProbe() error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	status, e := modem.ReadNativeAPNStatus(ctx)
	if outputError := json.NewEncoder(os.Stdout).Encode(status); outputError != nil {
		return outputError
	}
	return e
}
