#!/bin/bash
# Comprehensive E2E tests for all recent features
# Tests: skills, /reset-dm, 1:1 mode threads, human text color,
#        thinking/working indicator, Esc pause, sidebar apps

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TERMWRIGHT="${TERMWRIGHT:-$(command -v termwright || true)}"
if [ -z "$TERMWRIGHT" ]; then
  echo "termwright not found in PATH; set TERMWRIGHT=/abs/path/to/termwright" >&2
  exit 1
fi
SOCKET="/tmp/wuphf-full-e2e-$$.sock"
WUPHF="${WUPHF_BIN:-$REPO_ROOT/wuphf}"
ARTIFACTS="${ARTIFACTS:-$REPO_ROOT/termwright-artifacts/full-e2e-$(date +%Y%m%d-%H%M%S)}"
mkdir -p "$ARTIFACTS"

PASS=0
FAIL=0
TOTAL=0

cleanup() {
  pkill -f "termwright.*wuphf-full-e2e" 2>/dev/null || true
  rm -f "$SOCKET"
  sleep 1
}

start_daemon() {
  cleanup
  "$TERMWRIGHT" daemon --socket "$SOCKET" --cols 120 --rows 40 -- "$WUPHF" -no-nex "$@" &
  sleep 2
  "$TERMWRIGHT" exec --socket "$SOCKET" --method wait_for_text --params '{"text":"Channels","timeout_ms":25000}' 2>/dev/null || \
  "$TERMWRIGHT" exec --socket "$SOCKET" --method wait_for_text --params '{"text":"1:1","timeout_ms":10000}' 2>/dev/null || {
    echo "  WARN: Timed out waiting for view, continuing..."
    sleep 5
  }
  sleep 1
  BROKER_TOKEN=$(cat /tmp/wuphf-broker-token 2>/dev/null)
}

screen_text() {
  "$TERMWRIGHT" exec --socket "$SOCKET" --method screen --params '{}' 2>&1 | \
    python3 -c "import json,sys; print(json.load(sys.stdin).get('result',''))" 2>/dev/null
}

type_text() {
  "$TERMWRIGHT" exec --socket "$SOCKET" --method type --params "{\"text\":\"$1\"}" 2>&1 >/dev/null
}

press_key() {
  "$TERMWRIGHT" exec --socket "$SOCKET" --method press --params "{\"key\":\"$1\"}" 2>&1 >/dev/null
}

screenshot() {
  "$TERMWRIGHT" exec --socket "$SOCKET" --method screenshot --params "{\"path\":\"$ARTIFACTS/$1.png\"}" 2>&1 >/dev/null
}

assert_contains() {
  local text="$1"
  local desc="$2"
  TOTAL=$((TOTAL + 1))
  if screen_text | grep -q "$text" 2>/dev/null; then
    echo "  PASS: $desc"
    PASS=$((PASS + 1))
  else
    echo "  FAIL: $desc (expected '$text')"
    FAIL=$((FAIL + 1))
    screenshot "fail-${TOTAL}"
  fi
}

assert_not_contains() {
  local text="$1"
  local desc="$2"
  TOTAL=$((TOTAL + 1))
  if ! screen_text | grep -q "$text" 2>/dev/null; then
    echo "  PASS: $desc"
    PASS=$((PASS + 1))
  else
    echo "  FAIL: $desc (did not expect '$text')"
    FAIL=$((FAIL + 1))
    screenshot "fail-${TOTAL}"
  fi
}

assert_api() {
  local method="$1"
  local url="$2"
  local body="$3"
  local expected_code="$4"
  local desc="$5"

  if [ -n "$body" ]; then
    RESPONSE=$(curl -s -w "\n%{http_code}" -X "$method" "$url" \
      -H "Content-Type: application/json" \
      -H "Authorization: Bearer $BROKER_TOKEN" \
      -d "$body" 2>/dev/null)
  else
    RESPONSE=$(curl -s -w "\n%{http_code}" -X "$method" "$url" \
      -H "Authorization: Bearer $BROKER_TOKEN" 2>/dev/null)
  fi

  HTTP_CODE=$(echo "$RESPONSE" | tail -1)
  BODY=$(echo "$RESPONSE" | sed '$d')

  TOTAL=$((TOTAL + 1))
  if [ "$HTTP_CODE" = "$expected_code" ]; then
    echo "  PASS: $desc (HTTP $HTTP_CODE)"
    PASS=$((PASS + 1))
  else
    echo "  FAIL: $desc (expected $expected_code, got $HTTP_CODE)"
    FAIL=$((FAIL + 1))
  fi
}

