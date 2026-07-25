package slogx

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

type loggerkey struct{}

// Format is the logger's output encoding, typed so that an invalid value is
// rejected where it enters the program rather than at the first log call.
type Format string

const (
	FormatJSON Format = "json"
	FormatText Format = "text"
)

// UnmarshalText lets configuration loaders parse and validate a Format in one step.
func (f *Format) UnmarshalText(text []byte) error {
	switch format := Format(text); format {
	case FormatJSON, FormatText:
		*f = format
		return nil
	default:
		return fmt.Errorf("invalid log format: %q", format)
	}
}

// New creates a new logger with the given level and format.
func New(level slog.Level, format Format) (*slog.Logger, error) {
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	switch format {
	case FormatJSON:
		handler = slog.NewJSONHandler(os.Stdout, opts)
	case FormatText:
		handler = slog.NewTextHandler(os.Stdout, opts)
	default:
		// Format's underlying type is string, so callers bypassing UnmarshalText
		// can still hand us something invalid.
		return nil, fmt.Errorf("invalid log format: %q", format)
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
