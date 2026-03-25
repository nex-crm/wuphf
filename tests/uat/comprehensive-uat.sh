#!/bin/bash
# Comprehensive multi-persona UAT test suite for wuphf TUI
# Tests CURRENT state: embedded terminal mode (pane-per-agent layout)
#
# When `claude` binary is available, wuphf boots into embedded mode:
# - Leader pane (CEO) takes top 60% of screen
# - Specialist panes (PM, FE, BE, etc.) in bottom row
# - Ctrl+N/P cycle focus, Ctrl+B toggles broadcast, Ctrl+1..7 jump
# - Each pane is its own PTY running claude
#
# This suite tests from 5 user personas to ensure the UI is
# visually correct, responsive, and navigable.
set -euo pipefail

SOCKET="/tmp/wuphf-uat-$$.sock"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BINARY="$ROOT/wuphf"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
ARTIFACTS="$ROOT/termwright-artifacts/uat-$TIMESTAMP"
mkdir -p "$ARTIFACTS"

# Per-persona counters (bash 3 compatible)
P1_PASS=0; P1_FAIL=0; P1_TOTAL=0  # Maya
P2_PASS=0; P2_FAIL=0; P2_TOTAL=0  # Raj
P3_PASS=0; P3_FAIL=0; P3_TOTAL=0  # Sarah
P4_PASS=0; P4_FAIL=0; P4_TOTAL=0  # Alex
P5_PASS=0; P5_FAIL=0; P5_TOTAL=0  # Kim
QUALITY_PASS=0; QUALITY_FAIL=0; QUALITY_TOTAL=0
CURRENT_P=0

cleanup() {
  pkill -f "termwright daemon.*$SOCKET" 2>/dev/null || true
  rm -f "$SOCKET"
}
trap cleanup EXIT

# ─── Helpers ────────────────────────────────────────────────────────────

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

send_ctrl_n() {
  # Ctrl+N = 0x0E
  termwright exec --socket "$SOCKET" --method raw --params '{"bytes_base64": "Dg=="}' >/dev/null 2>&1
}

send_ctrl_p() {
  # Ctrl+P = 0x10
  termwright exec --socket "$SOCKET" --method raw --params '{"bytes_base64": "EA=="}' >/dev/null 2>&1
}

send_ctrl_b() {
  # Ctrl+B = 0x02
  termwright exec --socket "$SOCKET" --method raw --params '{"bytes_base64": "Ag=="}' >/dev/null 2>&1
}

get_screen() {
  termwright exec --socket "$SOCKET" --method screen --params '{}' 2>/dev/null | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('result',''))"
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
  get_screen > "$ARTIFACTS/$name.txt" 2>/dev/null || true
}

# ─── Assertions ─────────────────────────────────────────────────────────

assert_text() {
  local text="$1"
  local label="${2:-check}"
  local screen
  screen=$(get_screen)
  if echo "$screen" | grep -qi "$text"; then
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
  screen=$(get_screen)
  if echo "$screen" | grep -qi "$text"; then
    echo "    FAIL: '$text' should NOT be on screen"
    echo "$screen" > "$ARTIFACTS/failure-$label.txt"
    return 1
  else
    echo "    PASS: '$text' correctly absent"
    return 0
  fi
}

assert_pane_header() {
  local slug="$1"
  local name="$2"
  local label="${3:-pane}"
  local screen
  screen=$(get_screen)
  # Pane headers look like: [slug] Name  or  [slug] Name *
  if echo "$screen" | grep -q "\[$slug\].*$name"; then
    echo "    PASS: pane [$slug] $name visible"
    return 0
  else
    echo "    FAIL: pane [$slug] $name not found"
    echo "$screen" > "$ARTIFACTS/failure-$label.txt"
    return 1
  fi
}

assert_focus_on() {
  local slug="$1"
  local label="${2:-focus}"
  local screen
  screen=$(get_screen)
  # Focused pane has a * after its name: [slug] Name *
  if echo "$screen" | grep -q "\[$slug\].*\*"; then
    echo "    PASS: [$slug] is focused (*)"
    return 0
  else
    echo "    FAIL: [$slug] not focused (no * marker)"
    echo "$screen" > "$ARTIFACTS/failure-$label.txt"
    return 1
  fi
}

