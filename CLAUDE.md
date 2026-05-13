# Waddlewatch — Claude Code Guide

## What this project is

A self-hosted observability tool. Ingests OpenTelemetry data via Vector. Stores it as Parquet files registered in a DuckLake catalog. Serves an htmx-based web UI for querying. Single Go binary. Single config file.

## Project goals (in order)

1. Ship a working MVP that ingests OTel logs/metrics/traces and lets a human query them via a web UI
2. Be deployable as a single binary plus a config file
3. Be obvious to operate. Files on disk, no clustered services, no separate database
4. Handle a year of homelab-scale telemetry without choking on memory

## Non-goals (for v1)

- Multi-tenancy or authentication. Bind to localhost or a trusted network.
- High availability or horizontal scaling
- Pretty charts and dashboards beyond the basics
- Alerting and paging
- Trace waterfall view. A flat span table sorted by start time is fine for now.
- Cross-customer analytics or data export pipelines

## Architecture

Three components:

1. **Vector** (external). Receives OTLP from apps. Writes Parquet files per signal type to a configured local directory or S3 bucket. Not part of this codebase. Example config at `examples/vector.yaml`.

2. **Waddlewatch binary** (this codebase). Two responsibilities:
   - Background goroutine: polls the parquet output directory on a timer (default 60s). For any new files, calls `ducklake_add_data_files(catalog, table, file_path)` to register them.
   - HTTP server: serves an htmx-driven UI. Executes queries against DuckDB attached to the DuckLake catalog. Returns HTML fragments.

3. **Storage**. Parquet files on local disk or S3. DuckLake catalog as a SQLite file alongside the Parquet.

## Tech stack and why

- **Go 1.23+**. Single binary deployment, simple ops, fits boring-infra-tool aesthetic.
- **DuckDB via go-duckdb** (`github.com/marcboeker/go-duckdb`). Query engine, embedded in the binary, no separate service to run.
- **DuckLake**. Catalog format that tracks parquet files. SQLite-backed catalog for v1 (zero ops).
- **htmx + html/template**. Server-rendered HTML. No JS toolchain. No build step. Templates in `web/templates/`.
- **chi router** (`github.com/go-chi/chi/v5`). Lightweight HTTP routing.
- **YAML config** (`gopkg.in/yaml.v3`). Readable config with env var overrides.
- **log/slog** (stdlib). Structured logging. No `fmt.Println` in production paths.
- **Chart.js via CDN** for the small number of charts. Don't bundle.

## File layout

```
waddlewatch/
├── README.md
├── CLAUDE.md
├── LICENSE
├── go.mod
├── go.sum
├── cmd/waddlewatch/
│   └── main.go              # entry point, flag parsing, lifecycle
├── internal/
│   ├── config/              # YAML config loading, env overrides
│   ├── catalog/             # DuckLake attach, add_data_files
│   ├── ingest/              # file polling loop, dedupe of seen files
│   ├── query/               # DuckDB query helpers (time filters, etc.)
│   └── server/              # HTTP routes, handlers, middleware
├── web/
│   ├── templates/           # html/template files (embedded)
│   └── static/              # CSS, htmx.min.js, chart loader (embedded)
├── examples/
│   ├── config.yaml          # example app config
│   └── vector.yaml          # example Vector config
├── deploy/
│   ├── Dockerfile
│   └── waddlewatch.service  # systemd unit
└── docs/
    ├── architecture.md
    └── setup.md
```

Embed `web/templates/` and `web/static/` via `go:embed` so the binary is self-contained.

## Conventions

- Short, direct functions. Avoid interfaces unless there's a real second implementation.
- Wrap errors with `fmt.Errorf("doing X: %w", err)`. No custom error types unless needed.
- Use `slog` with structured fields. No bare `fmt.Println` or `log.Print` in production paths.
- Tests live next to the code (`foo_test.go`). Use `testing` plus `testify/require` for assertions.
- Configuration: YAML primary. Env vars override individual fields for ops convenience. Sensitive values via env only.
- No dependency injection frameworks. Pass deps as struct fields or function args.
- HTTP handlers return errors. Middleware handles them. Don't write to ResponseWriter and return error.
- No em dashes in user-facing strings or documentation. Use periods, commas, or parens.

## What to build first (v1 scope)

1. Config schema and loader in `internal/config/`. Fields: storage path or S3 bucket, catalog path, HTTP listen addr, poll interval.
2. Catalog operations in `internal/catalog/`. Attach DuckLake. Idempotent `ducklake_add_data_files`. Track seen-files state (in a tiny SQLite table or in-memory map with persistent checkpoint).
3. Ingest loop in `internal/ingest/`. Polls storage path on a ticker. New files get registered via catalog package.
4. HTTP server in `internal/server/` with these routes:
   - `GET /` landing page, links to logs/metrics/traces
   - `GET /logs` htmx-driven log view with time range and service filter
   - `GET /metrics` htmx-driven metrics view
   - `GET /traces` htmx-driven trace view (flat span table)
   - `GET /tail` SSE endpoint for live log tail
   - `GET /healthz` health check
5. Templates in `web/templates/` matching each view. Use partials for the htmx-swapped fragments.
6. `go:embed` directives for templates and static assets.
7. Dockerfile and systemd unit in `deploy/`.

## What NOT to do in v1

- Don't add authentication. Bind to localhost or trusted networks only.
- Don't write a custom chart library. Use Chart.js via CDN.
- Don't add Postgres support for the catalog. SQLite only.
- Don't build a trace waterfall. Flat table sorted by `start_time` is fine.
- Don't add alerting, notification, or paging.
- Don't add an ORM or query builder. Raw SQL via DuckDB.
- Don't add a frontend framework. htmx and html/template only.
- Don't add a CLI subcommand structure beyond what's needed. A single binary with flags is fine.
- Don't gold-plate the file watcher. Polling on a timer is fine. fsnotify is overkill for v1.

## Common commands

```bash
# Build
go build -o waddlewatch ./cmd/waddlewatch

# Run
./waddlewatch --config examples/config.yaml

# Test
go test ./...

# Format and lint
go fmt ./...
go vet ./...
```

## OTel schema reference

For Parquet table shapes, follow the [duckdb-otlp](https://github.com/smithclay/duckdb-otlp) extension. It defines ClickHouse-compatible OTel schemas for logs, metrics (gauge, sum, histogram, exp_histogram), and traces. Matching those shapes keeps Waddlewatch interoperable with the wider ecosystem.

Vector writes Parquet via the `aws_s3` sink or `file` sink with parquet encoding. Schemas should align with the duckdb-otlp definitions so the catalog can register them as compatible tables.

## When in doubt

- Prefer boring over clever
- Prefer explicit over magic
- Prefer one focused function over five tiny ones if the logic isn't reused
- Prefer stdlib over dependencies
- Ship the thing, polish later
