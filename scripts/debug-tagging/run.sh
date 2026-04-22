#!/usr/bin/env bash
# Isolated reproduction rig for the "tagging any specialist apart from CEO
# drops silently" bug. Sets up a clean HOME, fresh broker on non-default
# ports, pre-seeds the onboarding state to skip the wizard, posts a tagged
# message, and reports whether the specialist's headless queue was woken.
#
# Usage:
#   ./run.sh                   # default: pack=founding-team, specialist=pm
#   SPECIALIST=fe ./run.sh     # tag a different specialist
#   PACK=starter ./run.sh      # different pack (roster must include SPECIALIST)
#   KEEP=1 ./run.sh            # leave the server running after the test
#   MODE=focus ./run.sh        # focus mode instead of collaborative
#
# The rig is deliberately hostile to hidden global state: it uses its own HOME,
# its own broker port, and its own log directory. Nothing about this script
# touches your real ~/.wuphf state.

set -euo pipefail

# --- Config -----------------------------------------------------------------

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BINARY="${BINARY:-$REPO_ROOT/.wuphf-debug-tagging}"
SANDBOX_HOME="${SANDBOX_HOME:-/tmp/wuphf-debug-tagging-home}"
BROKER_PORT="${BROKER_PORT:-7899}"
WEB_PORT="${WEB_PORT:-7900}"
PACK="${PACK:-founding-team}"
SPECIALIST="${SPECIALIST:-pm}"
MODE="${MODE:-collab}"      # collab | focus
KEEP="${KEEP:-0}"
WAIT_SECS="${WAIT_SECS:-4}" # how long to wait for the queue to fire

# Wizard-hire scenario: hire a new agent via POST /office-members AFTER the
# server starts, then tag them. This is the path PR #218 fixed for
# activeSessionMembers — wizard-hired agents were excluded from
# agentNotificationTargets. Set HIRE_SLUG to any new slug not in the pack.
HIRE_SLUG="${HIRE_SLUG:-}"

BROKER_URL="http://127.0.0.1:${BROKER_PORT}"
WEB_URL="http://127.0.0.1:${WEB_PORT}"
LOG_DIR="${SANDBOX_HOME}/.wuphf/logs"
TOKEN_FILE="/tmp/wuphf-broker-token-${BROKER_PORT}"
SERVER_LOG="${SANDBOX_HOME}/server.log"

# --- Helpers ----------------------------------------------------------------

log()  { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }
fail() { log "FAIL: $*"; exit 1; }

# --- Step 1: build --------------------------------------------------------

if [ ! -x "$BINARY" ] || [ "${BUILD:-1}" = "1" ]; then
  log "build: compiling wuphf to $BINARY"
  (cd "$REPO_ROOT" && go build -buildvcs=false -o "$BINARY" ./cmd/wuphf)
fi

# --- Step 2: kill anything on our ports ---------------------------------

kill_port() {
  local port=$1
  local pids
  pids=$(lsof -ti :"$port" 2>/dev/null || true)
  if [ -n "$pids" ]; then
    log "kill: pid(s) on :$port -> $pids"
    kill $pids 2>/dev/null || true
    sleep 0.3
    kill -9 $pids 2>/dev/null || true
  fi
}

kill_port "$BROKER_PORT"
kill_port "$WEB_PORT"

# --- Step 3: fresh sandbox HOME with skip-onboarding state ----------------

rm -rf "$SANDBOX_HOME"
mkdir -p "$SANDBOX_HOME/.wuphf"

# Seed a completed onboarding state so the wizard is skipped.
# State schema is in internal/onboarding/state.go — keep in sync.
cat > "$SANDBOX_HOME/.wuphf/onboarded.json" <<JSON
{
  "completed_at": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
  "version": 1,
  "company_name": "Debug Tagging Test Co",
  "completed_steps": ["welcome", "pick_team", "first_task"],
  "checklist_dismissed": true,
  "checklist": []
}
JSON

