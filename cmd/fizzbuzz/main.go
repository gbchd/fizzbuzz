package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gbchd/fizzbuzz/internal/config"
	"github.com/gbchd/fizzbuzz/internal/handler"
	"github.com/gbchd/fizzbuzz/internal/pkg/slogx"
	"github.com/gbchd/fizzbuzz/internal/repository/stats/statsmemory"
	"github.com/gbchd/fizzbuzz/internal/service/fizzbuzz"
	"github.com/gbchd/fizzbuzz/internal/service/statistics"
)

// Bound how long one connection can hold resources. Constants, not config:
// no deployment target makes a different answer right.
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 120 * time.Second
)

// Shutdown timing, from https://victoriametrics.com/blog/go-graceful-shutdown/
const (
	_shutdownPeriod      = 15 * time.Second
	_shutdownHardPeriod  = 3 * time.Second
	_readinessDrainDelay = 5 * time.Second
)

func setupGlobalLogger(logger *slog.Logger) {
	slog.SetDefault(logger)
	slog.SetLogLoggerLevel(slog.LevelError)
}

func run(ctx context.Context) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger, err := slogx.New(cfg.Log.Level, cfg.Log.Format)
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	ctx = slogx.WithCtx(ctx, logger)
	ctxlog := context.WithoutCancel(ctx)

	setupGlobalLogger(logger)

	var shuttingDown atomic.Bool
	isReady := func() bool { return !shuttingDown.Load() }

	svc := statistics.NewRecorder(fizzbuzz.NewService(), statsmemory.New())

	rateLimit := handler.RateLimit{
		Requests: cfg.HTTP.RateLimitRequests,
		Window:   cfg.HTTP.RateLimitWindow,
	}

	h, err := handler.New(svc, svc, isReady, rateLimit)
	if err != nil {
		return fmt.Errorf("init handler: %w", err)
	}

	requestCtx, cancelRequests := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRequests()

	server := &http.Server{
		Addr:              cfg.HTTP.Address,
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		BaseContext:       func(net.Listener) context.Context { return requestCtx },
	}

	// Buffered, so the goroutine never blocks if nobody is left to receive.
	serveErr := make(chan error, 1)
	go func() {
		slogx.FromCtx(ctxlog).InfoContext(ctxlog, "starting server", "address", cfg.HTTP.Address)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- fmt.Errorf("serve: %w", err)
		}
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	stop()
	shuttingDown.Store(true)
	slogx.FromCtx(ctxlog).InfoContext(ctxlog, "received shutdown signal, shutting down")

	time.Sleep(_readinessDrainDelay)
	slogx.FromCtx(ctxlog).InfoContext(ctxlog, "readiness check propagated, now waiting for ongoing requests to finish")

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), _shutdownPeriod)
	defer cancel()

	err = server.Shutdown(shutdownCtx)
	cancelRequests()
	if err != nil {
		slogx.FromCtx(ctxlog).ErrorContext(ctxlog, "graceful shutdown timed out, forcing close", "error", err)
		_ = server.Close()
		return fmt.Errorf("shutdown: %w", err)
	}

	slogx.FromCtx(ctxlog).InfoContext(ctxlog, "server shut down gracefully")
	return nil
}

func main() {
	ctx := context.Background()

	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		os.Exit(1)
	}
}
