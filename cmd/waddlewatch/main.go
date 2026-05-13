// Command waddlewatch ingests OTel parquet files into a DuckLake catalog and
// serves an htmx-driven UI for querying.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/nickdirienzo/waddlewatch/internal/catalog"
	"github.com/nickdirienzo/waddlewatch/internal/config"
	"github.com/nickdirienzo/waddlewatch/internal/ingest"
	"github.com/nickdirienzo/waddlewatch/internal/server"
	"github.com/nickdirienzo/waddlewatch/web"
)

func main() {
	configPath := flag.String("config", "", "path to YAML config file (optional)")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(*configPath, logger); err != nil {
		logger.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(configPath string, logger *slog.Logger) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	logger.Info("configuration loaded",
		"listen", cfg.Listen,
		"storage_path", cfg.StoragePath,
		"catalog_path", cfg.CatalogPath,
		"poll_interval", cfg.PollInterval,
		"s3", cfg.UsingS3(),
	)

	if err := ensureDirs(cfg); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	store, err := catalog.Open(ctx, catalog.Options{
		CatalogPath: cfg.CatalogPath,
		DataPath:    cfg.DataPath,
	})
	if err != nil {
		return fmt.Errorf("opening catalog: %w", err)
	}
	defer store.Close()

	srv, err := server.New(server.Deps{
		Store:       store,
		Templates:   web.Templates(),
		Static:      web.Static(),
		Logger:      logger,
		StoragePath: cfg.StoragePath,
		CatalogPath: cfg.CatalogPath,
	})
	if err != nil {
		return fmt.Errorf("building server: %w", err)
	}

	poller := &ingest.Poller{
		Store:    store,
		Root:     cfg.StoragePath,
		Interval: cfg.PollInterval,
		Logger:   logger.With("component", "ingest"),
	}

	errCh := make(chan error, 2)
	go func() { errCh <- poller.Run(ctx) }()
	go func() { errCh <- srv.Start(ctx, cfg.Listen) }()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil {
			cancel()
			return err
		}
	}

	// Drain remaining goroutine error after cancellation.
	if err := <-errCh; err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	logger.Info("shutdown complete")
	return nil
}

func ensureDirs(cfg config.Config) error {
	dirs := []string{cfg.DataPath}
	if !cfg.UsingS3() {
		dirs = append(dirs, cfg.StoragePath)
	}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", d, err)
		}
	}
	// Catalog file directory must exist before DuckLake opens the DuckDB file.
	if cfg.CatalogPath != "" {
		if err := os.MkdirAll(parentDir(cfg.CatalogPath), 0o755); err != nil {
			return fmt.Errorf("creating catalog dir: %w", err)
		}
	}
	return nil
}

func parentDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == os.PathSeparator {
			return p[:i]
		}
	}
	return "."
}
