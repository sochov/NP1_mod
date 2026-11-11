package main

import (
	"context"
	"encoding/json"
	"github.com/np1-control-center/ncc/internal/modem"
	"os"
	"time"
)

func runNativeAPNNetworkCheck() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, e := modem.CheckNativeAPNNetwork(ctx)
	if encodeError := json.NewEncoder(os.Stdout).Encode(out); encodeError != nil {
		return encodeError
	}
	return e
}
