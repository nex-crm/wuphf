#!/usr/bin/env bash
# shellcheck disable=SC2317,SC2329
# (SC2317 / SC2329 — the teardown function and its body are dispatched
# indirectly via the EXIT trap; shellcheck can't see the indirect call
# and flags those lines as unreachable. Same convention as
# icp-tutorial-harness.sh.)
#
# Phase 5 final scenario walk — all three ICPs from the
# onboarding-into-office spec, end-to-end through the real broker.
#
# Marcus is the deterministic zero-LLM gate (already passed in Phase 2;
# re-run here as a regression check). Sam (scratch) and Priya
# (blueprint) hit the real claude CLI via the configured LLM provider
# to exercise the draft writer + Approve & Start + execution lineup
# flow.
#
# Usage:
#   bash scripts/phase5-icp-walks.sh [--scenario=marcus|sam|priya|all]
#
# Defaults to all three. Output goes to
# /tmp/phase5-icp-results-<timestamp>/. Exit code is the number of
# failed scenarios.

set -euo pipefail

REPO_ROOT="$(git -C "$(dirname "$0")/.." rev-parse --show-toplevel)"
TS="$(date -u +%Y%m%dT%H%M%SZ)"
OUT_DIR="/tmp/phase5-icp-results-${TS}"
BROKER_PORT="${WUPHF_PHASE5_PORT:-7893}"
WEB_PORT="$((BROKER_PORT + 1))"
SCENARIO="all"

for arg in "$@"; do
  case "$arg" in
    --scenario=*) SCENARIO="${arg#*=}" ;;
    *) echo "unknown arg: $arg" >&2; exit 2 ;;
  esac
done

mkdir -p "$OUT_DIR"
RUNTIME_HOME="$OUT_DIR/runtime"
mkdir -p "$RUNTIME_HOME"
WUPHF_LOG="$OUT_DIR/wuphf.log"
WUPHF_BINARY="$OUT_DIR/wuphf"

log() { printf "[icp] %s\n" "$*"; }
fail() { printf "[icp] FAIL: %s\n" "$*" >&2; FAILED=$((FAILED + 1)); }

FAILED=0
FAILED_SCENARIOS=""

log "build wuphf -> $WUPHF_BINARY"
( cd "$REPO_ROOT" && go build -o "$WUPHF_BINARY" ./cmd/wuphf )

if lsof -nP -iTCP:"$BROKER_PORT" -sTCP:LISTEN 2>/dev/null | grep -q LISTEN; then
  echo "[icp] FAIL: port $BROKER_PORT already in use" >&2
  exit 1
fi

log "boot wuphf on broker=$BROKER_PORT runtime=$RUNTIME_HOME"
WUPHF_RUNTIME_HOME="$RUNTIME_HOME" "$WUPHF_BINARY" \
  --broker-port "$BROKER_PORT" --web-port "$WEB_PORT" --no-open \
  > "$WUPHF_LOG" 2>&1 &
WUPHF_PID=$!

teardown() {
  if kill -0 "$WUPHF_PID" 2>/dev/null; then
    kill "$WUPHF_PID" 2>/dev/null || true
    wait "$WUPHF_PID" 2>/dev/null || true
  fi
}
trap teardown EXIT

for _ in $(seq 1 30); do
  if ! kill -0 "$WUPHF_PID" 2>/dev/null; then
    echo "[icp] FAIL: wuphf exited during boot; see $WUPHF_LOG" >&2; exit 1
  fi
  if curl -fs --connect-timeout 2 --max-time 5 \
      "http://localhost:${BROKER_PORT}/health" >/dev/null 2>&1; then
    log "wuphf up (pid=$WUPHF_PID)"
    break
  fi
  sleep 0.5
done

TOKEN_FILE="/tmp/wuphf-broker-token-${BROKER_PORT}"
if [ ! -f "$TOKEN_FILE" ]; then
  echo "[icp] FAIL: broker token file not found at $TOKEN_FILE" >&2; exit 1
