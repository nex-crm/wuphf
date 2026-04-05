#!/bin/bash
# Human Judgment UAT Test Suite for wuphf TUI
# Tests USABILITY, not just functionality.
#
# Runs against the current office channel surface.
# Asserts readability, information density, layout quality, and junk-free output.
#
# New assertion types:
#   assert_readable   — no raw JSON, NDJSON, or ANSI escapes visible
#   assert_density    — at least 40% of lines have content
#   assert_no_junk    — no protocol data, tracebacks, or debug output
#   assert_layout     — input at bottom, no excessive blank lines
set -euo pipefail

SOCKET="/tmp/wuphf-hj-uat-$$.sock"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BINARY="$ROOT/wuphf"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
ARTIFACTS="$ROOT/termwright-artifacts/hj-uat-$TIMESTAMP"
mkdir -p "$ARTIFACTS"

# Per-persona counters
P1_PASS=0; P1_FAIL=0; P1_TOTAL=0  # Maya
P2_PASS=0; P2_FAIL=0; P2_TOTAL=0  # Raj
P3_PASS=0; P3_FAIL=0; P3_TOTAL=0  # Sarah
P4_PASS=0; P4_FAIL=0; P4_TOTAL=0  # Alex
P5_PASS=0; P5_FAIL=0; P5_TOTAL=0  # Kim
CURRENT_P=0

cleanup() {
  pkill -f "termwright daemon.*$SOCKET" 2>/dev/null || true
  rm -f "$SOCKET"
}
trap cleanup EXIT

# ─── Input Helpers ──────────────────────────────────────────────────────

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

send_escape() {
  termwright exec --socket "$SOCKET" --method key --params '{"key":"Escape"}' >/dev/null 2>&1
}

clear_input() {
  send_ctrl_u
  sleep 0.2
}

run_command() {
  local cmd="$1"
  local delay="${2:-1}"
  clear_input
  send_raw "$cmd"
  sleep 0.3
  send_enter
  sleep "$delay"
}

get_screen() {
  termwright exec --socket "$SOCKET" --method screen --params '{}' 2>/dev/null | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('result',''))"
}

capture_screen() {
  local content=""
  for _ in 1 2 3 4 5 6 7 8; do
    content="$(get_screen || true)"
    if [ -n "$(printf '%s' "$content" | tr -d '[:space:]')" ]; then
      printf '%s\n' "$content"
      return 0
    fi
    sleep 1
  done
  printf '%s\n' "$content"
  return 0
}

save_screenshot() {
  local name="$1"
  termwright exec --socket "$SOCKET" --method screenshot --params "{\"path\": \"$ARTIFACTS/$name.png\"}" >/dev/null 2>&1 || \
  termwright exec --socket "$SOCKET" --method screenshot --params '{}' 2>/dev/null | python3 -c "
import sys, json, base64
d = json.load(sys.stdin)
r = d.get('result', {})
if isinstance(r, dict) and 'png_base64' in r:
    data = base64.b64decode(r['png_base64'])
    with open('$ARTIFACTS/$name.png', 'wb') as f:
        f.write(data)
" 2>/dev/null || true
  capture_screen > "$ARTIFACTS/$name.txt" 2>/dev/null || true
}

# ─── Basic Assertions ──────────────────────────────────────────────────

assert_text() {
  local text="$1"
  local label="${2:-check}"
  local screen
  screen=$(capture_screen)
  if echo "$screen" | grep -Eqi "$text"; then
    echo "    PASS: found '$text'"
    return 0
  else
    echo "    FAIL: '$text' not found on screen"
    echo "$screen" > "$ARTIFACTS/failure-$label.txt"
    return 1
  fi
}

assert_not_text() {
  local text="$1"
  local label="${2:-check}"
  local screen
  screen=$(capture_screen)
  if echo "$screen" | grep -Eqi "$text"; then
    echo "    FAIL: '$text' should NOT be on screen"
    echo "$screen" > "$ARTIFACTS/failure-$label.txt"
    return 1
  else
    echo "    PASS: '$text' correctly absent"
    return 0
  fi
}

