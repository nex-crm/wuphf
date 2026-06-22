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

echo "== /run: a running task re-hydrates and returns a projection =="
RUN=$(curl -fsS -X POST "$BASE/run" -H 'Content-Type: application/json' -d '{
  "schema_version": 1, "task_id": "smoke-1",
  "record": {"id":"smoke-1","task_id":"smoke-1","lifecycle_state":"running","owner":"eng","title":"render a chart"},
  "model": "claude-sonnet-4-6",
  "mcp": {"wuphf-office": {"command":"wuphf","args":["mcp-team"],"env_passthrough":["WUPHF_BROKER_TOKEN","WUPHF_BROKER_BASE"]}}
}')
echo "$RUN" | python3 -m json.tool
echo "$RUN" | python3 -c 'import sys,json; d=json.load(sys.stdin); p=d["projection"]; assert p["lifecycle_state"]=="running" and p["pipeline_stage"]=="implement", d; assert set(p)=={"task_id","lifecycle_state","pipeline_stage","review_state","status","blocked"}, p' || fail "projection shape drifted from the Go decode"

echo "== /run: an unmappable record fails loud to unknown =="
UNK=$(curl -fsS -X POST "$BASE/run" -H 'Content-Type: application/json' -d '{
  "schema_version": 1, "task_id": "smoke-2",
  "record": {"id":"smoke-2","pipeline_stage":"nonsense","review_state":"weird","status":"???"}
}')
echo "$UNK" | python3 -m json.tool
echo "$UNK" | python3 -c 'import sys,json; d=json.load(sys.stdin); assert d["projection"]["lifecycle_state"]=="unknown", d' || fail "unmappable record did not fail loud to unknown"

echo "SMOKE OK"