fi
TOKEN="$(cat "$TOKEN_FILE")"

api() {
  local method="$1" path="$2" body="${3-}"
  local url="http://localhost:${BROKER_PORT}${path}"
  if [ -n "$body" ]; then
    curl -fsS --connect-timeout 2 --max-time 60 -X "$method" \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d "$body" "$url"
  else
    curl -fsS --connect-timeout 2 --max-time 60 -X "$method" \
      -H "Authorization: Bearer $TOKEN" "$url"
  fi
}

reset_state() {
  # Wipe onboarding state between scenarios so each starts from a
  # fresh greet phase. The runtime is persistent across the harness;
  # only onboarded.json is reset.
  rm -f "$RUNTIME_HOME/onboarded.json" 2>/dev/null || true
}

# ----------------------------------------------------------------
# Scenario: Marcus (look around first — deterministic only)
# ----------------------------------------------------------------
scenario_marcus() {
  log ""
  log "===== Marcus: deterministic look-around ====="
  reset_state

  # Phase 1: pre-pick (writes initial state with Phase="greet" implicitly
  # via the legacy /onboarding/complete path that already exists).
  api POST /onboarding/complete '{
    "company":"","description":"","priority":"","website":"",
    "owner_name":"","owner_role":"","scan_completed":false,
    "runtime":"claude-code","runtime_priority":["claude-code"],
    "memory_backend":"markdown","blueprint":"","agents":[],
    "api_keys":{},"task":"","skip_task":true
  }' >/dev/null

  state="$(api GET /onboarding/state)"
  onboarded="$(echo "$state" | jq -r '.onboarded // false')"
  if [ "$onboarded" != "true" ]; then
    fail "Marcus: expected onboarded=true after /onboarding/complete"
    FAILED_SCENARIOS="$FAILED_SCENARIOS marcus"
    return
  fi

  # Count LLM hits to date.
  before_llm="$(grep -cE 'ceo_draft:|llm.dispatch|provider.invoke' "$WUPHF_LOG" || true)"
  if [ "$before_llm" -gt 0 ]; then
    fail "Marcus: $before_llm LLM hits already in log before walk (expected 0)"
    FAILED_SCENARIOS="$FAILED_SCENARIOS marcus"
    return
  fi
  log "✓ Marcus: onboarded=true, ZERO LLM hits"
}