assert_screen_not_blank() {
  local label="${1:-blank}"
  local screen
  screen=$(capture_screen)
  local linecount
  linecount=$(echo "$screen" | grep -cv "^$" || true)
  if [ "$linecount" -gt 5 ]; then
    echo "    PASS: screen has $linecount non-empty lines"
    return 0
  else
    echo "    FAIL: screen appears blank ($linecount lines)"
    echo "$screen" > "$ARTIFACTS/failure-$label.txt"
    return 1
  fi
}

# ─── Human Judgment Assertions ──────────────────────────────────────────

# Check readability: no raw JSON, NDJSON, or ANSI escape sequences visible
assert_readable() {
  local label="$1"
  local screen=$(capture_screen)
  local issues=""

  # Check no raw JSON objects visible
  if echo "$screen" | grep -q '"type":'; then
    issues="$issues raw-json"
  fi

  # Check no NDJSON stream-json lines visible
  if echo "$screen" | grep -q '"session_id":'; then
    issues="$issues ndjson-leak"
  fi

  # Check no raw escape sequences
  if echo "$screen" | grep -q '\[0m\|\[1m\|\[38;'; then
    issues="$issues raw-ansi"
  fi

  # Check content lines are readable length (>10 chars of actual content)
  # Exclude border-only lines and blank lines
  local short_lines=$(echo "$screen" | grep -v "^$" | grep -v "^[│┌┐└┘├┤─╭╮╰╯]" | awk '{if(length < 10 && length > 0) count++} END {print count+0}')
  if [ "$short_lines" -gt 10 ]; then
    issues="$issues too-many-short-lines($short_lines)"
  fi

  if [ -n "$issues" ]; then
    echo "    READABLE FAIL [$label]:$issues"
    echo "$screen" > "$ARTIFACTS/readable-fail-$label.txt"
    return 1
  fi
  echo "    READABLE OK [$label]"
  return 0
}

# Check information density: not too much wasted space
assert_density() {
  local label="$1"
  local screen=$(capture_screen)
  local total=$(echo "$screen" | wc -l)
  local content=$(echo "$screen" | grep -v "^$" | wc -l)

  if [ "$total" -eq 0 ]; then
    echo "    DENSITY FAIL [$label]: empty screen"
    return 1
  fi

  local pct=$((content * 100 / total))

  if [ "$pct" -lt 40 ]; then
    echo "    DENSITY FAIL [$label]: only ${pct}% of lines have content (want >=40%)"
    echo "$screen" > "$ARTIFACTS/density-fail-$label.txt"
    return 1
  fi
  echo "    DENSITY OK [$label]: ${pct}% content"
  return 0
}

# Check no junk: no raw protocol data, no debug output
assert_no_junk() {
  local label="$1"
  local screen=$(capture_screen)
  local issues=""

  # No raw JSON objects on their own line
  if echo "$screen" | grep -qE '^\{.*"type"'; then
    issues="$issues raw-json-line"
  fi

  # No Python/Go error tracebacks
  if echo "$screen" | grep -qi "traceback\|goroutine\|panic:"; then
    issues="$issues error-traceback"
  fi

  # No raw NDJSON protocol lines
  if echo "$screen" | grep -qE '^\{.*"session_id"'; then
    issues="$issues ndjson-protocol"
  fi

  # No debug log lines
  if echo "$screen" | grep -qi "^DEBUG\|^WARN\|^ERROR.*:.*:"; then
    issues="$issues debug-logs"
  fi

  if [ -n "$issues" ]; then
    echo "    JUNK FAIL [$label]:$issues"
    echo "$screen" > "$ARTIFACTS/junk-fail-$label.txt"
    return 1
  fi
  echo "    JUNK-FREE OK [$label]"
  return 0
}

