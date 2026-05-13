// Package query runs read-only DuckDB queries against the DuckLake catalog.
package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Range describes a half-open [Start, End) time window.
type Range struct {
	Start time.Time
	End   time.Time
}

// ParseRange parses optional "from" and "to" RFC3339 strings, falling back to
// the last hour ending now.
func ParseRange(from, to string) (Range, error) {
	end := time.Now().UTC()
	start := end.Add(-1 * time.Hour)
	if to != "" {
		t, err := time.Parse(time.RFC3339, to)
		if err != nil {
			return Range{}, fmt.Errorf("parsing 'to' (%s): %w", to, err)
		}
		end = t.UTC()
	}
	if from != "" {
		t, err := time.Parse(time.RFC3339, from)
		if err != nil {
			return Range{}, fmt.Errorf("parsing 'from' (%s): %w", from, err)
		}
		start = t.UTC()
	}
	if !end.After(start) {
		return Range{}, fmt.Errorf("'to' must be after 'from'")
	}
	return Range{Start: start, End: end}, nil
}

// LogRow is one log row returned to the UI.
type LogRow struct {
	Timestamp time.Time
	Service   string
	Severity  string
	Body      string
	TraceID   string
}

// LogsQuery wraps the filter options for /logs.
type LogsQuery struct {
	Range    Range
	Service  string
	Severity string
	Search   string
	Limit    int
}