# Seed a minimal config so the launcher knows which pack we chose.
# `blueprint` is the primary field read by cfg.ActiveBlueprint(); `pack`
# is the legacy alias. We pass --pack on the CLI too so this is belt+braces.
cat > "$SANDBOX_HOME/.wuphf/config.json" <<JSON
{
  "blueprint": "$PACK",
  "pack": "$PACK",
  "memory_backend": "markdown"
}
JSON

# Fake a "claude" binary so PreflightWeb passes. We don't actually want the
# LLM to run — we only want to observe whether the specialist's headless
# queue got an enqueue. The fake binary just exits 0, leaving a log entry
# via appendHeadlessCodexLog that we can grep.
FAKE_BIN_DIR="$SANDBOX_HOME/fake-bin"
mkdir -p "$FAKE_BIN_DIR"
cat > "$FAKE_BIN_DIR/claude" <<'SH'
#!/usr/bin/env bash
# Fake claude — the real one is not needed for this test. We want to observe
# WHICH agent got its turn dispatched, not run an actual LLM round-trip.
# stream-json reply that the broker can parse as "no message, done".
cat <<'JSON'
{"type":"result","result":"(debug-tagging-fake-claude) turn received"}
JSON
exit 0
SH
chmod +x "$FAKE_BIN_DIR/claude"

# Fake codex too, in case pack or per-agent provider is configured for codex.
cat > "$FAKE_BIN_DIR/codex" <<'SH'
#!/usr/bin/env bash
echo "(debug-tagging-fake-codex) turn received"
exit 0
SH
chmod +x "$FAKE_BIN_DIR/codex"

log "sandbox: HOME=$SANDBOX_HOME"
log "sandbox: broker=$BROKER_URL web=$WEB_URL pack=$PACK specialist=$SPECIALIST mode=$MODE"

# --- Step 4: start the server --------------------------------------------

MODE_FLAG=""
if [ "$MODE" = "collab" ]; then
  MODE_FLAG="--collab"
fi

# Isolate: custom HOME, custom PATH with fake bins first, no Nex, no browser.
env \
  HOME="$SANDBOX_HOME" \
  PATH="$FAKE_BIN_DIR:$PATH" \
  WUPHF_BROKER_PORT="$BROKER_PORT" \
  WUPHF_NO_NEX=1 \
  "$BINARY" \
    --pack "$PACK" \
    --web-port "$WEB_PORT" \
    --no-open \
    --no-nex \
    $MODE_FLAG \
  > "$SERVER_LOG" 2>&1 &

SERVER_PID=$!
log "server: started pid=$SERVER_PID (log=$SERVER_LOG)"

cleanup() {
  if [ "$KEEP" != "1" ]; then
    log "cleanup: killing pid=$SERVER_PID"
    kill "$SERVER_PID" 2>/dev/null || true
    sleep 0.3
    kill -9 "$SERVER_PID" 2>/dev/null || true
  else
    log "cleanup: KEEP=1 — leaving server running on pid=$SERVER_PID"
    log "cleanup: logs at $LOG_DIR  server-log at $SERVER_LOG"
    log "cleanup: kill manually with: kill $SERVER_PID"
  fi
}
trap cleanup EXIT

# --- Step 5: wait for broker readiness ------------------------------------

log "wait: broker /health up"
for i in $(seq 1 50); do
  if curl -sf "${BROKER_URL}/health" >/dev/null 2>&1; then
    log "ready: broker up after ${i}00ms"
    break
  fi
  sleep 0.1
  if [ "$i" = "50" ]; then
    log "server log tail:"
    tail -50 "$SERVER_LOG" >&2
    fail "broker did not come up on $BROKER_URL within 5s"
  fi
done

# Token is written on broker start. ResolveTokenFile appends the port when
# non-default, so for BROKER_PORT=7899 it's /tmp/wuphf-broker-token-7899.
for i in $(seq 1 20); do
  if [ -s "$TOKEN_FILE" ]; then
    break
  fi
  sleep 0.1
