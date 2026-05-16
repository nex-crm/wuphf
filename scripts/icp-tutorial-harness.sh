#!/usr/bin/env bash
# shellcheck disable=SC2329
# (SC2329 — all scenario_* helpers are dispatched indirectly via the
# ALL_SCENARIOS array; shellcheck can't see the dynamic invocation.)
# icp-tutorial-harness.sh
#
# Runs each of the 10 ICP tutorial scenarios against a freshly booted
# wuphf instance via the broker REST API, scores each against its
# "What success looks like" line, and writes a JSON + Markdown report
# to /tmp/icp-tutorial-results-<timestamp>/.
#
# The harness does NOT spin up a browser. Tutorial steps that require
# human interaction (typing a goal in a channel, editing a JSON file,
# waiting 24h between sessions) are exercised by hitting the same
# REST endpoints the UI uses.
#
# Usage:
#   bash scripts/icp-tutorial-harness.sh [--keep-runtime] [--scenarios=01a,02a,...]
#
# Flags:
#   --keep-runtime         Leave the wuphf runtime home on disk for
#                          inspection. Default is to wipe.
#   --scenarios=<csv>      Run only the named scenarios; default = all 10.
#   --binary=<path>        Use this wuphf binary instead of building.
#   --no-llm               Skip scenarios that require an LLM provider
#                          response (01a step 3, 02a, 02b, 03a, 03b,
#                          05a, 05b). Just verifies API contracts.
#
# Exit code is the number of failed scenarios.

set -euo pipefail

# -----------------------------------------------------------------------------
# Config

REPO_ROOT="$(git -C "$(dirname "$0")/.." rev-parse --show-toplevel)"
TS="$(date -u +%Y%m%dT%H%M%SZ)"
REPORT_DIR="/tmp/icp-tutorial-results-${TS}"
RUNTIME_HOME=""
BROKER_PORT="${WUPHF_HARNESS_PORT:-7888}"
WEB_PORT="$((BROKER_PORT + 1))"
WUPHF_BINARY=""
LLM_GATED=true
SCENARIOS=""
KEEP_RUNTIME=false

for arg in "$@"; do
  case "$arg" in
    --keep-runtime) KEEP_RUNTIME=true ;;
    --no-llm) LLM_GATED=false ;;
    --scenarios=*) SCENARIOS="${arg#*=}" ;;
    --binary=*) WUPHF_BINARY="${arg#*=}" ;;
    *) echo "unknown arg: $arg" >&2; exit 2 ;;
  esac
done

# -----------------------------------------------------------------------------
# Output

mkdir -p "$REPORT_DIR"
RESULTS_JSON="$REPORT_DIR/results.json"
RESULTS_MD="$REPORT_DIR/RESULTS.md"
WUPHF_LOG="$REPORT_DIR/wuphf.log"
echo "[]" > "$RESULTS_JSON"

log()  { printf "[harness] %s\n" "$*"; }
fail() { printf "[harness] FAIL: %s\n" "$*" >&2; }

# Append a JSON result object to RESULTS_JSON.
record_result() {
  local scenario="$1" status="$2" notes="$3"
  local tmp
  tmp="$(mktemp)"
  jq --arg s "$scenario" --arg st "$status" --arg n "$notes" \
    '. + [{scenario:$s, status:$st, notes:$n, at:(now | todate)}]' \
    "$RESULTS_JSON" > "$tmp"
  mv "$tmp" "$RESULTS_JSON"
}

# -----------------------------------------------------------------------------
# Build / boot wuphf

build_binary() {
  if [ -n "$WUPHF_BINARY" ] && [ -x "$WUPHF_BINARY" ]; then
    log "using provided binary: $WUPHF_BINARY"
    return
  fi
  WUPHF_BINARY="$REPORT_DIR/wuphf"
  log "building wuphf -> $WUPHF_BINARY"
  ( cd "$REPO_ROOT" && go build -o "$WUPHF_BINARY" ./cmd/wuphf )
}

