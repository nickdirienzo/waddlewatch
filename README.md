# Waddlewatch

A self-hosted observability stack you can run on a Raspberry Pi.

Watches your services like a duck watches its pond. Vigilant, weird, low-effort.

## What it is

Waddlewatch ingests OpenTelemetry data (logs, metrics, traces) via [Vector](https://vector.dev), stores it as Parquet files registered in a [DuckLake](https://ducklake.select) catalog, and serves a queryable web UI backed by [DuckDB](https://duckdb.org).

The whole stack is intentionally boring:

- One Go binary
- One config file
- One catalog file (DuckDB)
- Parquet files on disk or in S3

Your data lives in open files. You can query it with anything that reads Parquet, with or without Waddlewatch running. The binary is doing accounting, not data storage.

## Status

Experimental side project. Not production. Don't bet your job on it. Probably fine for your homelab.

## Architecture

```
┌──────────────┐    OTLP    ┌─────────┐   Parquet    ┌──────────────┐
│ Your apps    │───────────▶│ Vector  │─────────────▶│  /var/wdw/   │
└──────────────┘            └─────────┘              │   logs/      │
                                                     │   metrics/   │
                                                     │   traces/    │
                                                     └──────┬───────┘
                                                            │
                                                            ▼
                          ┌─────────────────────────────────────────┐
                          │ Waddlewatch (Go binary)                 │
                          │                                         │
                          │  Cron: ducklake_add_data_files          │
                          │  HTTP: htmx UI + DuckDB query layer     │
                          │  SSE: live tail                         │
                          └────────────────────┬────────────────────┘
                                               ▲
                                               │ HTTPS
                                               │
                                        ┌──────┴────────┐
                                        │ Your browser  │
                                        └───────────────┘
```

## Quickstart

Install Vector. Point it at a directory or S3 bucket via the included example config.

```bash
# Build
go build -o waddlewatch ./cmd/waddlewatch

# Run
./waddlewatch --config config.yaml
```

Open `http://localhost:8080`. Query your stuff.

A full setup walkthrough lives in [docs/setup.md](docs/setup.md).

## Stack

- **Ingest**: Vector (external, not part of this repo)
- **Storage**: Parquet files on disk or S3
- **Catalog**: DuckLake, DuckDB-file backed
- **Query**: DuckDB embedded in the binary
- **UI**: htmx, html/template, a touch of Chart.js
- **Binary**: Go

## Why

ClickHouse-based observability stacks are operationally heavy. Datadog is expensive. Most self-hosted alternatives still require running a real database. Waddlewatch wants to be the "one binary plus some files" version that works for homelab and small-team use.

Lakehouse architecture (DuckLake + Parquet + DuckDB) makes this possible without a separate OLAP database. Vector handles the parts that need to be hardened. Everything else is small enough to be one Go binary.

## Limitations

- Not multi-tenant. Single user, bind to a trusted network.
- No alerting or notification.
- No trace waterfall view. Span tables only.
- Query memory limited by host RAM. Not designed for "year of data" aggregations on a Raspberry Pi.
- DuckLake is new. Tooling for "oh shit something broke" is less mature than Postgres or ClickHouse.

## License

MIT. Do whatever.

## Why "Waddlewatch"

It watches your stuff. Ducks waddle. Look, just go with it.