done
[ -s "$TOKEN_FILE" ] || fail "no broker token at $TOKEN_FILE"
TOKEN="$(cat "$TOKEN_FILE")"
AUTH="Authorization: Bearer $TOKEN"
log "auth: token loaded from $TOKEN_FILE"

# --- Step 6: inspect initial roster ---------------------------------------

log "probe: office members"
curl -sf -H "$AUTH" "${BROKER_URL}/office-members" > "$SANDBOX_HOME/.members.json"
SANDBOX_HOME="$SANDBOX_HOME" python3 <<'PY' >&2
import json, os
with open(os.environ["SANDBOX_HOME"] + "/.members.json") as f:
    data = json.load(f)
for m in data.get("members", []):
    slug = m.get("slug") or ""
    name = m.get("name") or ""
    role = m.get("role") or ""
    print("  - {:<14} name={!r:<30} role={!r}".format(slug, name, role))
PY

# If HIRE_SLUG is set, hire that agent via POST /office-members (wizard path)
# and switch SPECIALIST to it. This reproduces the exact path from PR #218:
# agents added AFTER launch that were silently excluded from agentNotificationTargets.
if [ -n "$HIRE_SLUG" ]; then
  log "hire: POST /office-members action=create slug=$HIRE_SLUG (wizard path)"
  HIRE_PAYLOAD=$(HIRE_SLUG="$HIRE_SLUG" python3 <<'PY'
import json, os
print(json.dumps({
    "action": "create",
    "slug": os.environ["HIRE_SLUG"],
    "name": os.environ["HIRE_SLUG"].upper() + " (wizard-hired)",
    "role": "Wizard-hired specialist",
    "expertise": ["testing"],
    "personality": "Added after launch via POST /office-members, not in pack",
    "permission_mode": "plan",
}))
PY
)
  curl -sf -X POST -H "$AUTH" -H "Content-Type: application/json" \
    -d "$HIRE_PAYLOAD" "${BROKER_URL}/office-members" >/dev/null \
    || fail "hire: POST /office-members failed"
  SPECIALIST="$HIRE_SLUG"
  log "hire: SPECIALIST is now '$SPECIALIST' (wizard-hired, not in pack)"

  # Refresh roster listing so the check below sees the new agent.
  curl -sf -H "$AUTH" "${BROKER_URL}/office-members" > "$SANDBOX_HOME/.members.json"
fi

# Check that SPECIALIST exists in the roster.
HAS_SPECIALIST=$(SPECIALIST="$SPECIALIST" SANDBOX_HOME="$SANDBOX_HOME" python3 <<'PY'
import json, os
with open(os.environ["SANDBOX_HOME"] + "/.members.json") as f:
    data = json.load(f)
slugs = [m.get("slug") for m in data.get("members", [])]
print("yes" if os.environ["SPECIALIST"] in slugs else "no")
PY
)
if [ "$HAS_SPECIALIST" != "yes" ]; then
  fail "pack=$PACK roster does not include $SPECIALIST — pick a different SPECIALIST or PACK"
fi

# --- Step 7: post a tagged message ----------------------------------------

log "post: @${SPECIALIST} message to #general"
MSG_PAYLOAD=$(SPECIALIST="$SPECIALIST" python3 <<'PY'
import json, os
s = os.environ["SPECIALIST"]
print(json.dumps({
    "from": "you",
    "channel": "general",
    "content": "@" + s + " debug-tagging-rig wants you to acknowledge this message",
    "tagged": [s],
}))
PY
)

POST_RESP=$(curl -sf -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d "$MSG_PAYLOAD" "${BROKER_URL}/messages")
MSG_ID=$(printf '%s' "$POST_RESP" | python3 -c "import json,sys; print(json.load(sys.stdin).get('id',''))")
log "post: accepted id=$MSG_ID resp=$POST_RESP"

# --- Step 8: wait for dispatch --------------------------------------------

log "wait: ${WAIT_SECS}s for headless queue to fire for $SPECIALIST"
sleep "$WAIT_SECS"