boot_wuphf() {
  RUNTIME_HOME="$REPORT_DIR/runtime"
  mkdir -p "$RUNTIME_HOME"
  log "booting wuphf on broker=$BROKER_PORT web=$WEB_PORT runtime=$RUNTIME_HOME"
  WUPHF_RUNTIME_HOME="$RUNTIME_HOME" "$WUPHF_BINARY" \
    --broker-port "$BROKER_PORT" --web-port "$WEB_PORT" --no-open \
    > "$WUPHF_LOG" 2>&1 &
  WUPHF_PID=$!
  echo "$WUPHF_PID" > "$REPORT_DIR/wuphf.pid"
  # Wait for the broker to start listening.
  for _ in $(seq 1 30); do
    if curl -fs "http://localhost:${BROKER_PORT}/health" >/dev/null 2>&1; then
      log "wuphf up (pid=$WUPHF_PID)"
      return
    fi
    sleep 0.5
  done
  fail "wuphf did not come up within 15s"
  exit 1
}

teardown() {
  if [ -f "$REPORT_DIR/wuphf.pid" ]; then
    local pid
    pid="$(cat "$REPORT_DIR/wuphf.pid")"
    log "stopping wuphf pid=$pid"
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
  if [ "$KEEP_RUNTIME" = false ]; then
    rm -rf "$RUNTIME_HOME"
  fi
}
trap teardown EXIT

# -----------------------------------------------------------------------------
# REST helpers

token() { cat "/tmp/wuphf-broker-token-${BROKER_PORT}"; }

api() {
  local method="$1" path="$2" body="${3-}"
  local url="http://localhost:${BROKER_PORT}${path}"
  if [ -n "$body" ]; then
    curl -fsS -X "$method" \
      -H "Authorization: Bearer $(token)" \
      -H "Content-Type: application/json" \
      -d "$body" "$url"
  else
    curl -fsS -X "$method" \
      -H "Authorization: Bearer $(token)" "$url"
  fi
}

# -----------------------------------------------------------------------------
# Scenarios

# Each scenario function returns 0 on success, non-zero on failure.
# It must call record_result with one of: pass / fail / skipped.

scenario_01a_alex_first_install() {
  # 01a: install + first look. Verifies the canonical 4 agents + #general.
  log "01a: install + first look"
  local members channels
  members="$(api GET /office-members | jq -r '[.members[].slug] | sort | join(",")')"
  channels="$(api GET /channels | jq -r '[.channels[].slug] | join(",")')"
  if [ "$members" != "ceo,executor,planner,reviewer" ]; then
    record_result "01a" "fail" "expected ceo+planner+executor+reviewer roster, got: $members"
    return 1
  fi
  if ! echo "$channels" | grep -q "general"; then
    record_result "01a" "fail" "expected #general channel, got: $channels"
    return 1
  fi
  record_result "01a" "pass" "4-agent roster + #general present"
}

scenario_01b_jordan_pack() {
  # 01b: pack-aware install. The harness boots without --pack so we
  # only confirm the version chip is queryable. The pack flow itself
  # is a CLI invocation and not a runtime check.
  log "01b: founding-team pack (CLI gate)"
  local health
  health="$(api GET /health | jq -r '.build.version')"
  if [ -z "$health" ]; then
    record_result "01b" "fail" "/health missing build.version"
    return 1
  fi
  record_result "01b" "skipped" "pack=founding-team flow is a CLI invocation; harness boots default pack. Version chip queryable: $health"
}

scenario_02a_sam_onboarding() {
  # 02a: drop a goal, observe CEO decomposition + named-agent task creation.
  if [ "$LLM_GATED" = false ]; then
    record_result "02a" "skipped" "--no-llm passed"
    return 0
  fi
  log "02a: drop onboarding goal"
  api POST /messages '{"channel":"general","content":"Ship the onboarding flow by Friday. Planner owns scoping, Executor builds it, Reviewer signs off."}' >/dev/null
  # Wait up to 180s for at least 1 task to appear in inbox.
  local deadline now items
  now="$(date +%s)"
  deadline=$((now + 180))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    items="$(api GET '/inbox/items?filter=all' | jq '.items | length')"
    if [ "$items" -ge 1 ]; then break; fi
    sleep 5
  done
  if [ "$items" -lt 1 ]; then
    record_result "02a" "fail" "no inbox items appeared within 180s"
    return 1
  fi
  # Optional check: at least one task is assigned to planner / executor / reviewer.
  local assigned
  assigned="$(api GET '/inbox/items?filter=all' | jq -r '[.items[] | select(.kind=="task") | .agentSlug] | sort | unique | join(",")')"
  record_result "02a" "pass" "$items inbox items; agents assigned: $assigned"
}

