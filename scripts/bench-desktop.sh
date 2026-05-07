#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

RESULT_DIR="$ROOT/bench/results"
TIMESTAMP="$(date -u +"%Y%m%dT%H%M%SZ")"
RUN_ID="${TIMESTAMP}-$$"
OUT_FILE="$RESULT_DIR/$RUN_ID.json"
WUPHF_BIN="${WUPHF_BENCH_WUPHF_BINARY:-$ROOT/wuphf}"
BENCH_BIN="${WUPHFBENCH_BIN:-$ROOT/wuphfbench}"
PROBE="${WUPHFBENCH_PROBE:-all}"

mkdir -p "$RESULT_DIR"

if [[ ! -x "$WUPHF_BIN" ]]; then
  go build -o "$WUPHF_BIN" ./cmd/wuphf
fi

go build -o "$BENCH_BIN" ./cmd/wuphfbench

args=(--probe "$PROBE" --out json)
if [[ -n "${WUPHFBENCH_ITERS:-}" ]]; then
  args+=(--iters "$WUPHFBENCH_ITERS")
fi

WUPHF_BENCH_WUPHF_BINARY="$WUPHF_BIN" "$BENCH_BIN" "${args[@]}" > "$OUT_FILE"
echo "$OUT_FILE"
