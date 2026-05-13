// Package ingest scans the parquet output directory and registers new files with
// the DuckLake catalog on a poll interval.
package ingest

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickdirienzo/waddlewatch/internal/catalog"
)

// Poller walks the configured storage path on a tick and registers new files.
type Poller struct {
	Store    *catalog.Store
	Root     string
	Interval time.Duration
	Logger   *slog.Logger
}

// Run blocks until ctx is cancelled, scanning Root on every Interval tick. The
// first scan happens immediately so a fresh install reflects existing files.
func (p *Poller) Run(ctx context.Context) error {
	if p.Store == nil {
		return errors.New("ingest: store is required")
	}
	if p.Root == "" {
		return errors.New("ingest: root path is required")
	}
	if p.Interval <= 0 {
		p.Interval = 60 * time.Second
	}
	logger := p.Logger
	if logger == nil {
		logger = slog.Default()
	}

	p.scanOnce(ctx, logger)

	t := time.NewTicker(p.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			p.scanOnce(ctx, logger)
		}
	}
}

func (p *Poller) scanOnce(ctx context.Context, logger *slog.Logger) {
	added := 0
	err := filepath.WalkDir(p.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".parquet") {
			return nil
		}
		if p.Store.Seen(path) {
			return nil
		}
		sig, ok := catalog.SignalForFile(path)
		if !ok {
			logger.Debug("skipping parquet with unknown signal", "path", path)
			return nil
		}
		if err := p.Store.AddFile(ctx, sig, path); err != nil {
			logger.Warn("registering parquet file", "path", path, "error", err)
			return nil
		}
		logger.Info("registered parquet file", "signal", sig, "path", path)
		added++
		return nil
	})
	if err != nil {
		logger.Warn("scanning parquet root", "root", p.Root, "error", err)
	}
	if added > 0 {
		logger.Info("ingest scan complete", "added", added)
	}
}
