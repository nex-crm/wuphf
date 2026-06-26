#!/usr/bin/env bash
# Live cross-language smoke for the orchestrator seam: boots the FastAPI sidecar
# and POSTs the exact wire shapes the Go DispatchClient sends
# (internal/provider/deepagents.go), asserting the StepResult contract holds.
#
# Proves end-to-end, without the broker: Go-shaped DispatchRequest -> Python
# pydantic decode -> re-hydrate -> graph -> harness -> StepResult/projection that
# the Go client decodes. Run from the orchestrator/ directory:
#
#   bash scripts/smoke.sh
#
# Requires the venv built per README (uv venv .venv && uv pip install ...).
set -euo pipefail

cd "$(dirname "$0")/.."
PORT="${ORCH_SMOKE_PORT:-8770}"
BASE="http://127.0.0.1:${PORT}"

.venv/bin/uvicorn orchestrator.service:app --app-dir src --port "$PORT" --log-level warning &
PID=$!
trap 'kill "$PID" 2>/dev/null || true' EXIT

# Wait for /health.
for _ in $(seq 1 50); do
  if curl -fsS -m 1 "$BASE/health" >/dev/null 2>&1; then break; fi
  sleep 0.2
done

fail() { echo "SMOKE FAIL: $1" >&2; exit 1; }

echo "== health =="
curl -fsS "$BASE/health" | python3 -m json.tool

echo "== /run: a running task submits for review and parks at the gate (not re-dispatchable) =="
# FakeHarness (no SDK) submits for review, so the step interrupts at a human gate.
# The projection MUST be the gate state (review), never the pre-gate executable
# "running" — otherwise the broker would re-dispatch the task every tick.
RUN=$(curl -fsS -X POST "$BASE/run" -H 'Content-Type: application/json' -d '{
  "schema_version": 1, "task_id": "smoke-1",
  "record": {"id":"smoke-1","task_id":"smoke-1","lifecycle_state":"running","owner":"eng","title":"render a chart"},
  "model": "claude-sonnet-4-6",
  "mcp": {"wuphf-office": {"command":"wuphf","args":["mcp-team"],"env_passthrough":["WUPHF_BROKER_TOKEN","WUPHF_BROKER_BASE"]}}
}')
echo "$RUN" | python3 -m json.tool
echo "$RUN" | python3 -c 'import sys,json; d=json.load(sys.stdin); assert d["status"]=="interrupted", d; p=d["projection"]; assert p["lifecycle_state"]=="review" and p["pipeline_stage"]=="review", d; assert set(p)=={"task_id","lifecycle_state","pipeline_stage","review_state","status","blocked"}, p' || fail "gate projected an executable state (re-dispatch loop) or shape drifted"

echo "== /run: a mismatched schema_version fails loud (400) =="
CODE=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/run" -H 'Content-Type: application/json' -d '{
  "schema_version": 999, "task_id": "smoke-v", "record": {"task_id":"smoke-v","lifecycle_state":"running"}
}')
[ "$CODE" = "400" ] || fail "schema_version mismatch was not rejected (got HTTP $CODE)"
echo "schema_version 999 -> HTTP $CODE"

echo "== /run: an unmappable record fails loud to unknown =="
UNK=$(curl -fsS -X POST "$BASE/run" -H 'Content-Type: application/json' -d '{
  "schema_version": 1, "task_id": "smoke-2",
  "record": {"id":"smoke-2","pipeline_stage":"nonsense","review_state":"weird","status":"???"}
}')
echo "$UNK" | python3 -m json.tool
echo "$UNK" | python3 -c 'import sys,json; d=json.load(sys.stdin); p=d["projection"]; assert p["lifecycle_state"]=="unknown" and p["status"]=="unknown", d; assert set(p)=={"task_id","lifecycle_state","pipeline_stage","review_state","status","blocked"}, p' || fail "unmappable record did not fail loud to unknown with full shape"

echo "SMOKE OK"