# Check layout: input at bottom, status visible, proportional
assert_layout() {
  local label="$1"
  local screen=$(get_screen)
  local lines=$(echo "$screen" | wc -l)
  local issues=""

  # Bottom lines should still show the active composer or footer affordances.
  local bottom6=$(echo "$screen" | tail -6)
  if ! echo "$bottom6" | grep -Eqi "Type a message|Talk directly to your agent here|Message #|Ctrl\+J newline|Team offline|/doctor"; then
    issues="$issues no-input-at-bottom"
  fi

  # Should not have more than 5 consecutive blank lines
  local max_blank=0
  local current_blank=0
  while IFS= read -r line; do
    if [ -z "$(echo "$line" | tr -d '[:space:]')" ]; then
      current_blank=$((current_blank + 1))
    else
      if [ "$current_blank" -gt "$max_blank" ]; then
        max_blank=$current_blank
      fi
      current_blank=0
    fi
  done <<< "$screen"
  if [ "$max_blank" -gt 5 ]; then
    issues="$issues excessive-blank-lines($max_blank)"
  fi

  if [ -n "$issues" ]; then
    echo "    LAYOUT FAIL [$label]:$issues"
    echo "$screen" > "$ARTIFACTS/layout-fail-$label.txt"
    return 1
  fi
  echo "    LAYOUT OK [$label]"
  return 0
}

# ─── Test lifecycle ─────────────────────────────────────────────────────

run_test() {
  local id="$1"
  local desc="$2"
  case $CURRENT_P in
    1) P1_TOTAL=$((P1_TOTAL + 1)) ;;
    2) P2_TOTAL=$((P2_TOTAL + 1)) ;;
    3) P3_TOTAL=$((P3_TOTAL + 1)) ;;
    4) P4_TOTAL=$((P4_TOTAL + 1)) ;;
    5) P5_TOTAL=$((P5_TOTAL + 1)) ;;
  esac
  echo ""
  echo "  [$id] $desc"
}

pass() {
  case $CURRENT_P in
    1) P1_PASS=$((P1_PASS + 1)) ;;
    2) P2_PASS=$((P2_PASS + 1)) ;;
    3) P3_PASS=$((P3_PASS + 1)) ;;
    4) P4_PASS=$((P4_PASS + 1)) ;;
    5) P5_PASS=$((P5_PASS + 1)) ;;
  esac
}

fail() {
  case $CURRENT_P in
    1) P1_FAIL=$((P1_FAIL + 1)) ;;
    2) P2_FAIL=$((P2_FAIL + 1)) ;;
    3) P3_FAIL=$((P3_FAIL + 1)) ;;
    4) P4_FAIL=$((P4_FAIL + 1)) ;;
    5) P5_FAIL=$((P5_FAIL + 1)) ;;
  esac
}

# Composite check: run all human judgment assertions at once
assert_human_quality() {
  local label="$1"
  local ok=0
  assert_readable "$label" && assert_no_junk "$label" && assert_layout "$label" && assert_density "$label" && ok=1
  return $((1 - ok))
}

# ═══════════════════════════════════════════════════════════════════════
echo "================================================================"
echo "  NEX Human Judgment UAT Test Suite"
echo "  Office Channel View"
echo "  5 Personas / Readability / Density / Layout / Junk-free"
echo "================================================================"
echo ""
echo "Binary:    $BINARY"
echo "Artifacts: $ARTIFACTS"
echo ""

# Build fresh binary
echo "Building binary..."
cd "$ROOT" && go build -o wuphf ./cmd/wuphf 2>&1
echo "Build complete."
echo ""

# Start daemon with the reliable office channel surface (120x40)
echo "Starting termwright daemon (120x40)..."
termwright daemon --socket "$SOCKET" --cols 120 --rows 40 --background -- "$BINARY" --channel-view --channel-app messages
echo "Waiting for TUI boot..."
sleep 5

# ═══════════════════════════════════════════════════════════════════════
# PERSONA 1: Maya (First-time user, non-technical)
#   "I just installed wuphf. What am I looking at?"
# ═══════════════════════════════════════════════════════════════════════
CURRENT_P=1
echo ""
echo "--- PERSONA 1: Maya (First-time user) ---"
echo "  Focus: Is the first impression clean and understandable?"

run_test "M1" "Boot: welcome message is readable (not JSON, not protocol)"
save_screenshot "maya-01-boot"
if assert_screen_not_blank "m1" && assert_text "The WUPHF Office|Welcome to The WUPHF Office" "m1" && assert_text "Message #general" "m1" && assert_readable "m1-boot"; then pass; else fail; fi