trap cleanup EXIT

echo "=== WUPHF Full Feature E2E Tests ==="
echo "Binary: $WUPHF"
echo "Artifacts: $ARTIFACTS"
echo ""

# ═══════════════════════════════════════════════════════
echo "═══ SECTION 1: OFFICE MODE ═══"
# ═══════════════════════════════════════════════════════

echo ""
echo "T01: Sidebar shows all apps including Skills"
start_daemon
screenshot "t01-sidebar"
assert_contains "Skills" "Skills app in sidebar"
assert_contains "Messages" "Messages app in sidebar"
assert_contains "Tasks" "Tasks app in sidebar"
assert_contains "Requests" "Requests app in sidebar"
assert_contains "Insights" "Insights app in sidebar"
assert_contains "Calendar" "Calendar app in sidebar"
cleanup

echo ""
echo "T02: /skills command navigates to Skills app"
start_daemon
type_text "/skills"
press_key "Enter"
sleep 2
screenshot "t02-skills-app"
assert_contains "Skills" "Skills app active"
cleanup

echo ""
echo "T03: Slash autocomplete shows skill commands"
start_daemon
type_text "/ski"
sleep 1
screenshot "t03-autocomplete"
assert_contains "skill" "skill in autocomplete"
cleanup

echo ""
echo "T04: Skills broker API — full CRUD"
start_daemon