# ----------------------------------------------------------------
# Scenario: Sam (scratch + first issue via LLM draft)
# ----------------------------------------------------------------
scenario_sam() {
  log ""
  log "===== Sam: scratch path + LLM-drafted first issue ====="
  reset_state

  # Pre-pick (claude runtime).
  api POST /onboarding/complete '{
    "company":"","description":"","priority":"","website":"",
    "owner_name":"","owner_role":"","scan_completed":false,
    "runtime":"claude-code","runtime_priority":["claude-code"],
    "memory_backend":"markdown","blueprint":"","agents":[],
    "api_keys":{},"task":"","skip_task":true
  }' >/dev/null

  # Walk the phase machine: greet → identity → scan → blueprint → seed → bridge → draft.
  # In this harness we drive transitions explicitly; in the UI the cards drive them.
  api POST /onboarding/answer '{"field":"company_name","value":"Acme Billing"}' >/dev/null
  api POST /onboarding/transition '{"phase":"identity"}' >/dev/null || true
  api POST /onboarding/answer '{"field":"description","value":"Subscription billing for indie SaaS."}' >/dev/null
  api POST /onboarding/transition '{"phase":"scan"}' >/dev/null || true
  api POST /onboarding/answer '{"field":"scan_complete","value":true}' >/dev/null
  api POST /onboarding/transition '{"phase":"blueprint"}' >/dev/null || true
  api POST /onboarding/answer '{"field":"blueprint_id","value":""}' >/dev/null
  api POST /onboarding/transition '{"phase":"seed"}' >/dev/null || true
  api POST /onboarding/transition '{"phase":"bridge"}' >/dev/null || true

  # The user types their first issue prompt and triggers draft.
  api POST /onboarding/answer '{
    "field":"task_prompt",
    "value":"Build a Stripe webhook handler that verifies signatures, updates subscription state, and sends an email on past_due."
  }' >/dev/null
  api POST /onboarding/transition '{"phase":"draft"}' >/dev/null || true

  # Wait up to 90s for the draft writer to populate the issue.
  log "Sam: waiting for LLM draft (up to 90s)…"
  local issue_id deadline spec_filled
  deadline=$(( $(date +%s) + 90 ))
  spec_filled=0
  while [ "$(date +%s)" -lt "$deadline" ]; do
    state="$(api GET /onboarding/state)"
    issue_id="$(echo "$state" | jq -r '.first_issue_id // empty')"
    if [ -n "$issue_id" ]; then
      # Probe the task for filled spec sections.
      task="$(api GET "/tasks/$issue_id" 2>/dev/null || true)"
      if [ -n "$task" ]; then
        goal="$(echo "$task" | jq -r '.issue_draft_spec.goal // empty' 2>/dev/null || true)"
        accept="$(echo "$task" | jq -r '.issue_draft_spec.acceptance // empty' 2>/dev/null || true)"
        if [ -n "$goal" ] && [ -n "$accept" ]; then
          spec_filled=1
          break
        fi
      fi
    fi
    sleep 3
  done

  if [ "$spec_filled" != "1" ]; then
    fail "Sam: LLM draft did not populate spec sections within 90s (issue=$issue_id)"
    echo "$state" > "$OUT_DIR/sam-state.json"
    if [ -n "$issue_id" ]; then
      api GET "/tasks/$issue_id" > "$OUT_DIR/sam-task.json" 2>/dev/null || true
    fi
    FAILED_SCENARIOS="$FAILED_SCENARIOS sam"
    return
  fi
  log "✓ Sam: LLM draft populated (issue=$issue_id)"

  # Save the task snapshot for inspection.
  api GET "/tasks/$issue_id" > "$OUT_DIR/sam-task.json"

  # Approve & Start: the existing /tasks/<id>/decision flow used by PR #885.
  # The dispatch gate refuses execution dispatch until this approves.
  approve_resp="$(api POST "/tasks/$issue_id/decision" '{"action":"approve"}' 2>"$OUT_DIR/sam-approve.err" || true)"
  if [ -z "$approve_resp" ] && [ -s "$OUT_DIR/sam-approve.err" ]; then
    fail "Sam: approve action failed; see $OUT_DIR/sam-approve.err"
    FAILED_SCENARIOS="$FAILED_SCENARIOS sam"
    return
  fi
  log "✓ Sam: Approve & Start posted"

  # Verify the task transitioned to Running.
  sleep 2
  task_post="$(api GET "/tasks/$issue_id")"
  state_post="$(echo "$task_post" | jq -r '.lifecycleState // .lifecycle_state // empty')"
  if [ "$state_post" != "running" ] && [ "$state_post" != "approved" ]; then
    fail "Sam: expected post-approve state in {running,approved}; got '$state_post'"
    FAILED_SCENARIOS="$FAILED_SCENARIOS sam"
    return
  fi
  log "✓ Sam: post-approve state=$state_post"
  log "✓ Sam path: PASS"
}

