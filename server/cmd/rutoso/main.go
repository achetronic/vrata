// Rutoso is a REST API control plane for Envoy proxies.
// It manages route groups and routes, pushing configuration to Envoy via xDS
// (go-control-plane) without requiring Envoy restarts.
//
// Usage:
//
//	rutoso --config /path/to/config.yaml [--store-path /path/to/rutoso.db]
//
//	@title			Rutoso API
//	@version		1.0
//	@description	REST API control plane for Envoy proxies. Manage route groups and routes;
//	@description	changes are pushed to all connected Envoy instances via xDS in real time.
//	@contact.name	Rutoso project
//	@contact.url	https://github.com/achetronic/rutoso
//	@license.name	Apache 2.0
//	@license.url	https://www.apache.org/licenses/LICENSE-2.0
//	@host			localhost:8080
//	@BasePath		/
//	@schemes		http https
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

	"github.com/achetronic/rutoso/internal/api"
	"github.com/achetronic/rutoso/internal/config"
	"github.com/achetronic/rutoso/internal/gateway"
	boltstore "github.com/achetronic/rutoso/internal/store/bolt"
	"github.com/achetronic/rutoso/internal/xds"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "rutoso: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "config.yaml", "path to the YAML configuration file")
	storePath := flag.String("store-path", "rutoso.db", "path to the bbolt database file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	logger := buildLogger(cfg)
	logger.Info("rutoso starting",
		slog.String("http", cfg.Server.Address),
		slog.String("xds", cfg.XDS.Address),
		slog.String("store", *storePath),
	)

	// Persistent bbolt store.
	st, err := boltstore.New(*storePath)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			logger.Error("closing store", slog.String("error", err.Error()))
		}
	}()

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