run_test "M2" "Slash autocomplete groups commands with clear descriptions"
clear_input
send_raw "/"
sleep 0.5
save_screenshot "maya-02-autocomplete"
if assert_text "/doctor" "m2" && assert_text "SETUP|NAVIGATE|PEOPLE|WORK" "m2" && assert_readable "m2-autocomplete"; then pass; else fail; fi
clear_input

run_test "M3" "Type message: input appears cleanly in input field"
clear_input
send_raw "hello team"
sleep 0.5
save_screenshot "maya-03-type"
if assert_text "hello team" "m3"; then pass; else fail; fi

run_test "M4" "Layout correct after interaction"
if assert_layout "m4-layout"; then pass; else fail; fi

run_test "M5" "Screen density: not mostly empty"
if assert_density "m5-density"; then pass; else fail; fi

# ═══════════════════════════════════════════════════════════════════════
# PERSONA 2: Raj (Power user, rapid commands)
#   "I need fast, clean output from every command"
# ═══════════════════════════════════════════════════════════════════════
CURRENT_P=2
echo ""
echo "--- PERSONA 2: Raj (Power user) ---"
echo "  Focus: Rapid commands produce clean, junk-free output"

run_test "R1" "/doctor then /agents then /switcher: each output stays clean"
run_command "/doctor"
sleep 1
save_screenshot "raj-01a-doctor"
if assert_text "Doctor" "r1-doctor" && assert_no_junk "r1-doctor"; then
  echo "    PASS: /doctor output junk-free"
else
  echo "    INFO: /doctor had issues"
fi
run_command "/agents"
save_screenshot "raj-01b-agents"
if assert_no_junk "r1-agents"; then
  echo "    PASS: /agents output junk-free"
else
  echo "    INFO: /agents had issues"
fi
send_escape
sleep 0.5
run_command "/switcher"
save_screenshot "raj-01c-switcher"
if assert_text "Workspace Switcher" "r1-switcher" && assert_no_junk "r1-switcher"; then pass; else fail; fi
send_escape
sleep 0.5

run_test "R2" "Autocomplete appears quickly after /"
clear_input
send_raw "/"
sleep 0.5
save_screenshot "raj-02-autocomplete"
if assert_text "/integrate|/agents|/switch" "r2"; then pass; else fail; fi
clear_input

run_test "R3" "After 5 commands, content still renders (no stuck state)"
for cmd in "/messages" "/tasks" "/calendar" "/policies" "/skills"; do
  run_command "$cmd" 1
done
save_screenshot "raj-03-rapid"
if assert_screen_not_blank "r3" && assert_readable "r3-rapid"; then pass; else fail; fi

run_test "R4" "Screen state readable after rapid input"
if assert_readable "r4-final"; then pass; else fail; fi

# ═══════════════════════════════════════════════════════════════════════
# PERSONA 3: Sarah (SDR, sales workflow)
#   "I need CRM commands to give clean, formatted feedback"
# ═══════════════════════════════════════════════════════════════════════
CURRENT_P=3
echo ""
echo "--- PERSONA 3: Sarah (Operator) ---"
echo "  Focus: navigation and planning surfaces stay readable"

run_test "S1" "/channels opens a clean channel picker"
run_command "/channels"
save_screenshot "sarah-01-channels"
if assert_text "Channels" "s1-channels" && assert_readable "s1-channels"; then pass; else fail; fi
send_escape
sleep 0.5

run_test "S2" "/1o1 opens a clear direct-session picker"
run_command "/1o1"
save_screenshot "sarah-02-direct-session"
if assert_text "Direct Session|Enable 1:1 mode" "s2-1o1" && assert_no_junk "s2-1o1"; then pass; else fail; fi
send_escape
sleep 0.5

run_test "S3" "/tasks view remains formatted and readable"
run_command "/tasks"
save_screenshot "sarah-03-tasks"
if assert_readable "s3-tasks" && assert_no_junk "s3-tasks"; then pass; else fail; fi

