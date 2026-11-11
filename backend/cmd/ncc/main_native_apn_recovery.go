package main

import (
	"context"
	"encoding/json"
	"github.com/np1-control-center/ncc/internal/modem"
	"os"
	"time"
)

func runNativeAPNRouteRecovery() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	status, e := modem.RecoverNativeAPNManagementRoute(ctx)
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		Status string `json:"status"`
		Source string `json:"source"`
	}{status, "private-apn-management-baseline"})
}
