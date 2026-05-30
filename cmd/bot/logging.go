package main

import (
	"log/slog"
	"os"
)

func setupLogging() {
	options := &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug,
	}
	handler := slog.NewJSONHandler(os.Stdout, options)
	logger := slog.New(handler)
	slog.SetDefault(logger)
}
