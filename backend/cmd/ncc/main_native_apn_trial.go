package main

import (
	"context"
	"encoding/json"
	"github.com/np1-control-center/ncc/internal/modem"
	"os"
	"time"
)

func runNativeAPNSameProfileTrial() error {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	result, e := modem.TrialNativeAPNSameProfile1(ctx)
	if outputError := json.NewEncoder(os.Stdout).Encode(result); outputError != nil {
		return outputError
	}
	return e
}
