#!/usr/bin/env bash
# Live smoke for the operator agent service: boots it and exercises the FE-facing
# endpoints, including a real key-free /build/stream against a local model.
set -euo pipefail
cd "$(dirname "$0")/.."
PORT="${AGENT_SMOKE_PORT:-8821}"
BASE="http://127.0.0.1:${PORT}"

PORT="$PORT" bun run src/service.ts &
PID=$!
trap 'kill "$PID" 2>/dev/null || true' EXIT

for _ in $(seq 1 50); do curl -fsS -m 1 "$BASE/health" >/dev/null 2>&1 && break; sleep 0.2; done
fail() { echo "SMOKE FAIL: $1" >&2; exit 1; }

echo "== health =="; curl -fsS "$BASE/health" | python3 -m json.tool
echo "== providers =="; curl -fsS "$BASE/providers" | python3 -m json.tool

echo "== /run: gated step halts, then completes when approved =="
SPEC='{"name":"n","tool_id":"t","narration":"","clarify":null,"steps":[{"id":"a","kind":"action","title":"Route","detail":"d","integration":"Slack","gated":true}]}'
curl -fsS -X POST "$BASE/run" -H 'content-type: application/json' -d "{\"spec\":$SPEC,\"input\":{}}" | python3 -c 'import sys,json;d=json.load(sys.stdin);assert d["status"]=="needs_approval",d' || fail "gate did not halt"
curl -fsS -X POST "$BASE/run" -H 'content-type: application/json' -d "{\"spec\":$SPEC,\"input\":{\"approved\":[\"a\"]}}" | python3 -c 'import sys,json;d=json.load(sys.stdin);assert d["status"]=="done",d' || fail "approved run did not complete"

echo "== /build/stream: pi-mono compiles a spec LIVE (key-free, local model) =="
BUILD=$(curl -fsS -N -m 150 -X POST "$BASE/build/stream" -H 'content-type: application/json' \
  -d '{"schema_version":1,"message":"route inbound demo requests to a slack channel"}')
echo "$BUILD" | grep -q 'event: spec' || fail "no spec event from /build/stream"
echo "build stream OK (spec emitted)"
echo "SMOKE OK"
