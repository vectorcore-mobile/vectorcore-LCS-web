package main

import (
	"io"
	"log/slog"
	"os"

	"github.com/vectorcore/lcs/internal/config"
)

// buildLogger writes to cfg.File if set, and always also to stdout when
// debug is true. If no file is configured, stdout is used regardless.
func buildLogger(cfg config.LogConfig, debug bool) (*slog.Logger, func()) {
	level := slog.LevelInfo
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	var writers []io.Writer
	closeFn := func() {}

	if cfg.File != "" {
		f, err := os.OpenFile(cfg.File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			writers = append(writers, f)
			closeFn = func() { f.Close() }
		} else {
			os.Stderr.WriteString("VectorCore LCS: could not open log file, logging to stdout only: " + err.Error() + "\n")
		}
	}
	if debug || len(writers) == 0 {
		writers = append(writers, os.Stdout)
	}

	handler := slog.NewTextHandler(io.MultiWriter(writers...), &slog.HandlerOptions{Level: level})
	return slog.New(handler), closeFn
}
