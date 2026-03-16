// Rutoso is a REST API control plane for Envoy proxies.
// It manages route groups and routes, pushing configuration to Envoy via xDS
// (go-control-plane) without requiring Envoy restarts.
//
// Usage:
//
//	rutoso --config /path/to/config.yaml
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/achetronic/rutoso/server/internal/api"
	"github.com/achetronic/rutoso/server/internal/config"
	"github.com/achetronic/rutoso/server/internal/gateway"
	"github.com/achetronic/rutoso/server/internal/store/memory"
	"github.com/achetronic/rutoso/server/internal/xds"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "rutoso: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "config.yaml", "path to the YAML configuration file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	logger := buildLogger(cfg)
	logger.Info("rutoso starting",
		slog.String("http", cfg.Server.Address),
		slog.String("xds", cfg.XDS.Address),
	)

	// In-memory store (swap for a persistent implementation later).
	st := memory.New()

	// xDS control-plane server.
	xdsSrv := xds.New(logger)

	// Gateway: bridges store events → xDS snapshot updates.
	gw := gateway.New(gateway.Dependencies{
		Store:       st,
		Cache:       xdsSrv.Cache(),
		Logger:      logger,
		NextVersion: xdsSrv.NextVersion,
	})

	// HTTP REST API.
	router := api.NewRouter(st, logger)
	httpSrv := &http.Server{
		Addr:    cfg.Server.Address,
		Handler: router,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Run all three components concurrently.
	errCh := make(chan error, 3)

	go func() {
		if err := gw.Run(ctx); err != nil {
			errCh <- fmt.Errorf("gateway: %w", err)
		}
	}()

	go func() {
		if err := xdsSrv.Serve(ctx, cfg.XDS.Address); err != nil {
			errCh <- fmt.Errorf("xds server: %w", err)
		}
	}()

	go func() {
		logger.Info("http server listening", slog.String("address", cfg.Server.Address))
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("http server: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
		_ = httpSrv.Shutdown(context.Background())
		return nil
	case err := <-errCh:
		return err
	}
}

// buildLogger creates an slog.Logger based on the log configuration.
func buildLogger(cfg *config.Config) *slog.Logger {
	var level slog.Level
	switch cfg.Log.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if cfg.Log.Format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}
