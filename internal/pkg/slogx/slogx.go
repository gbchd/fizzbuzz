package slogx

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

type loggerkey struct{}

// New creates a new logger with the given level and format.
//
// Supported formats: "json" or "text"
func New(level slog.Level, format string) (*slog.Logger, error) {
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	switch format {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, opts)
	case "text":
		handler = slog.NewTextHandler(os.Stdout, opts)
	default:
		return nil, fmt.Errorf("invalid log format: %s", format)
	}

	return slog.New(handler), nil
}

// FromCtx returns the logger from the context, or a nop logger if none is found (useful for tests).
func FromCtx(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerkey{}).(*slog.Logger); ok {
		return logger
	}

	return slog.New(slog.DiscardHandler)
}

// WithCtx returns a new context with the given logger.
func WithCtx(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerkey{}, logger)
}