scenario_02b_riley_buildflag() {
  if [ "$LLM_GATED" = false ]; then
    record_result "02b" "skipped" "--no-llm passed"
    return 0
  fi
  log "02b: build-flag goal — clarifying question expected"
  api POST /messages '{"channel":"general","content":"Add a kill switch for the new pricing experiment. Should default to off in production but be flippable per environment without a deploy."}' >/dev/null
  # Heuristic: wait for at least one new agent message in #general.
  local before after
  before="$(api GET '/messages?channel=general' | jq '.messages | length')"
  sleep 60
  after="$(api GET '/messages?channel=general' | jq '.messages | length')"
  if [ "$after" -le "$before" ]; then
    record_result "02b" "fail" "no agent reply in #general within 60s"
    return 1
  fi
  record_result "02b" "pass" "$((after - before)) new messages in #general"
}

scenario_03a_alex_svgblocker() {
  if [ "$LLM_GATED" = false ]; then
    record_result "03a" "skipped" "--no-llm passed (and requires 20min agent runtime)"
    return 0
  fi
  log "03a: SVG blocker — manual gate, requires 20min autonomous loop"
  record_result "03a" "skipped" "requires ~20min autonomous agent runtime; not in harness scope"
}

scenario_03b_morgan_pipeline() {
  if [ "$LLM_GATED" = false ]; then
    record_result "03b" "skipped" "--no-llm passed"
    return 0
  fi
  log "03b: asset-pipeline escalation — manual gate"
  record_result "03b" "skipped" "requires ~30min autonomous agent runtime; not in harness scope"
}

scenario_04a_sam_forkswap() {
  # 04a: config layer. Harness can verify the agent config files exist
  # and parse cleanly without restarting the broker.
  log "04a: agent config layer"
  local agentdir
  agentdir="$RUNTIME_HOME/.wuphf/team"
  if [ ! -d "$agentdir" ]; then
    record_result "04a" "fail" "$agentdir missing"
    return 1
  fi
  record_result "04a" "skipped" "agent JSON edit + restart is a manual CLI flow; harness verified state dir exists at $agentdir"
}

scenario_04b_morgan_pack() {
  log "04b: custom pack — CLI gate"
  record_result "04b" "skipped" "custom-pack flow is a CLI invocation; not in harness scope"
}

scenario_05a_alex_postmortem() {
  log "05a: Day-2 postmortem — requires 24h prior history"
  record_result "05a" "skipped" "requires 24h of prior session history; not in harness scope"
}

scenario_05b_jordan_recall() {
  log "05b: Day-2 recall — requires 24h prior history"
  record_result "05b" "skipped" "requires 24h of prior session history; not in harness scope"
}

# -----------------------------------------------------------------------------
# Driver

declare -a ALL_SCENARIOS=(
  scenario_01a_alex_first_install
  scenario_01b_jordan_pack
  scenario_02a_sam_onboarding
  scenario_02b_riley_buildflag
  scenario_03a_alex_svgblocker
  scenario_03b_morgan_pipeline
  scenario_04a_sam_forkswap
  scenario_04b_morgan_pack
  scenario_05a_alex_postmortem
  scenario_05b_jordan_recall
)

build_binary
boot_wuphf

FAIL_COUNT=0
for fn in "${ALL_SCENARIOS[@]}"; do
  # extract the short scenario id (e.g. "01a") from the function name
  id="$(echo "$fn" | awk -F'_' '{print $2}')"
  if [ -n "$SCENARIOS" ] && ! echo ",$SCENARIOS," | grep -q ",$id,"; then
    log "skipping $id (not in --scenarios)"
    continue
  fi
  if ! $fn; then
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi
done

# -----------------------------------------------------------------------------
# Report

log "writing report to $REPORT_DIR"
{
  echo "# ICP tutorial harness report"
  echo
  echo "- timestamp: $TS"
  echo "- binary: $WUPHF_BINARY"
  echo "- runtime: $RUNTIME_HOME"
  echo "- LLM-gated: $LLM_GATED"
  echo
  echo "| scenario | status | notes |"
  echo "|---|---|---|"
  jq -r '.[] | "| \(.scenario) | \(.status) | \(.notes) |"' "$RESULTS_JSON"
  echo
  echo "Failures: $FAIL_COUNT"
} > "$RESULTS_MD"

cat "$RESULTS_MD"
exit "$FAIL_COUNT"
