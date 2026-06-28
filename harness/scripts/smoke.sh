#!/usr/bin/env bash
# Live smoke for the operator harness: boots the FastAPI service and exercises the
# exact FE-facing endpoints (/health, /providers, /build/stream SSE, /run incl. the
# approval-gate halt). Run from harness/ after building the venv.
set -euo pipefail

cd "$(dirname "$0")/.."
PORT="${HARNESS_SMOKE_PORT:-8810}"
BASE="http://127.0.0.1:${PORT}"

.venv/bin/uvicorn harness.service:app --app-dir src --port "$PORT" --log-level warning &
PID=$!
trap 'kill "$PID" 2>/dev/null || true' EXIT

for _ in $(seq 1 50); do
  if curl -fsS -m 1 "$BASE/health" >/dev/null 2>&1; then break; fi
  sleep 0.2
done

fail() { echo "SMOKE FAIL: $1" >&2; exit 1; }

echo "== health =="; curl -fsS "$BASE/health" | python3 -m json.tool
echo "== providers =="; curl -fsS "$BASE/providers" | python3 -m json.tool

echo "== /build/stream: a description assembles a WorkflowSpec (SSE) =="
BUILD=$(curl -fsS -N --max-time 15 -X POST "$BASE/build/stream" -H 'Content-Type: application/json' \
  -d '{"schema_version":1,"message":"route inbound demo requests to a slack channel"}')
echo "$BUILD" | grep -q 'event: spec' || fail "no spec event in the build stream"
echo "$BUILD" | grep -q '"tool_id": "inbound-routing"' || echo "$BUILD" | grep -q 'inbound-routing' || fail "spec did not resolve the tool"
echo "build stream OK (spec emitted)"

echo "== /run: a gated step halts for approval, then completes once approved =="
SPEC='{"name":"n","tool_id":"inbound-routing","steps":[{"id":"p-action","kind":"action","title":"Route","detail":"d","integration":"Slack","gated":true}]}'
R1=$(curl -fsS --max-time 10 -X POST "$BASE/run" -H 'Content-Type: application/json' -d "{\"schema_version\":1,\"spec\":$SPEC,\"input\":{}}")
echo "$R1" | python3 -c 'import sys,json;d=json.load(sys.stdin);assert d["status"]=="needs_approval",d;assert d["pending_approval"]["step_id"]=="p-action",d' || fail "gated step did not halt for approval"
R2=$(curl -fsS --max-time 10 -X POST "$BASE/run" -H 'Content-Type: application/json' -d "{\"schema_version\":1,\"spec\":$SPEC,\"input\":{\"approved\":[\"p-action\"]}}")
echo "$R2" | python3 -c 'import sys,json;d=json.load(sys.stdin);assert d["status"]=="done",d' || fail "approved run did not complete"

echo "SMOKE OK"