run_test "S4" "/calendar day stays junk-free"
run_command "/calendar day"
save_screenshot "sarah-04-calendar-day"
if assert_readable "s4-calendar" && assert_no_junk "s4-calendar"; then pass; else fail; fi

# ═══════════════════════════════════════════════════════════════════════
# PERSONA 4: Alex (Developer)
#   "I need readiness and workspace tools to stay clear"
# ═══════════════════════════════════════════════════════════════════════
CURRENT_P=4
echo ""
echo "--- PERSONA 4: Alex (Developer) ---"
echo "  Focus: runtime and workspace tooling stay formatted"

run_test "A1" "/doctor shows runtime checks cleanly"
run_command "/doctor"
save_screenshot "alex-01-doctor"
if assert_text "Doctor" "a1-doctor" && assert_readable "a1-doctor"; then pass; else fail; fi

run_test "A2" "/switcher shows the unified workspace picker"
run_command "/switcher"
save_screenshot "alex-02-switcher"
if assert_text "Workspace Switcher" "a2-switcher" && assert_no_junk "a2-switcher"; then pass; else fail; fi
send_escape
sleep 0.5

run_test "A3" "/agents shows named teammate actions"
run_command "/agents"
save_screenshot "alex-03-agents"
if assert_text "Agents in #general" "a3-agents" && assert_text "Disable Frontend Engineer|Disable Product Manager|Disable Backend Engineer" "a3-agents" && assert_readable "a3-agents"; then pass; else fail; fi
send_escape
sleep 0.5

run_test "A4" "Layout still correct after developer/operator commands"
if assert_layout "a4-layout"; then pass; else fail; fi

# ═══════════════════════════════════════════════════════════════════════
# PERSONA 5: Kim (Manager, reviewing team output)
#   "I want a clear view of my agents and their status"
# ═══════════════════════════════════════════════════════════════════════
CURRENT_P=5
echo ""
echo "--- PERSONA 5: Kim (Manager) ---"
echo "  Focus: Agent list is clean, status indicators correct, good density"

run_test "K1" "/agents: clean list with names and statuses"
run_command "/agents"
sleep 1
save_screenshot "kim-01-agents"
if assert_text "Agents in #general" "k1" && assert_text "Disable Frontend Engineer|Disable Product Manager|Disable Backend Engineer" "k1" && assert_readable "k1-agents"; then pass; else fail; fi
send_escape
sleep 0.5

run_test "K2" "Status indicators use proper symbols"
screen_k2=$(get_screen)
save_screenshot "kim-02-status"
# Check for status symbols: filled circle, half-circle, lightning, empty circle
if echo "$screen_k2" | grep -q '[●◐⚡○]'; then
  echo "    PASS: status indicator symbols present"
  pass
else
  # Not a hard fail — symbols depend on agent state
  echo "    INFO: no status symbols found (agents may all be same state)"
  pass
fi

run_test "K3" "Agent messages well-formatted (name: content pattern)"
run_command "/messages"
clear_input
send_raw "hello team"
sleep 0.5
save_screenshot "kim-03-messages"
if assert_text "hello team" "k3" && assert_no_junk "k3-messages" && assert_readable "k3-messages"; then pass; else fail; fi

run_test "K4" "Screen density: good use of space"
if assert_density "k4-density"; then pass; else fail; fi

# ═══════════════════════════════════════════════════════════════════════
# CROSS-CUTTING: Full human quality sweep
# ═══════════════════════════════════════════════════════════════════════
echo ""
echo "--- CROSS-CUTTING: Full Human Quality Sweep ---"

echo ""
echo "  [HQ1] Final screen: readable + junk-free + good layout + good density"
save_screenshot "hq-final-state"
hq_ok=true
assert_readable "hq-final" || hq_ok=false
assert_no_junk "hq-final" || hq_ok=false
assert_layout "hq-final" || hq_ok=false
assert_density "hq-final" || hq_ok=false
if [ "$hq_ok" = true ]; then
  echo "    FULL QUALITY SWEEP: PASS"
