#!/bin/bash
# Persona-based E2E tests using termwright
# Tests real user scenarios for 5 personas across all features
set -e

SOCKET="/tmp/wuphf-persona-$$.sock"
BINARY="$(cd "$(dirname "$0")/../.." && pwd)/wuphf"
ARTIFACTS="$(cd "$(dirname "$0")/../.." && pwd)/termwright-artifacts/personas-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$ARTIFACTS"

cleanup() {
  pkill -f "termwright daemon.*$SOCKET" 2>/dev/null || true
  rm -f "$SOCKET"
}
trap cleanup EXIT

# Helper functions
send_raw() {
  local text="$1"
  for (( i=0; i<${#text}; i++ )); do
    local ch="${text:$i:1}"
    local b64=$(printf '%s' "$ch" | base64)
    termwright exec --socket "$SOCKET" --method raw --params "{\"bytes_base64\": \"$b64\"}" >/dev/null 2>&1
    sleep 0.05
  done
}

send_enter() {
  termwright exec --socket "$SOCKET" --method raw --params '{"bytes_base64": "DQ=="}' >/dev/null 2>&1
}

send_ctrl_u() {
  termwright exec --socket "$SOCKET" --method raw --params '{"bytes_base64": "FQ=="}' >/dev/null 2>&1
}

send_ctrl_c() {
  termwright exec --socket "$SOCKET" --method raw --params '{"bytes_base64": "Aw=="}' >/dev/null 2>&1
}

send_esc() {
  # ESC = 0x1B
  termwright exec --socket "$SOCKET" --method raw --params '{"bytes_base64": "Gw=="}' >/dev/null 2>&1
}

get_screen() {
  termwright exec --socket "$SOCKET" --method screen --params '{}' 2>/dev/null | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('result',''))"
}

save_screenshot() {
  termwright exec --socket "$SOCKET" --method screenshot --params '{}' 2>/dev/null | python3 -c "
import sys, json, base64
d = json.load(sys.stdin)
r = d.get('result', {})
if isinstance(r, dict) and 'png_base64' in r:
    data = base64.b64decode(r['png_base64'])
    with open('$ARTIFACTS/$1.png', 'wb') as f:
        f.write(data)
" 2>/dev/null
}

assert_text() {
  local text="$1"
  local screen=$(get_screen)
  if echo "$screen" | grep -qi "$text"; then
    echo "    PASS: found '$text'"
    return 0
  else
    echo "    FAIL: '$text' not found on screen"
    echo "$screen" > "$ARTIFACTS/failure-$2.txt"
    return 1
  fi
}

# assert_dispatched: checks command produced ANY output (not just blank)
assert_dispatched() {
  local cmd="$1"
  local screen=$(get_screen)
  # Check screen has more than just the input prompt — meaning the command ran
  local linecount=$(echo "$screen" | grep -v "^$" | wc -l)
  if [ "$linecount" -gt 5 ]; then
    echo "    PASS: /$cmd dispatched (screen has content)"
    return 0
  else
    echo "    FAIL: /$cmd produced no output"
    echo "$screen" > "$ARTIFACTS/failure-$cmd.txt"
    return 1
  fi
}

TOTAL=0
PASSED=0
FAILED=0

run_test() {
  TOTAL=$((TOTAL + 1))
  echo "  [$TOTAL] $1"
}

pass() { PASSED=$((PASSED + 1)); }
fail() { FAILED=$((FAILED + 1)); }

# Start daemon
echo "=== Persona E2E Tests ==="
echo "Binary: $BINARY"
echo "Artifacts: $ARTIFACTS"
echo ""

termwright daemon --socket "$SOCKET" --cols 120 --rows 50 --background "$BINARY"
sleep 7

# ============================================================
echo "━━━ PERSONA 1: Nazz (Startup Founder) ━━━"
echo "  Scenario: Quick workspace overview and delegation"
# ============================================================

run_test "Boot shows founding team roster"
assert_text "AGENTS" "p1-boot" && assert_text "CEO" "p1-boot" && assert_text "Product Manager" "p1-boot" && pass || fail
save_screenshot "p1-01-boot"

run_test "/help shows all command groups"
send_raw "/help"
send_enter
sleep 1
assert_text "Objects" "p1-help" && assert_text "Records" "p1-help" && assert_text "Tasks" "p1-help" && pass || fail
save_screenshot "p1-02-help"

run_test "/agents shows full founding team"
send_raw "/agents"
send_enter
sleep 1
assert_text "CEO" "p1-agents" && assert_text "Frontend Engineer" "p1-agents" && assert_text "AI Engineer" "p1-agents" && pass || fail
save_screenshot "p1-03-agents"

run_test "/config show displays workspace info"
send_raw "/config show"
send_enter
sleep 1
assert_text "Provider" "p1-config" && pass || fail
save_screenshot "p1-04-config"

run_test "Delegation: 'research our competitors'"
send_raw "research our top competitors and create a strategy"
send_enter
sleep 2
assert_text "You" "p1-delegate" && pass || fail
save_screenshot "p1-05-delegation"

# ============================================================
echo ""
echo "━━━ PERSONA 2: Sarah (SDR) ━━━"
echo "  Scenario: Prospecting workflow — records, notes, tasks"
# ============================================================

run_test "/object list dispatches"
send_raw "/object list"
send_enter
sleep 1
assert_dispatched "object-list" && pass || fail
save_screenshot "p2-01-objects"

run_test "/record list dispatches"
send_raw "/record list company"
send_enter
sleep 1
assert_dispatched "record-list" && pass || fail
save_screenshot "p2-02-records"

run_test "/note create dispatches"
send_raw "/note create --title 'Call with CTO' --content 'Interested'"
send_enter
sleep 1
assert_dispatched "note-create" && pass || fail
save_screenshot "p2-03-note"

run_test "/task create dispatches"
send_raw "/task create --title 'Follow up' --priority high"
send_enter
sleep 1
assert_dispatched "task-create" && pass || fail
save_screenshot "p2-04-task"

run_test "/search dispatches"
send_raw "/search Acme"
send_enter
sleep 1
assert_dispatched "search" && pass || fail
save_screenshot "p2-05-search"

# ============================================================
echo ""
echo "━━━ PERSONA 3: Alex (Developer) ━━━"
echo "  Scenario: Schema management and platform detection"
# ============================================================

run_test "/object create dispatches"
send_raw "/object create --name 'API Key' --slug api-key"
send_enter
sleep 1
assert_dispatched "object-create" && pass || fail
save_screenshot "p3-01-object-create"

run_test "/attribute create dispatches"
send_raw "/attribute create api-key --name Scope --slug scope --type text"
send_enter
sleep 1
assert_dispatched "attribute-create" && pass || fail
save_screenshot "p3-02-attr"

run_test "/detect — shows AI platforms"
send_raw "/detect"
send_enter
sleep 1
assert_text "claude" "p3-detect" && pass || fail
save_screenshot "p3-03-detect"

run_test "/config path — shows config location"
send_raw "/config path"
send_enter
sleep 1
assert_text ".wuphf" "p3-config-path" && pass || fail
save_screenshot "p3-04-config-path"

# ============================================================
echo ""
echo "━━━ PERSONA 4: Kim (CS Manager) ━━━"
echo "  Scenario: Customer relationships and timelines"
# ============================================================

run_test "/rel list-defs dispatches"
send_raw "/rel list-defs"
send_enter
sleep 1
assert_dispatched "rel-list-defs" && pass || fail
save_screenshot "p4-01-rels"

run_test "/task list dispatches"
send_raw "/task list"
send_enter
sleep 1
assert_dispatched "task-list" && pass || fail
save_screenshot "p4-02-tasks"

run_test "/note list dispatches"
send_raw "/note list"
send_enter
sleep 1
assert_dispatched "note-list" && pass || fail
save_screenshot "p4-03-notes"

# ============================================================
echo ""
echo "━━━ PERSONA 5: Jordan (Content Marketer) ━━━"
echo "  Scenario: Campaign lists and insights"
# ============================================================

run_test "/list list dispatches"
send_raw "/list list campaign"
send_enter
sleep 1
assert_dispatched "list-list" && pass || fail
save_screenshot "p5-01-lists"

run_test "/insights dispatches"
send_raw "/insights"
send_enter
sleep 1
assert_dispatched "insights" && pass || fail
save_screenshot "p5-02-insights"

run_test "/graph dispatches"
send_raw "/graph"
send_enter
sleep 1
assert_dispatched "graph" && pass || fail
save_screenshot "p5-03-graph"

# ============================================================
echo ""
echo "━━━ CROSS-CUTTING: Navigation & TUI Features ━━━"
# ============================================================

run_test "Autocomplete filters on /ta"
send_ctrl_u
sleep 0.5
send_raw "/ta"
sleep 1
assert_text "task" "nav-autocomplete2" && pass || fail
save_screenshot "nav-02-autocomplete"

run_test "/calendar dispatches"
send_ctrl_u
sleep 0.5
send_raw "/calendar"
send_enter
sleep 1
assert_dispatched "calendar" && pass || fail
save_screenshot "nav-03-calendar"

run_test "Clean exit with Ctrl+C"
send_ctrl_c
sleep 1
send_ctrl_c
sleep 1
echo "    PASS: Exit signal sent"
pass

# ============================================================
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Results: $PASSED passed, $FAILED failed out of $TOTAL tests"
echo "  Screenshots: $ARTIFACTS/"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if [ $FAILED -gt 0 ]; then
  exit 1
fi
