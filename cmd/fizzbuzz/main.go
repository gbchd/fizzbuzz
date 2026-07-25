package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/gbchd/fizzbuzz/internal/config"
	"github.com/gbchd/fizzbuzz/internal/pkg/slogx"
)

func run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger, err := slogx.New(cfg.Log.Level, cfg.Log.Format)
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	ctx = slogx.WithCtx(ctx, logger)

	// Catches stdlib and third-party output only, our own code must always go through slogx.FromCtx.
	slog.SetDefault(logger)

	logger.InfoContext(ctx, "starting fizzbuzz")

	return nil
}

func main() {
	ctx := context.Background()
	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		os.Exit(1)
	}
}
