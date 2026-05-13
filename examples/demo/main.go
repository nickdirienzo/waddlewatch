// Command demo writes synthetic OTel-shaped parquet files into a target
// directory layout so Waddlewatch can pick them up without a real OTLP
// collector in the loop. Useful for kicking the tyres end to end.
//
//	go run ./examples/demo -out /tmp/wdw-demo/parquet -interval 5s
//
// The generated schema matches the catalog tables defined in
// internal/catalog/catalog.go. Run alongside `waddlewatch` pointed at the same
// storage_path.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "github.com/marcboeker/go-duckdb/v2"
)

func main() {
	out := flag.String("out", "/tmp/wdw-demo/parquet", "parquet output root (logs/, metrics/, traces/ are created underneath)")
	interval := flag.Duration("interval", 5*time.Second, "how often to write a new batch")
	batch := flag.Int("batch", 50, "rows per batch per signal")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	for _, sub := range []string{"logs", "metrics", "traces"} {
		if err := os.MkdirAll(filepath.Join(*out, sub), 0o755); err != nil {
			logger.Error("mkdir", "path", filepath.Join(*out, sub), "error", err)
			os.Exit(1)
		}
	}

	db, err := sql.Open("duckdb", "")
	if err != nil {
		logger.Error("open duckdb", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	if _, err := db.Exec("INSTALL json; LOAD json;"); err != nil {
		logger.Error("load json extension", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := writeBatch(ctx, db, *out, *batch, logger); err != nil {
		logger.Warn("initial batch", "error", err)
	}

	t := time.NewTicker(*interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down")
			return
		case <-t.C:
			if err := writeBatch(ctx, db, *out, *batch, logger); err != nil {
				logger.Warn("batch failed", "error", err)
			}
		}
	}
}

var services = []string{"web", "checkout", "payments", "auth"}
var severities = []struct {
	text string
	num  int
}{
	{"DEBUG", 5},
	{"INFO", 9},
	{"INFO", 9},
	{"INFO", 9},
	{"WARN", 13},
	{"ERROR", 17},
}
var spanNames = []string{"GET /", "POST /checkout", "db.query", "http.client", "kafka.publish"}
var metricNames = []string{"http.requests", "http.latency_ms", "cpu.percent", "queue.depth"}

func writeBatch(ctx context.Context, db *sql.DB, root string, batch int, logger *slog.Logger) error {
	now := time.Now().UTC()
	stamp := now.Format("20060102T150405.000")

	if err := writeLogs(ctx, db, filepath.Join(root, "logs", "logs-"+stamp+".parquet"), now, batch); err != nil {
		return fmt.Errorf("logs: %w", err)
	}
	if err := writeMetrics(ctx, db, filepath.Join(root, "metrics", "metrics-"+stamp+".parquet"), now, batch); err != nil {
		return fmt.Errorf("metrics: %w", err)
	}
	if err := writeTraces(ctx, db, filepath.Join(root, "traces", "traces-"+stamp+".parquet"), now, batch); err != nil {
		return fmt.Errorf("traces: %w", err)
	}
	logger.Info("wrote batch", "rows_per_signal", batch, "stamp", stamp)
	return nil
}

func writeLogs(ctx context.Context, db *sql.DB, path string, now time.Time, n int) error {
	rows := make([]string, 0, n)
	for i := 0; i < n; i++ {
		sev := severities[rand.IntN(len(severities))]
		svc := services[rand.IntN(len(services))]
		ts := now.Add(-time.Duration(rand.IntN(60)) * time.Second).Format(time.RFC3339Nano)
		body := sqlQuote(fmt.Sprintf("%s handled request %d", svc, rand.IntN(100000)))
		rows = append(rows, logRowSelect(i == 0, ts, randID(16), randID(8), sev.text, sev.num, svc, body))
	}
	return runCopy(ctx, db, path, strings.Join(rows, " UNION ALL "))
}

func logRowSelect(withAliases bool, ts, traceID, spanID, sevText string, sevNum int, svc, body string) string {
	if withAliases {
		return fmt.Sprintf(`SELECT TIMESTAMP '%s' AS timestamp, TIMESTAMP '%s' AS observed_timestamp,
			'%s' AS trace_id, '%s' AS span_id, '%s' AS severity_text, %d AS severity_number,
			'%s' AS service_name, %s AS body,
			CAST('{"region":"us-east-1"}' AS JSON) AS attributes,
			CAST('{"host":"demo-host"}' AS JSON) AS resource_attributes,
			'demo' AS scope_name`,
			ts, ts, traceID, spanID, sevText, sevNum, svc, body)
	}
	return fmt.Sprintf(`SELECT TIMESTAMP '%s', TIMESTAMP '%s', '%s', '%s', '%s', %d, '%s', %s,
			CAST('{"region":"us-east-1"}' AS JSON),
			CAST('{"host":"demo-host"}' AS JSON),
			'demo'`,
		ts, ts, traceID, spanID, sevText, sevNum, svc, body)
}

func writeMetrics(ctx context.Context, db *sql.DB, path string, now time.Time, n int) error {
	kinds := []string{"gauge", "sum"}
	rows := make([]string, 0, n)
	for i := 0; i < n; i++ {
		svc := services[rand.IntN(len(services))]
		name := metricNames[rand.IntN(len(metricNames))]
		kind := kinds[rand.IntN(len(kinds))]
		value := rand.Float64() * 100
		ts := now.Add(-time.Duration(rand.IntN(60)) * time.Second).Format(time.RFC3339Nano)
		rows = append(rows, metricRowSelect(i == 0, ts, svc, name, kind, value))
	}
	return runCopy(ctx, db, path, strings.Join(rows, " UNION ALL "))
}

func metricRowSelect(withAliases bool, ts, svc, name, kind string, value float64) string {
	if withAliases {
		return fmt.Sprintf(`SELECT TIMESTAMP '%s' AS timestamp, '%s' AS service_name,
			'%s' AS metric_name, '%s' AS metric_kind, CAST(%f AS DOUBLE) AS value,
			CAST('{}' AS JSON) AS attributes,
			CAST('{"host":"demo-host"}' AS JSON) AS resource_attributes,
			'demo' AS scope_name`,
			ts, svc, name, kind, value)
	}
	return fmt.Sprintf(`SELECT TIMESTAMP '%s', '%s', '%s', '%s', CAST(%f AS DOUBLE),
			CAST('{}' AS JSON),
			CAST('{"host":"demo-host"}' AS JSON),
			'demo'`,
		ts, svc, name, kind, value)
}

func writeTraces(ctx context.Context, db *sql.DB, path string, now time.Time, n int) error {
	rows := make([]string, 0, n)
	for i := 0; i < n; i++ {
		svc := services[rand.IntN(len(services))]
		name := spanNames[rand.IntN(len(spanNames))]
		startOffset := time.Duration(rand.IntN(60)) * time.Second
		durNs := int64(rand.IntN(500_000_000) + 1_000_000) // 1ms..500ms
		start := now.Add(-startOffset)
		end := start.Add(time.Duration(durNs))
		status := "OK"
		if rand.IntN(20) == 0 {
			status = "ERROR"
		}
		rows = append(rows, traceRowSelect(i == 0,
			start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano),
			randID(16), randID(8), randID(8),
			svc, name, status, durNs))
	}
	return runCopy(ctx, db, path, strings.Join(rows, " UNION ALL "))
}

func traceRowSelect(withAliases bool, start, end, traceID, spanID, parentID, svc, name, status string, durNs int64) string {
	if withAliases {
		return fmt.Sprintf(`SELECT TIMESTAMP '%s' AS start_time, TIMESTAMP '%s' AS end_time,
			'%s' AS trace_id, '%s' AS span_id, '%s' AS parent_span_id,
			'%s' AS service_name, '%s' AS span_name, 'SERVER' AS span_kind,
			'%s' AS status_code, '' AS status_message,
			CAST(%d AS BIGINT) AS duration_ns,
			CAST('{}' AS JSON) AS attributes,
			CAST('{"host":"demo-host"}' AS JSON) AS resource_attributes,
			'demo' AS scope_name`,
			start, end, traceID, spanID, parentID, svc, name, status, durNs)
	}
	return fmt.Sprintf(`SELECT TIMESTAMP '%s', TIMESTAMP '%s', '%s', '%s', '%s',
			'%s', '%s', 'SERVER', '%s', '',
			CAST(%d AS BIGINT),
			CAST('{}' AS JSON),
			CAST('{"host":"demo-host"}' AS JSON),
			'demo'`,
		start, end, traceID, spanID, parentID, svc, name, status, durNs)
}

func runCopy(ctx context.Context, db *sql.DB, path, selectStmt string) error {
	stmt := fmt.Sprintf("COPY (%s) TO '%s' (FORMAT PARQUET)",
		selectStmt, strings.ReplaceAll(path, "'", "''"))
	_, err := db.ExecContext(ctx, stmt)
	return err
}

const hex = "0123456789abcdef"

func randID(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = hex[rand.IntN(16)]
	}
	return string(b)
}

func sqlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
