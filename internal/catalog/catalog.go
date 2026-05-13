// Package catalog wraps DuckLake operations: attach, schema setup, and idempotent
// data-file registration.
package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	_ "github.com/marcboeker/go-duckdb/v2"
)

// Signal identifies an OTel signal type backed by one DuckLake table.
type Signal string

const (
	SignalLogs    Signal = "logs"
	SignalMetrics Signal = "metrics"
	SignalTraces  Signal = "traces"
)

// AllSignals returns the signals Waddlewatch knows about.
func AllSignals() []Signal { return []Signal{SignalLogs, SignalMetrics, SignalTraces} }

// Table returns the qualified DuckLake table name for a signal.
func (s Signal) Table() string {
	return "otel_" + string(s)
}

// Store manages the DuckDB connection attached to a DuckLake catalog.
type Store struct {
	db          *sql.DB
	catalogName string

	mu   sync.Mutex
	seen map[string]struct{}
}

// Options configures Store creation.
type Options struct {
	CatalogPath string
	DataPath    string
}

// Open attaches the DuckLake catalog (creating it if missing) and ensures the
// schema for OTel signals exists.
func Open(ctx context.Context, opts Options) (*Store, error) {
	if opts.CatalogPath == "" {
		return nil, errors.New("catalog path is required")
	}
	if opts.DataPath == "" {
		opts.DataPath = filepath.Join(filepath.Dir(opts.CatalogPath), "data")
	}

	db, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, fmt.Errorf("open duckdb: %w", err)
	}
	db.SetMaxOpenConns(1) // DuckDB serialises writes; keep things simple.

	bootstrap := []string{
		"INSTALL ducklake",
		"LOAD ducklake",
		"INSTALL sqlite",
		"LOAD sqlite",
		fmt.Sprintf("ATTACH 'ducklake:sqlite:%s' AS waddlewatch (DATA_PATH '%s')",
			escape(opts.CatalogPath), escape(opts.DataPath)),
		"USE waddlewatch",
	}
	for _, stmt := range bootstrap {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("bootstrap %q: %w", stmt, err)
		}
	}

	s := &Store{db: db, catalogName: "waddlewatch", seen: map[string]struct{}{}}
	if err := s.ensureSchema(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.loadSeen(ctx); err != nil {
		slog.Warn("loading seen files", "error", err)
	}
	return s, nil
}

// DB returns the underlying connection for query helpers.
func (s *Store) DB() *sql.DB { return s.db }

// Close releases resources.
func (s *Store) Close() error { return s.db.Close() }

// CatalogName returns the attached DuckLake catalog name.
func (s *Store) CatalogName() string { return s.catalogName }

// Seen reports whether a file path has already been registered with the catalog.
func (s *Store) Seen(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.seen[path]
	return ok
}

// AddFile registers a parquet file with the table for a signal. It is safe to call
// repeatedly for the same path.
func (s *Store) AddFile(ctx context.Context, sig Signal, path string) error {
	s.mu.Lock()
	if _, ok := s.seen[path]; ok {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	stmt := fmt.Sprintf("CALL ducklake_add_data_files('%s', '%s', '%s')",
		escape(s.catalogName), escape(sig.Table()), escape(path))
	if _, err := s.db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("add file %s to %s: %w", path, sig.Table(), err)
	}

	s.mu.Lock()
	s.seen[path] = struct{}{}
	s.mu.Unlock()
	return nil
}

// SignalForFile picks a signal based on the directory the file lives in. Vector
// writes per-signal subdirectories (logs/metrics/traces) which we honour here.
func SignalForFile(path string) (Signal, bool) {
	lower := strings.ToLower(filepath.ToSlash(path))
	switch {
	case strings.Contains(lower, "/logs/") || strings.HasSuffix(filepath.Dir(lower), "/logs"):
		return SignalLogs, true
	case strings.Contains(lower, "/metrics/") || strings.HasSuffix(filepath.Dir(lower), "/metrics"):
		return SignalMetrics, true
	case strings.Contains(lower, "/traces/") || strings.HasSuffix(filepath.Dir(lower), "/traces"):
		return SignalTraces, true
	}
	return "", false
}

func (s *Store) ensureSchema(ctx context.Context) error {
	for _, stmt := range schemaDDL() {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("ensure schema: %w", err)
		}
	}
	return nil
}

// loadSeen pulls the set of already-registered data files from the catalog so
// restarts don't re-register them. Queried per signal table because
// ducklake_list_files is table-scoped.
func (s *Store) loadSeen(ctx context.Context) error {
	for _, sig := range AllSignals() {
		q := fmt.Sprintf("SELECT data_file FROM ducklake_list_files('%s', '%s')",
			escape(s.catalogName), escape(sig.Table()))
		rows, err := s.db.QueryContext(ctx, q)
		if err != nil {
			return fmt.Errorf("listing %s files: %w", sig.Table(), err)
		}
		for rows.Next() {
			var p string
			if err := rows.Scan(&p); err != nil {
				rows.Close()
				return err
			}
			s.seen[p] = struct{}{}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}
	return nil
}

func schemaDDL() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS otel_logs (
			timestamp TIMESTAMP,
			observed_timestamp TIMESTAMP,
			trace_id VARCHAR,
			span_id VARCHAR,
			severity_text VARCHAR,
			severity_number INTEGER,
			service_name VARCHAR,
			body VARCHAR,
			attributes JSON,
			resource_attributes JSON,
			scope_name VARCHAR
		)`,
		`CREATE TABLE IF NOT EXISTS otel_metrics (
			timestamp TIMESTAMP,
			service_name VARCHAR,
			metric_name VARCHAR,
			metric_kind VARCHAR,
			value DOUBLE,
			attributes JSON,
			resource_attributes JSON,
			scope_name VARCHAR
		)`,
		`CREATE TABLE IF NOT EXISTS otel_traces (
			start_time TIMESTAMP,
			end_time TIMESTAMP,
			trace_id VARCHAR,
			span_id VARCHAR,
			parent_span_id VARCHAR,
			service_name VARCHAR,
			span_name VARCHAR,
			span_kind VARCHAR,
			status_code VARCHAR,
			status_message VARCHAR,
			duration_ns BIGINT,
			attributes JSON,
			resource_attributes JSON,
			scope_name VARCHAR
		)`,
	}
}

// escape doubles single quotes so we can safely inject identifiers into DuckDB
// statements that don't support parameters (DDL, CALL).
func escape(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