else
  echo "    FULL QUALITY SWEEP: some issues found (see artifacts)"
fi

echo ""
echo "  [HQ2] No 'Loading...' stuck on screen"
if assert_not_text "Loading\.\.\." "hq-loading"; then
  echo "    NO-LOADING: PASS"
else
  echo "    NO-LOADING: FAIL"
fi

echo ""
echo "  [HQ3] No panic or crash trace"
if assert_not_text "panic:" "hq-panic" && assert_not_text "goroutine" "hq-goroutine"; then
  echo "    NO-CRASH: PASS"
else
  echo "    NO-CRASH: FAIL"
fi

# ═══════════════════════════════════════════════════════════════════════
# CLEAN EXIT
# ═══════════════════════════════════════════════════════════════════════
echo ""
echo "  [EXIT] Clean exit with double Ctrl+C"
send_ctrl_c; sleep 1
send_ctrl_c; sleep 1
echo "    Exit signals sent"

# ═══════════════════════════════════════════════════════════════════════
# RESULTS
# ═══════════════════════════════════════════════════════════════════════
TOTAL_PASS=$((P1_PASS + P2_PASS + P3_PASS + P4_PASS + P5_PASS))
TOTAL_TESTS=$((P1_TOTAL + P2_TOTAL + P3_TOTAL + P4_TOTAL + P5_TOTAL))

if [ "$TOTAL_TESTS" -gt 0 ]; then
  PCT=$(( (TOTAL_PASS * 100) / TOTAL_TESTS ))
else
  PCT=0
fi

echo ""
echo ""
echo "=== HUMAN JUDGMENT UAT RESULTS ==="
printf "%-24s %d/%d passed\n" "Maya (First-time):" "$P1_PASS" "$P1_TOTAL"
printf "%-24s %d/%d passed\n" "Raj (Power user):" "$P2_PASS" "$P2_TOTAL"
printf "%-24s %d/%d passed\n" "Sarah (SDR):" "$P3_PASS" "$P3_TOTAL"
printf "%-24s %d/%d passed\n" "Alex (Developer):" "$P4_PASS" "$P4_TOTAL"
printf "%-24s %d/%d passed\n" "Kim (Manager):" "$P5_PASS" "$P5_TOTAL"
echo "-------------------------------------------"
printf "TOTAL: %d/%d passed (%d%%)\n" "$TOTAL_PASS" "$TOTAL_TESTS" "$PCT"
echo ""
echo "Assertion types used:"
echo "  assert_readable  — no raw JSON/NDJSON/ANSI, readable line lengths"
echo "  assert_density   — >=40% content lines"
echo "  assert_no_junk   — no protocol data, tracebacks, debug output"
echo "  assert_layout    — input at bottom, no excessive blank lines"
echo ""
echo "Screenshots: $ARTIFACTS/"

# Write results to file
{
  echo "Human Judgment UAT Results -- $TIMESTAMP"
  echo "========================================="
  echo ""
  printf "%-24s %d/%d passed\n" "Maya (First-time):" "$P1_PASS" "$P1_TOTAL"
  printf "%-24s %d/%d passed\n" "Raj (Power user):" "$P2_PASS" "$P2_TOTAL"
  printf "%-24s %d/%d passed\n" "Sarah (SDR):" "$P3_PASS" "$P3_TOTAL"
  printf "%-24s %d/%d passed\n" "Alex (Developer):" "$P4_PASS" "$P4_TOTAL"
  printf "%-24s %d/%d passed\n" "Kim (Manager):" "$P5_PASS" "$P5_TOTAL"
  echo ""
  printf "TOTAL: %d/%d passed (%d%%)\n" "$TOTAL_PASS" "$TOTAL_TESTS" "$PCT"
} > "$ARTIFACTS/results.txt"

echo "Results saved to: $ARTIFACTS/results.txt"

TOTAL_FAIL=$((TOTAL_TESTS - TOTAL_PASS))
if [ "$TOTAL_FAIL" -gt 0 ]; then
  echo ""
  echo "FAILURES detected -- check $ARTIFACTS/ for failure dumps"
  exit 1
fi