# --- Step 9: diagnostics ---------------------------------------------------

echo
echo "========================================================================"
echo "DIAGNOSTIC RESULTS"
echo "========================================================================"

# 9a) broker-stored message — did Tagged get normalized correctly?
MESSAGES_JSON=$(curl -sf -H "$AUTH" "${BROKER_URL}/messages?channel=general")
printf '%s' "$MESSAGES_JSON" > "$SANDBOX_HOME/.messages.json"
STORED_TAGGED=$(MSG_ID="$MSG_ID" SANDBOX_HOME="$SANDBOX_HOME" python3 <<'PY'
import json, os
data = json.load(open(os.environ["SANDBOX_HOME"] + "/.messages.json"))
for m in data.get("messages", []):
    if m.get("id") == os.environ["MSG_ID"]:
        print("tagged={} from={} content={}".format(
            json.dumps(m.get("tagged", [])), m.get("from"), repr((m.get("content") or ""))[:120]))
        break
PY
)
echo "[1] broker message row: $STORED_TAGGED"

# 9b) Log files in sandbox
echo
echo "[2] log dir contents ($LOG_DIR):"
if [ -d "$LOG_DIR" ]; then
  ls -la "$LOG_DIR" | sed 's/^/    /'
else
  echo "    (log dir does not exist — NO turns were dispatched to anyone)"
fi

# 9c) Specialist log
echo
SPECIALIST_CLAUDE_LOG="$LOG_DIR/headless-claude-$SPECIALIST.log"
SPECIALIST_CODEX_LOG="$LOG_DIR/headless-codex-$SPECIALIST.log"
LATENCY_LOG="$LOG_DIR/headless-codex-latency.log"

specialist_dispatched=false
if [ -s "$SPECIALIST_CLAUDE_LOG" ] || [ -s "$SPECIALIST_CODEX_LOG" ]; then
  specialist_dispatched=true
fi
if [ -s "$LATENCY_LOG" ] && grep -q "agent=$SPECIALIST " "$LATENCY_LOG"; then
  specialist_dispatched=true
fi

echo "[3] $SPECIALIST log files:"
for f in "$SPECIALIST_CLAUDE_LOG" "$SPECIALIST_CODEX_LOG"; do
  if [ -s "$f" ]; then
    echo "    $f:"
    sed 's/^/      /' "$f"
  fi
done
if [ -s "$LATENCY_LOG" ]; then
  echo "    $LATENCY_LOG entries for $SPECIALIST:"
  grep "agent=$SPECIALIST " "$LATENCY_LOG" | sed 's/^/      /' || echo "      (none)"
fi

# 9d) CEO log — was the turn ALSO (mis)routed to CEO?
CEO_CLAUDE_LOG="$LOG_DIR/headless-claude-ceo.log"
CEO_CODEX_LOG="$LOG_DIR/headless-codex-ceo.log"
ceo_dispatched=false
if [ -s "$CEO_CLAUDE_LOG" ] || [ -s "$CEO_CODEX_LOG" ]; then
  ceo_dispatched=true
fi
if [ -s "$LATENCY_LOG" ] && grep -q "agent=ceo " "$LATENCY_LOG"; then
  ceo_dispatched=true
fi

# --- Step 10: verdict ------------------------------------------------------

echo
echo "========================================================================"
if $specialist_dispatched; then
  echo "RESULT: PASS — $SPECIALIST was dispatched a turn. Fix is working."
  echo "        (CEO dispatched=$ceo_dispatched — expected TRUE in collab mode)"
else
  echo "RESULT: FAIL — $SPECIALIST was NOT dispatched a turn. Bug reproduced."
  echo "        (CEO dispatched=$ceo_dispatched)"
fi
echo "========================================================================"
echo
echo "Server log tail: tail -50 $SERVER_LOG"
echo "Broker URL:      $BROKER_URL  (token in $TOKEN_FILE)"
echo "Web UI:          $WEB_URL"
echo

if $specialist_dispatched; then
  exit 0
else
  exit 1
fi
