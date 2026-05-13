# Waddlewatch setup

This guide walks through running Waddlewatch end to end. The first half uses
the included synthetic demo so you can confirm the binary works without
deploying Vector. The second half points at a real Vector pipeline.

## Prerequisites

- Go 1.23 or newer (only needed to build from source)
- A few hundred MB of disk for parquet output
- Network access on the loopback interface (default port 8080)

## Build

```bash
go build -o waddlewatch ./cmd/waddlewatch
go build -o waddlewatch-demo ./examples/demo
```

## Smoke test with the synthetic demo

The demo writes parquet files into `logs/`, `metrics/`, and `traces/`
subdirectories using the same column shapes the catalog expects from Vector.
Useful when you want to see the UI move without standing up OTel infra.

```bash
mkdir -p /tmp/wdw/parquet /tmp/wdw/data

cat > /tmp/wdw/config.yaml <<'YAML'
listen: 127.0.0.1:8080
storage_path: /tmp/wdw/parquet
catalog_path: /tmp/wdw/catalog.sqlite
data_path: /tmp/wdw/data
poll_interval: 3s
YAML

# Terminal 1: the binary
./waddlewatch --config /tmp/wdw/config.yaml

# Terminal 2: the synthetic generator (writes a fresh batch every 3s)
./waddlewatch-demo -out /tmp/wdw/parquet -interval 3s -batch 50
```

Open <http://127.0.0.1:8080>. Logs and traces should appear within a poll
interval. The metrics chart fills in after at least two batches land so the
time-bucketing has something to plot.

Demo flags:

- `-out`  path that becomes `storage_path` for Waddlewatch (default `/tmp/wdw-demo/parquet`)
- `-interval`  time between batches (default `5s`)
- `-batch`  rows per signal per batch (default `50`)

Stop both processes with Ctrl-C. The catalog file persists, so re-running
either component picks up where you left off.

## Pointing Waddlewatch at Vector

`examples/vector.yaml` is a starting point that exposes the OTLP gRPC and HTTP
receivers and writes a per-signal parquet tree under
`/var/lib/waddlewatch/parquet`. Adjust paths to match your host, then run:

```bash
vector --config examples/vector.yaml
./waddlewatch --config /etc/waddlewatch/config.yaml
```

Make sure Vector's parquet output uses the column names and types declared in
`internal/catalog/catalog.go`. DuckLake validates types at registration time;
mismatched columns surface as `Failed to map column ...` warnings in the
Waddlewatch log and the file is not added to the catalog. The most common
gotchas:

- `attributes` and `resource_attributes` are typed as `JSON`, not `VARCHAR`
- numeric metrics `value` must be `DOUBLE` (not `DECIMAL`)
- timestamps must be `TIMESTAMP` (microsecond precision)

If you need a different schema, edit `schemaDDL()` and rebuild.

## Production-ish deploy

`deploy/Dockerfile` and `deploy/waddlewatch.service` provide a starting point
for container and systemd installs respectively. Both expect a writable
`/var/lib/waddlewatch` for the catalog and DuckLake data files. Neither sets
up authentication. Bind to localhost or front the service with whatever
auth-proxy you already trust.

## Resetting state

The catalog is a single SQLite file plus the DuckLake data directory. To start
fresh:

```bash
rm -rf /tmp/wdw/catalog.sqlite /tmp/wdw/data
```

Your raw parquet files in `storage_path` are untouched. The next poll will
re-register them from scratch.
