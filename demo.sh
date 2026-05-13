#!/usr/bin/env bash
# Spin up a working Waddlewatch demo. Builds both binaries, writes a config
# under .demo/, starts waddlewatch + the synthetic generator, and waits for
# Ctrl-C. See docs/setup.md for the manual walkthrough this script automates.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRATCH="${WADDLEWATCH_DEMO_DIR:-$ROOT/.demo}"
PORT="${WADDLEWATCH_PORT:-8080}"
INTERVAL="${WADDLEWATCH_DEMO_INTERVAL:-3s}"
BATCH="${WADDLEWATCH_DEMO_BATCH:-50}"

bin_wdw="$ROOT/.demo/waddlewatch"
bin_demo="$ROOT/.demo/waddlewatch-demo"
config="$SCRATCH/config.yaml"
log_wdw="$SCRATCH/waddlewatch.log"
log_demo="$SCRATCH/demo.log"

cleanup() {
  echo
  echo "[demo] shutting down..."
  if [[ -n "${WDW_PID:-}" ]] && kill -0 "$WDW_PID" 2>/dev/null; then
    kill "$WDW_PID" 2>/dev/null || true
    wait "$WDW_PID" 2>/dev/null || true
  fi
  if [[ -n "${DEMO_PID:-}" ]] && kill -0 "$DEMO_PID" 2>/dev/null; then
    kill "$DEMO_PID" 2>/dev/null || true
    wait "$DEMO_PID" 2>/dev/null || true
  fi
  echo "[demo] done. State preserved in $SCRATCH"
}
trap cleanup EXIT INT TERM

mkdir -p "$SCRATCH/parquet/logs" "$SCRATCH/parquet/metrics" "$SCRATCH/parquet/traces" "$SCRATCH/data"

echo "[demo] building binaries..."
( cd "$ROOT" && go build -o "$bin_wdw" ./cmd/waddlewatch )
( cd "$ROOT" && go build -o "$bin_demo" ./examples/demo )

cat > "$config" <<EOF
listen: 127.0.0.1:$PORT
storage_path: $SCRATCH/parquet
catalog_path: $SCRATCH/catalog.sqlite
data_path: $SCRATCH/data
poll_interval: $INTERVAL
EOF

echo "[demo] starting waddlewatch on http://127.0.0.1:$PORT"
"$bin_wdw" --config "$config" > "$log_wdw" 2>&1 &
WDW_PID=$!

echo -n "[demo] waiting for healthz "
for i in $(seq 1 20); do
  if curl -fsS "http://127.0.0.1:$PORT/healthz" > /dev/null 2>&1; then
    echo "ok (${i}s)"
    break
  fi
  if ! kill -0 "$WDW_PID" 2>/dev/null; then
    echo
    echo "[demo] waddlewatch exited early. Last lines of $log_wdw:"
    tail -20 "$log_wdw" >&2
    exit 1
  fi
  echo -n "."
  sleep 1
done

echo "[demo] starting generator (interval=$INTERVAL, batch=$BATCH)"
"$bin_demo" -out "$SCRATCH/parquet" -interval "$INTERVAL" -batch "$BATCH" > "$log_demo" 2>&1 &
DEMO_PID=$!

cat <<EOF

[demo] ready. Open http://127.0.0.1:$PORT

  logs:    http://127.0.0.1:$PORT/logs
  metrics: http://127.0.0.1:$PORT/metrics
  traces:  http://127.0.0.1:$PORT/traces
  tail:    http://127.0.0.1:$PORT/tail

  waddlewatch log: tail -f $log_wdw
  generator log:   tail -f $log_demo

Press Ctrl-C to stop.
EOF

wait
