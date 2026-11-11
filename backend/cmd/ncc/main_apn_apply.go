package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/np1-control-center/ncc/internal/broker"
)

func runAPNApplyRevision(revision int) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return broker.RunAPNApplyRevision(ctx, revision, os.Stdout)
}