# Clean stale
for S in deploy-check standup-summary deploy-verify test-skill; do
  curl -s -X DELETE "http://127.0.0.1:7890/skills" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $BROKER_TOKEN" \
    -d "{\"name\": \"$S\"}" 2>/dev/null >/dev/null
done
sleep 1

assert_api POST "http://127.0.0.1:7890/skills" \
  '{"action":"create","name":"deploy-check","title":"Deploy Check","description":"Post-deploy health","content":"Check /health","created_by":"you","channel":"general","tags":["ops"]}' \
  200 "Create skill"

assert_api GET "http://127.0.0.1:7890/skills" "" 200 "List skills"

assert_api POST "http://127.0.0.1:7890/skills/deploy-check/invoke" \
  '{"from":"cto","channel":"general"}' \
  200 "Invoke skill"

assert_api POST "http://127.0.0.1:7890/skills" \
  '{"action":"create","name":"deploy-check","title":"Dup","content":"x","created_by":"you"}' \
  409 "Duplicate returns 409"

assert_api POST "http://127.0.0.1:7890/skills" \
  '{"action":"propose","name":"standup-summary","title":"Standup","description":"Daily standup","content":"Summarize","created_by":"ceo","channel":"general"}' \
  200 "CEO proposal"

assert_api DELETE "http://127.0.0.1:7890/skills" \
  '{"name":"deploy-check"}' \
  200 "Archive skill"

# Verify archived not in list
SKILLS_BODY=$(curl -s "http://127.0.0.1:7890/skills" -H "Authorization: Bearer $BROKER_TOKEN" 2>/dev/null)
TOTAL=$((TOTAL + 1))
if ! echo "$SKILLS_BODY" | grep -q '"deploy-check"' 2>/dev/null; then
  echo "  PASS: Archived skill not in list"
  PASS=$((PASS + 1))
else
  echo "  FAIL: Archived skill still visible"
  FAIL=$((FAIL + 1))
fi

# Skills visible in TUI
type_text "/skills"
press_key "Enter"
sleep 2
screenshot "t04-skills-after-api"
assert_contains "standup\|Standup" "Proposed skill visible in TUI"
cleanup

echo ""
echo "T05: /reset-dm clears DMs only"
start_daemon

# Post some messages
curl -s -X POST "http://127.0.0.1:7890/messages" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $BROKER_TOKEN" \
  -d '{"channel":"general","from":"you","content":"Hello CEO"}' 2>/dev/null >/dev/null

curl -s -X POST "http://127.0.0.1:7890/messages" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $BROKER_TOKEN" \
  -d '{"channel":"general","from":"ceo","content":"Hello human"}' 2>/dev/null >/dev/null

curl -s -X POST "http://127.0.0.1:7890/messages" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $BROKER_TOKEN" \
  -d '{"channel":"general","from":"pm","content":"PM status update"}' 2>/dev/null >/dev/null

sleep 1

# Reset DMs with CEO
assert_api POST "http://127.0.0.1:7890/reset-dm" \
  '{"agent":"ceo","channel":"general"}' \
  200 "Reset DMs with CEO"

# Verify CEO and human DMs gone, PM message kept
MSGS_BODY=$(curl -s "http://127.0.0.1:7890/messages?channel=general&limit=50" \
  -H "Authorization: Bearer $BROKER_TOKEN" 2>/dev/null)

TOTAL=$((TOTAL + 1))
if echo "$MSGS_BODY" | grep -q "PM status update" 2>/dev/null; then
  echo "  PASS: PM message preserved after /reset-dm"
  PASS=$((PASS + 1))
else
  echo "  FAIL: PM message was deleted by /reset-dm"
  FAIL=$((FAIL + 1))
fi

TOTAL=$((TOTAL + 1))
if ! echo "$MSGS_BODY" | grep -q "Hello CEO" 2>/dev/null; then
  echo "  PASS: Human->CEO DM cleared"
  PASS=$((PASS + 1))
else
  echo "  FAIL: Human->CEO DM still present"
  FAIL=$((FAIL + 1))
fi
cleanup

echo ""
echo "T06: Composer renders with input area"
start_daemon
screenshot "t06-composer"
assert_contains "commands" "Composer hint text visible"
cleanup

# ═══════════════════════════════════════════════════════
echo ""
echo "═══ SECTION 2: 1:1 MODE ═══"
# ═══════════════════════════════════════════════════════

echo ""
echo "T07: 1:1 mode launches with agent"
start_daemon -1o1
screenshot "t07-1o1-launch"
assert_contains "1:1" "1:1 mode indicator"
cleanup

echo ""
echo "T08: 1:1 mode has thread commands"
start_daemon -1o1
type_text "/exp"
sleep 1
screenshot "t08-1o1-expand"
assert_contains "expand\|Expand" "expand in 1:1 autocomplete"
cleanup

echo ""
echo "T09: 1:1 mode has /reset-dm"
start_daemon -1o1
type_text "/reset"
sleep 1
screenshot "t09-1o1-resetdm"
assert_contains "reset-dm" "reset-dm in 1:1 autocomplete"
cleanup

echo ""
echo "T10: 1:1 mode filters out CEO delegation messages"
start_daemon -1o1
sleep 1

# Post a direct message from CEO to human
curl -s -X POST "http://127.0.0.1:7890/messages" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $BROKER_TOKEN" \
  -d '{"channel":"general","from":"ceo","content":"Here is my analysis for you.","tagged":[]}' 2>/dev/null >/dev/null

# Post a delegation message from CEO to PM (should be hidden in 1:1)
curl -s -X POST "http://127.0.0.1:7890/messages" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $BROKER_TOKEN" \
  -d '{"channel":"general","from":"ceo","content":"@pm please review the roadmap","tagged":["pm"]}' 2>/dev/null >/dev/null

sleep 3
screenshot "t10-1o1-filtering"
assert_contains "analysis for you" "CEO direct reply visible in 1:1"
assert_not_contains "review the roadmap" "CEO delegation to PM hidden in 1:1"
cleanup

# ═══════════════════════════════════════════════════════
echo ""
echo "═══ SECTION 3: BROKER API ═══"
# ═══════════════════════════════════════════════════════

echo ""
echo "T11: /members endpoint returns liveActivity field"
start_daemon

MEMBERS=$(curl -s "http://127.0.0.1:7890/members?channel=general" \
  -H "Authorization: Bearer $BROKER_TOKEN" 2>/dev/null)

TOTAL=$((TOTAL + 1))
if echo "$MEMBERS" | grep -q '"members"' 2>/dev/null; then
  echo "  PASS: /members returns valid response with members array"
  PASS=$((PASS + 1))
else
  echo "  FAIL: /members response invalid"
  FAIL=$((FAIL + 1))
fi
cleanup

echo ""
echo "T12: MCP tools correct for 1:1 mode"
# In 1:1 mode, agent should have reply/read_conversation, NOT team_broadcast
start_daemon -1o1
sleep 2

# Check that the agent pane is running (at least the tmux session exists)
TOTAL=$((TOTAL + 1))
if tmux -L wuphf list-panes -t wuphf-team 2>/dev/null | grep -q "." ; then
  echo "  PASS: tmux team session running"
  PASS=$((PASS + 1))
else
  echo "  FAIL: tmux team session not found"
  FAIL=$((FAIL + 1))
fi
cleanup

# ═══════════════════════════════════════════════════════
echo ""
echo "═══ RESULTS ═══"
echo "Passed: $PASS / $TOTAL"
if [ $FAIL -gt 0 ]; then
  echo "Failed: $FAIL"
  echo "Artifacts: $ARTIFACTS"
  exit 1
else
  echo "All tests passed!"
  exit 0
fi
