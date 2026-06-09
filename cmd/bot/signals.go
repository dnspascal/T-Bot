package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func setupGracefulShutdown() (context.Context, context.CancelFunc) {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	return ctx, cancel
}