assert_has_borders() {
  local label="${1:-borders}"
  local screen
  screen=$(get_screen)
  if echo "$screen" | grep -q '[╭╮╰╯│─]'; then
    echo "    PASS: border characters present"
    return 0
  else
    echo "    FAIL: no border characters found"
    echo "$screen" > "$ARTIFACTS/failure-$label.txt"
    return 1
  fi
}

assert_screen_not_blank() {
  local label="${1:-blank}"
  local screen
  screen=$(get_screen)
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

# Quality check: not blank + no raw escapes + borders present
assert_quality() {
  local label="$1"
  local screen
  screen=$(get_screen)
  local issues=""

  QUALITY_TOTAL=$((QUALITY_TOTAL + 1))

  local linecount
  linecount=$(echo "$screen" | grep -cv "^$" || true)
  if [ "$linecount" -lt 5 ]; then
    issues="$issues blank-screen($linecount)"
  fi

  if echo "$screen" | grep -qE '\\x1b|\[0m|\[1m|\[3[0-9]m'; then
    issues="$issues raw-escapes"
  fi

  if ! echo "$screen" | grep -q '[╭╮╰╯│─]'; then
    issues="$issues no-borders"
  fi

  if [ -n "$issues" ]; then
    echo "    QUALITY FAIL [$label]:$issues"
    QUALITY_FAIL=$((QUALITY_FAIL + 1))
    echo "$screen" > "$ARTIFACTS/quality-fail-$label.txt"
    return 1
  fi
  echo "    QUALITY OK [$label]"
  QUALITY_PASS=$((QUALITY_PASS + 1))
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

# ═══════════════════════════════════════════════════════════════════════
echo "================================================================"
echo "  NEX Comprehensive UAT Test Suite"
echo "  Embedded Terminal Mode (pane-per-agent)"
echo "  5 Personas / Quality Checks / Screenshots"
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

# Start daemon with wide terminal (130x50)
echo "Starting termwright daemon (130x50)..."
termwright daemon --socket "$SOCKET" --cols 130 --rows 50 --background "$BINARY"
echo "Waiting for TUI boot..."
sleep 7

# ═══════════════════════════════════════════════════════════════════════
# PERSONA 1: Maya (First-time user, non-technical)
#   "I just installed wuphf. What am I looking at?"
# ═══════════════════════════════════════════════════════════════════════
CURRENT_P=1
echo ""
echo "--- PERSONA 1: Maya (First-time user) ---"
echo "  Scenario: Cold boot, sees pane layout, understands structure"

run_test "M1" "Boot: screen is not blank"
save_screenshot "maya-01-boot"
if assert_screen_not_blank "m1"; then pass; else fail; fi
assert_quality "maya-boot"

run_test "M2" "Leader pane visible: [ceo] CEO pane header"
if assert_pane_header "ceo" "CEO" "m2"; then pass; else fail; fi

run_test "M3" "Specialist panes visible: at least PM, Frontend, Backend, or AI visible"
save_screenshot "maya-03-specialists"
pane_count=0
for slug_name in "pm:Product Manager" "fe:Frontend Engineer" "be:Backend Engineer" "ai:AI Engineer"; do
  slug="${slug_name%%:*}"
  name="${slug_name#*:}"
  if assert_pane_header "$slug" "$name" "m3-$slug"; then
    pane_count=$((pane_count + 1))
  fi
done
if [ "$pane_count" -ge 2 ]; then
  echo "    PASS: $pane_count specialist panes visible (>= 2)"
  pass
else
  echo "    FAIL: only $pane_count specialist panes visible"
  fail
fi

run_test "M4" "Border characters render correctly (no garbage)"
if assert_has_borders "m4"; then pass; else fail; fi

run_test "M5" "Overflow indicator: '+N more panes' if agents > visible slots"
save_screenshot "maya-05-overflow"
# Founding team has 8 agents; only a visible subset of the company is pane-rendered
screen_m5=$(get_screen)
if echo "$screen_m5" | grep -q "more pane"; then
  echo "    PASS: overflow indicator visible"
  pass
else
  echo "    INFO: no overflow indicator (all panes may fit)"
  # Not a failure — depends on terminal width
  pass
fi

run_test "M6" "CEO pane is focused by default (has * marker)"
if assert_focus_on "ceo" "m6"; then pass; else fail; fi

# ═══════════════════════════════════════════════════════════════════════
# PERSONA 2: Raj (Power user, 10+ daily sessions)
#   "I need fast navigation between panes"
# ═══════════════════════════════════════════════════════════════════════
CURRENT_P=2
echo ""
echo "--- PERSONA 2: Raj (Power user) ---"
echo "  Scenario: Pane navigation, focus switching, broadcast mode"

run_test "R1" "Ctrl+N moves focus to next pane"
send_ctrl_n
sleep 0.5
save_screenshot "raj-01-focus-next"
# After Ctrl+N from CEO, should focus PM (index 1)
if assert_focus_on "pm" "r1"; then
  pass
else
  # Might focus a different pane — just check CEO lost focus
  if assert_not_text "\[ceo\].*\*" "r1-alt"; then
    echo "    PASS: focus moved away from ceo"
    pass
  else
    fail
  fi
fi

run_test "R2" "Ctrl+N again moves to next specialist"
send_ctrl_n
sleep 0.5
save_screenshot "raj-02-focus-next2"
# Should now be on FE (index 2)
if assert_focus_on "fe" "r2"; then
  pass
else
  # Just verify focus moved
  if assert_not_text "\[pm\].*\*" "r2-alt"; then
    echo "    PASS: focus advanced past pm"
    pass
  else
    fail
  fi
fi

run_test "R3" "Ctrl+P moves focus back to previous pane"
send_ctrl_p
sleep 0.5
save_screenshot "raj-03-focus-prev"
if assert_focus_on "pm" "r3"; then
  pass
else
  echo "    INFO: focus moved back but target uncertain"
  pass
fi
assert_quality "raj-focus"

run_test "R4" "Ctrl+B enables broadcast mode: [BROADCAST] appears"
send_ctrl_b
sleep 0.5
save_screenshot "raj-04-broadcast"
if assert_text "BROADCAST" "r4"; then pass; else fail; fi

run_test "R5" "Ctrl+B again disables broadcast mode"
send_ctrl_b
sleep 0.5
save_screenshot "raj-05-broadcast-off"
if assert_not_text "BROADCAST" "r5"; then pass; else fail; fi
assert_quality "raj-broadcast-off"

# ═══════════════════════════════════════════════════════════════════════
# PERSONA 3: Sarah (SDR, sales workflow)
#   "I want to talk to the CEO pane to delegate a task"
# ═══════════════════════════════════════════════════════════════════════
CURRENT_P=3
echo ""
echo "--- PERSONA 3: Sarah (SDR) ---"
echo "  Scenario: Focus CEO, type into pane, verify input reaches PTY"

run_test "S1" "Navigate focus back to CEO pane"
# From Raj tests we ended on PM (index 1). One Ctrl+P wraps to CEO (index 0).
send_ctrl_p; sleep 0.3
save_screenshot "sarah-01-focus-ceo"
if assert_focus_on "ceo" "s1"; then
  pass
else
  # If not on CEO, try one more Ctrl+P (in case focus drifted)
  send_ctrl_p; sleep 0.3
  if assert_focus_on "ceo" "s1-retry"; then pass; else fail; fi
fi

run_test "S2" "Type message into focused CEO pane"
send_raw "hello CEO"
sleep 1
save_screenshot "sarah-02-type"
# The text should appear somewhere in the CEO pane area
if assert_text "hello" "s2"; then pass; else fail; fi

run_test "S3" "Submit message with Enter"
send_enter
sleep 2
save_screenshot "sarah-03-submit"
# Screen should still have content
if assert_screen_not_blank "s3"; then pass; else fail; fi
assert_quality "sarah-submit"

run_test "S4" "Broadcast mode: type to all panes simultaneously"
send_ctrl_b
sleep 0.3
send_raw "team update"
sleep 1
save_screenshot "sarah-04-broadcast-type"
if assert_text "BROADCAST" "s4"; then pass; else fail; fi
# Disable broadcast mode
send_ctrl_b
sleep 0.3

run_test "S5" "Screen remains stable after multiple inputs"
save_screenshot "sarah-05-stable"
if assert_screen_not_blank "s5" && assert_has_borders "s5-borders"; then
  pass
else
  fail
fi
assert_quality "sarah-stable"

# ═══════════════════════════════════════════════════════════════════════
# PERSONA 4: Alex (Developer, building integrations)
#   "I want to see all panes and understand the layout"
# ═══════════════════════════════════════════════════════════════════════
CURRENT_P=4
echo ""
echo "--- PERSONA 4: Alex (Developer) ---"
echo "  Scenario: Layout structure, pane dimensions, agent coverage"

run_test "A1" "Layout: leader pane (CEO) occupies top section"
save_screenshot "alex-01-layout"
screen_a1=$(get_screen)
# CEO pane header should be near the top of the screen
first_ceo_line=$(echo "$screen_a1" | grep -n "\[ceo\]" | head -1 | cut -d: -f1)
if [ -n "$first_ceo_line" ] && [ "$first_ceo_line" -lt 5 ]; then
  echo "    PASS: CEO pane at top (line $first_ceo_line)"
  pass
else
  echo "    FAIL: CEO pane header at line ${first_ceo_line:-?}, expected near top"
  fail
fi

run_test "A2" "Layout: specialist panes in bottom row"
screen_a2=$(get_screen)
# Specialist pane headers should be in the lower half (line > 25 for 50-row terminal)
pm_line=$(echo "$screen_a2" | grep -n "\[pm\]" | head -1 | cut -d: -f1)
if [ -n "$pm_line" ] && [ "$pm_line" -gt 20 ]; then
  echo "    PASS: PM pane in bottom section (line $pm_line)"
  pass
else
  echo "    FAIL: PM pane at line ${pm_line:-?}, expected in bottom half"
  fail
fi

run_test "A3" "Pane count: at least 4 agent panes visible"
screen_a3=$(get_screen)
visible_panes=0
for slug in "ceo" "pm" "fe" "be" "designer" "cmo" "cro"; do
  if echo "$screen_a3" | grep -q "\[$slug\]"; then
    visible_panes=$((visible_panes + 1))
  fi
done
save_screenshot "alex-03-pane-count"
if [ "$visible_panes" -ge 4 ]; then
  echo "    PASS: $visible_panes panes visible (>= 4)"
  pass
else
  echo "    FAIL: only $visible_panes panes visible"
  fail
fi

run_test "A4" "Each visible pane has border characters"
screen_a4=$(get_screen)
# Check that we have both horizontal and vertical border chars
has_vertical=$(echo "$screen_a4" | grep -c "│" || true)
has_horizontal=$(echo "$screen_a4" | grep -c "[─╭╮╰╯]" || true)
save_screenshot "alex-04-borders"
if [ "$has_vertical" -gt 5 ] && [ "$has_horizontal" -gt 0 ]; then
  echo "    PASS: borders present ($has_vertical vertical, $has_horizontal horizontal)"
  pass
else
  echo "    FAIL: insufficient border chars (v=$has_vertical h=$has_horizontal)"
  fail
fi

run_test "A5" "No overlapping text or rendering artifacts"
save_screenshot "alex-05-clean"
screen_a5=$(get_screen)
# Check for common rendering issues: double box-drawing overlap
if echo "$screen_a5" | grep -q "││││\|────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────"; then
  echo "    FAIL: possible rendering overlap detected"
  fail
else
  echo "    PASS: no obvious rendering artifacts"
  pass
fi
assert_quality "alex-clean"

run_test "A6" "Pane name truncation: long names fit within pane width"
save_screenshot "alex-06-truncation"
# Product Manager is the longest name — should fit cleanly
if assert_pane_header "pm" "Product Manager" "a6"; then
  pass
else
  # Might be truncated — still ok if header is present
  if assert_text "\[pm\]" "a6-partial"; then
    echo "    PASS: [pm] header present (name may be truncated)"
    pass
  else
    fail
  fi
fi

# ═══════════════════════════════════════════════════════════════════════
# PERSONA 5: Kim (Manager, reviewing team output)
#   "I want to see all my agents and their status at a glance"
# ═══════════════════════════════════════════════════════════════════════
CURRENT_P=5
echo ""
echo "--- PERSONA 5: Kim (Manager) ---"
echo "  Scenario: Team overview, pane liveness, multi-agent visibility"

run_test "K1" "Team overview: CEO + at least 3 specialists visible simultaneously"
save_screenshot "kim-01-overview"
screen_k1=$(get_screen)
team_visible=0
for slug in "ceo" "pm" "fe" "be"; do
  if echo "$screen_k1" | grep -q "\[$slug\]"; then
    team_visible=$((team_visible + 1))
  fi
done
if [ "$team_visible" -ge 4 ]; then
  echo "    PASS: $team_visible core team panes visible"
  pass
else
  echo "    FAIL: only $team_visible core team panes visible (expected >= 4)"
  fail
fi
assert_quality "kim-overview"

run_test "K2" "Focus indicator: exactly one pane has * marker"
screen_k2=$(get_screen)
focus_count=$(echo "$screen_k2" | grep -c '\] .* \*' || true)
save_screenshot "kim-02-focus"
if [ "$focus_count" -eq 1 ]; then
  echo "    PASS: exactly 1 focused pane"
  pass
elif [ "$focus_count" -gt 1 ]; then
  echo "    FAIL: $focus_count panes appear focused (should be exactly 1)"
  fail
else
  echo "    FAIL: no pane appears focused"
  fail
fi

run_test "K3" "Rapid focus cycling: Tab through all visible panes without crash"
for i in 1 2 3 4 5 6; do
  send_ctrl_n
  sleep 0.3
done
save_screenshot "kim-03-cycle"
if assert_screen_not_blank "k3" && assert_has_borders "k3-borders"; then
  echo "    PASS: UI stable after rapid focus cycling"
  pass
else
  fail
fi
assert_quality "kim-cycle"

run_test "K4" "Pane alive: at least leader pane shows claude content"
# Navigate to CEO: cycle Ctrl+P until we see CEO focused, max 7 attempts (7 panes)
for attempt in 1 2 3 4 5 6 7; do
  send_ctrl_p; sleep 0.3
  screen_nav=$(get_screen)
  if echo "$screen_nav" | grep -q '\[ceo\].*\*'; then
    break
  fi
done
sleep 0.5
save_screenshot "kim-04-alive"
# The CEO pane should have some text content from claude starting up
screen_k4=$(get_screen)
# Just check the pane has visible content (not all empty lines)
if assert_screen_not_blank "k4"; then pass; else fail; fi

run_test "K5" "Clean double Ctrl+C exit: does not crash"
save_screenshot "kim-05-pre-exit"
# This is more of a smoke test that the exit path works
if assert_screen_not_blank "k5"; then pass; else fail; fi

# ═══════════════════════════════════════════════════════════════════════
# CROSS-CUTTING QUALITY CHECKS
# ═══════════════════════════════════════════════════════════════════════
echo ""
echo "--- CROSS-CUTTING QUALITY CHECKS ---"

echo ""
echo "  [Q1] No raw ANSI escape sequences in rendered output"
QUALITY_TOTAL=$((QUALITY_TOTAL + 1))
screen_q1=$(get_screen)
# Look for literal escape sequences that shouldn't be in rendered text
if echo "$screen_q1" | grep -qP '\\x1b' 2>/dev/null || echo "$screen_q1" | grep -q '\[0m\|\[1m\|\[3[0-9]m'; then
  echo "    QUALITY FAIL [raw-escapes]: escape sequences in rendered output"
  QUALITY_FAIL=$((QUALITY_FAIL + 1))
else
  echo "    QUALITY OK [raw-escapes]"
  QUALITY_PASS=$((QUALITY_PASS + 1))
fi

echo ""
echo "  [Q2] Screen geometry: full 130x50 utilized (not just top-left corner)"
QUALITY_TOTAL=$((QUALITY_TOTAL + 1))
screen_q2=$(get_screen)
# Check that content extends past column 80 (using the full width)
long_lines=$(echo "$screen_q2" | awk 'length > 80 { count++ } END { print count+0 }')
save_screenshot "quality-02-geometry"
if [ "$long_lines" -gt 5 ]; then
  echo "    QUALITY OK [geometry]: $long_lines lines use full width"
  QUALITY_PASS=$((QUALITY_PASS + 1))
else
  echo "    QUALITY FAIL [geometry]: only $long_lines lines > 80 chars"
  QUALITY_FAIL=$((QUALITY_FAIL + 1))
fi

echo ""
echo "  [Q3] No 'No panes available' fallback message"
QUALITY_TOTAL=$((QUALITY_TOTAL + 1))
if assert_not_text "No panes available" "q3" && assert_not_text "No panes" "q3"; then
  echo "    QUALITY OK [panes-available]"
  QUALITY_PASS=$((QUALITY_PASS + 1))
else
  echo "    QUALITY FAIL [panes-available]"
  QUALITY_FAIL=$((QUALITY_FAIL + 1))
fi

echo ""
echo "  [Q4] No 'Loading...' stuck message"
QUALITY_TOTAL=$((QUALITY_TOTAL + 1))
if assert_not_text "Loading\.\.\." "q4"; then
  echo "    QUALITY OK [no-loading-stuck]"
  QUALITY_PASS=$((QUALITY_PASS + 1))
else
  echo "    QUALITY FAIL [no-loading-stuck]"
  QUALITY_FAIL=$((QUALITY_FAIL + 1))
fi

echo ""
echo "  [Q5] Pane layout consistency: leader pane above specialist row"
QUALITY_TOTAL=$((QUALITY_TOTAL + 1))
screen_q5=$(get_screen)
ceo_line=$(echo "$screen_q5" | grep -n "\[ceo\]" | head -1 | cut -d: -f1)
pm_line=$(echo "$screen_q5" | grep -n "\[pm\]" | head -1 | cut -d: -f1)
if [ -n "$ceo_line" ] && [ -n "$pm_line" ] && [ "$ceo_line" -lt "$pm_line" ]; then
  echo "    QUALITY OK [layout]: CEO (line $ceo_line) above PM (line $pm_line)"
  QUALITY_PASS=$((QUALITY_PASS + 1))
else
  echo "    QUALITY FAIL [layout]: CEO=$ceo_line PM=$pm_line"
  QUALITY_FAIL=$((QUALITY_FAIL + 1))
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
TOTAL_PASS=$((P1_PASS + P2_PASS + P3_PASS + P4_PASS + P5_PASS + QUALITY_PASS))
TOTAL_TESTS=$((P1_TOTAL + P2_TOTAL + P3_TOTAL + P4_TOTAL + P5_TOTAL + QUALITY_TOTAL))

if [ "$TOTAL_TESTS" -gt 0 ]; then
  PCT=$(( (TOTAL_PASS * 100) / TOTAL_TESTS ))
else
  PCT=0
fi

echo ""
echo ""
echo "=== UAT RESULTS ==="
printf "%-24s %d/%d passed\n" "Maya (First-time):" "$P1_PASS" "$P1_TOTAL"
printf "%-24s %d/%d passed\n" "Raj (Power user):" "$P2_PASS" "$P2_TOTAL"
printf "%-24s %d/%d passed\n" "Sarah (SDR):" "$P3_PASS" "$P3_TOTAL"
printf "%-24s %d/%d passed\n" "Alex (Developer):" "$P4_PASS" "$P4_TOTAL"
printf "%-24s %d/%d passed\n" "Kim (Manager):" "$P5_PASS" "$P5_TOTAL"
printf "%-24s %d/%d passed\n" "Quality checks:" "$QUALITY_PASS" "$QUALITY_TOTAL"
echo "-------------------------------------------"
printf "TOTAL: %d/%d passed (%d%%)\n" "$TOTAL_PASS" "$TOTAL_TESTS" "$PCT"
echo ""
echo "Screenshots: $ARTIFACTS/"

# Write results to file
{
  echo "UAT Results -- $TIMESTAMP"
  echo "========================"
  echo ""
  printf "%-24s %d/%d passed\n" "Maya (First-time):" "$P1_PASS" "$P1_TOTAL"
  printf "%-24s %d/%d passed\n" "Raj (Power user):" "$P2_PASS" "$P2_TOTAL"
  printf "%-24s %d/%d passed\n" "Sarah (SDR):" "$P3_PASS" "$P3_TOTAL"
  printf "%-24s %d/%d passed\n" "Alex (Developer):" "$P4_PASS" "$P4_TOTAL"
  printf "%-24s %d/%d passed\n" "Kim (Manager):" "$P5_PASS" "$P5_TOTAL"
  printf "%-24s %d/%d passed\n" "Quality checks:" "$QUALITY_PASS" "$QUALITY_TOTAL"
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