# ----------------------------------------------------------------
# Scenario: Priya (blueprint + LLM draft)
# ----------------------------------------------------------------
scenario_priya() {
  log ""
  log "===== Priya: blueprint path + LLM-drafted first issue ====="
  reset_state

  api POST /onboarding/complete '{
    "company":"","description":"","priority":"","website":"",
    "owner_name":"","owner_role":"","scan_completed":false,
    "runtime":"claude-code","runtime_priority":["claude-code"],
    "memory_backend":"markdown","blueprint":"","agents":[],
    "api_keys":{},"task":"","skip_task":true
  }' >/dev/null

  # Blueprint path: pick a known operations template (search templates for
  # available ids — youtube-factory is one canonical media blueprint).
  api POST /onboarding/answer '{"field":"company_name","value":"Lumen Studio"}' >/dev/null
  api POST /onboarding/transition '{"phase":"identity"}' >/dev/null || true
  api POST /onboarding/answer '{"field":"description","value":"Short-form video studio for SaaS founders."}' >/dev/null
  api POST /onboarding/transition '{"phase":"scan"}' >/dev/null || true
  api POST /onboarding/answer '{"field":"scan_complete","value":true}' >/dev/null
  api POST /onboarding/transition '{"phase":"blueprint"}' >/dev/null || true
  api POST /onboarding/answer '{"field":"blueprint_id","value":"youtube-factory"}' >/dev/null
  api POST /onboarding/transition '{"phase":"team"}' >/dev/null || true
  api POST /onboarding/transition '{"phase":"seed"}' >/dev/null || true
  api POST /onboarding/transition '{"phase":"bridge"}' >/dev/null || true

  api POST /onboarding/answer '{
    "field":"task_prompt",
    "value":"Script a 60-second short on \"why founders should stop building dashboards\". 90% hook, 10% CTA."
  }' >/dev/null
  api POST /onboarding/transition '{"phase":"draft"}' >/dev/null || true

  log "Priya: waiting for LLM draft (up to 90s)…"
  local issue_id deadline spec_filled
  deadline=$(( $(date +%s) + 90 ))
  spec_filled=0
  while [ "$(date +%s)" -lt "$deadline" ]; do
    state="$(api GET /onboarding/state)"
    issue_id="$(echo "$state" | jq -r '.first_issue_id // empty')"
    if [ -n "$issue_id" ]; then
      task="$(api GET "/tasks/$issue_id" 2>/dev/null || true)"
      if [ -n "$task" ]; then
        goal="$(echo "$task" | jq -r '.issue_draft_spec.goal // empty' 2>/dev/null || true)"
        accept="$(echo "$task" | jq -r '.issue_draft_spec.acceptance // empty' 2>/dev/null || true)"
        if [ -n "$goal" ] && [ -n "$accept" ]; then
          spec_filled=1
          break
        fi
      fi
    fi
    sleep 3
  done

  if [ "$spec_filled" != "1" ]; then
    fail "Priya: LLM draft did not populate spec sections within 90s (issue=$issue_id)"
    echo "$state" > "$OUT_DIR/priya-state.json"
    if [ -n "$issue_id" ]; then
      api GET "/tasks/$issue_id" > "$OUT_DIR/priya-task.json" 2>/dev/null || true
    fi
    FAILED_SCENARIOS="$FAILED_SCENARIOS priya"
    return
  fi
  log "✓ Priya: LLM draft populated (issue=$issue_id)"
  api GET "/tasks/$issue_id" > "$OUT_DIR/priya-task.json"
  log "✓ Priya path: PASS"
}

# ----------------------------------------------------------------
# Dispatch
# ----------------------------------------------------------------
case "$SCENARIO" in
  marcus) scenario_marcus ;;
  sam)    scenario_sam ;;
  priya)  scenario_priya ;;
  all)    scenario_marcus; scenario_sam; scenario_priya ;;
  *) echo "unknown scenario: $SCENARIO" >&2; exit 2 ;;
esac

log ""
log "================================"
log " Results"
log "================================"
log " broker log:   $WUPHF_LOG"
log " artifacts:    $OUT_DIR/"
if [ "$FAILED" -eq 0 ]; then
  log " PASS — all scenarios green"
else
  log " FAIL — $FAILED scenario(s) failed:$FAILED_SCENARIOS"
fi
log ""
exit "$FAILED"