// Logs returns matching log rows ordered newest-first.
func Logs(ctx context.Context, db *sql.DB, q LogsQuery) ([]LogRow, error) {
	if q.Limit <= 0 || q.Limit > 1000 {
		q.Limit = 200
	}
	where := []string{"timestamp >= ? AND timestamp < ?"}
	args := []any{q.Range.Start, q.Range.End}
	if q.Service != "" {
		where = append(where, "service_name = ?")
		args = append(args, q.Service)
	}
	if q.Severity != "" {
		where = append(where, "severity_text = ?")
		args = append(args, q.Severity)
	}
	if q.Search != "" {
		where = append(where, "body ILIKE ?")
		args = append(args, "%"+q.Search+"%")
	}
	sqlStmt := fmt.Sprintf(`
		SELECT timestamp, COALESCE(service_name,'') , COALESCE(severity_text,''), COALESCE(body,''), COALESCE(trace_id,'')
		FROM otel_logs
		WHERE %s
		ORDER BY timestamp DESC
		LIMIT %d`, strings.Join(where, " AND "), q.Limit)

	rows, err := db.QueryContext(ctx, sqlStmt, args...)
	if err != nil {
		return nil, fmt.Errorf("querying logs: %w", err)
	}
	defer rows.Close()

	var out []LogRow
	for rows.Next() {
		var r LogRow
		if err := rows.Scan(&r.Timestamp, &r.Service, &r.Severity, &r.Body, &r.TraceID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MetricRow is one bucketed metric point.
type MetricRow struct {
	Bucket  time.Time
	Service string
	Name    string
	Value   float64
}

// SeriesKey identifies one metric series.
type SeriesKey struct {
	Service string
	Name    string
}

// MetricsQuery filters /metrics. When Series is empty, Metrics returns no rows.
type MetricsQuery struct {
	Range  Range
	Series []SeriesKey
	Bucket time.Duration
}

// Metrics returns metric points bucketed by Bucket (defaults to 1 minute) for
// the requested series. Empty Series returns no rows — the explorer view
// requires the user to pick what to plot.
func Metrics(ctx context.Context, db *sql.DB, q MetricsQuery) ([]MetricRow, error) {
	if len(q.Series) == 0 {
		return nil, nil
	}
	if q.Bucket <= 0 {
		q.Bucket = time.Minute
	}
	bucketSecs := int64(q.Bucket / time.Second)
	if bucketSecs <= 0 {
		bucketSecs = 60
	}

	conds := make([]string, 0, len(q.Series))
	args := []any{q.Range.Start, q.Range.End}
	for _, s := range q.Series {
		conds = append(conds, "(service_name = ? AND metric_name = ?)")
		args = append(args, s.Service, s.Name)
	}

	sqlStmt := fmt.Sprintf(`
		SELECT time_bucket(INTERVAL %d SECOND, timestamp) AS bucket,
		       COALESCE(service_name,''),
		       COALESCE(metric_name,''),
		       AVG(value) AS value
		FROM otel_metrics
		WHERE timestamp >= ? AND timestamp < ? AND (%s)
		GROUP BY bucket, service_name, metric_name
		ORDER BY bucket ASC
		LIMIT 5000`, bucketSecs, strings.Join(conds, " OR "))

	rows, err := db.QueryContext(ctx, sqlStmt, args...)
	if err != nil {
		return nil, fmt.Errorf("querying metrics: %w", err)
	}
	defer rows.Close()
	var out []MetricRow
	for rows.Next() {
		var r MetricRow
		if err := rows.Scan(&r.Bucket, &r.Service, &r.Name, &r.Value); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MetricSeriesRow describes one available (service, metric_name) pair in a
// time window, with how many raw points fell in that window.
type MetricSeriesRow struct {
	Service string
	Name    string
	Count   int64
}

// MetricSeriesList returns the available series in the time range, sorted by
// row count desc so the noisiest metrics surface first.
func MetricSeriesList(ctx context.Context, db *sql.DB, rng Range) ([]MetricSeriesRow, error) {
	sqlStmt := `
		SELECT COALESCE(service_name,'') AS svc,
		       COALESCE(metric_name,'') AS name,
		       COUNT(*) AS c
		FROM otel_metrics
		WHERE timestamp >= ? AND timestamp < ?
		GROUP BY svc, name
		ORDER BY c DESC, svc, name
		LIMIT 500`
	rows, err := db.QueryContext(ctx, sqlStmt, rng.Start, rng.End)
	if err != nil {
		return nil, fmt.Errorf("listing metric series: %w", err)
	}
	defer rows.Close()
	var out []MetricSeriesRow
	for rows.Next() {
		var r MetricSeriesRow
		if err := rows.Scan(&r.Service, &r.Name, &r.Count); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// TraceRow is one span returned to the UI.
type TraceRow struct {
	StartTime  time.Time
	DurationMs float64
	Service    string
	SpanName   string
	TraceID    string
	SpanID     string
	StatusCode string
}

// TracesQuery filters /traces.
type TracesQuery struct {
	Range   Range
	Service string
	TraceID string
	Limit   int
}

// Traces returns spans ordered by start time descending.
func Traces(ctx context.Context, db *sql.DB, q TracesQuery) ([]TraceRow, error) {
	if q.Limit <= 0 || q.Limit > 1000 {
		q.Limit = 200
	}
	where := []string{"start_time >= ? AND start_time < ?"}
	args := []any{q.Range.Start, q.Range.End}
	if q.Service != "" {
		where = append(where, "service_name = ?")
		args = append(args, q.Service)
	}
	if q.TraceID != "" {
		where = append(where, "trace_id = ?")
		args = append(args, q.TraceID)
	}
	sqlStmt := fmt.Sprintf(`
		SELECT start_time,
		       duration_ns / 1e6 AS duration_ms,
		       COALESCE(service_name,''),
		       COALESCE(span_name,''),
		       COALESCE(trace_id,''),
		       COALESCE(span_id,''),
		       COALESCE(status_code,'')
		FROM otel_traces
		WHERE %s
		ORDER BY start_time DESC
		LIMIT %d`, strings.Join(where, " AND "), q.Limit)

	rows, err := db.QueryContext(ctx, sqlStmt, args...)
	if err != nil {
		return nil, fmt.Errorf("querying traces: %w", err)
	}
	defer rows.Close()
	var out []TraceRow
	for rows.Next() {
		var r TraceRow
		if err := rows.Scan(&r.StartTime, &r.DurationMs, &r.Service, &r.SpanName, &r.TraceID, &r.SpanID, &r.StatusCode); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// TraceSpan is one span returned for the detail view.
type TraceSpan struct {
	StartTime    time.Time
	EndTime      time.Time
	SpanID       string
	ParentSpanID string
	Service      string
	SpanName     string
	SpanKind     string
	StatusCode   string
	DurationMs   float64
}

// TraceSpans returns every span belonging to a trace ordered by start time.
func TraceSpans(ctx context.Context, db *sql.DB, traceID string) ([]TraceSpan, error) {
	if traceID == "" {
		return nil, nil
	}
	sqlStmt := `
		SELECT start_time, end_time, COALESCE(span_id,''), COALESCE(parent_span_id,''),
		       COALESCE(service_name,''), COALESCE(span_name,''),
		       COALESCE(span_kind,''), COALESCE(status_code,''),
		       duration_ns / 1e6 AS duration_ms
		FROM otel_traces
		WHERE trace_id = ?
		ORDER BY start_time ASC
		LIMIT 5000`
	rows, err := db.QueryContext(ctx, sqlStmt, traceID)
	if err != nil {
		return nil, fmt.Errorf("loading trace %s: %w", traceID, err)
	}
	defer rows.Close()
	var out []TraceSpan
	for rows.Next() {
		var s TraceSpan
		if err := rows.Scan(&s.StartTime, &s.EndTime, &s.SpanID, &s.ParentSpanID,
			&s.Service, &s.SpanName, &s.SpanKind, &s.StatusCode, &s.DurationMs); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Services returns a deduped list of service names seen for the given signal table.
func Services(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	sqlStmt := fmt.Sprintf(`SELECT DISTINCT service_name FROM %s WHERE service_name IS NOT NULL ORDER BY service_name`, table)
	rows, err := db.QueryContext(ctx, sqlStmt)
	if err != nil {
		return nil, fmt.Errorf("listing services from %s: %w", table, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		if s != "" {
			out = append(out, s)
		}
	}
	return out, rows.Err()
}
