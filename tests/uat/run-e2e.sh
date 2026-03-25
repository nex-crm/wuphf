#!/bin/bash
# E2E test using termwright daemon with raw input for Bubbletea compatibility
set -e

SOCKET="/tmp/wuphf-e2e-$$.sock"
BINARY="$(cd "$(dirname "$0")/../.." && pwd)/wuphf"
ARTIFACTS="$(cd "$(dirname "$0")/../.." && pwd)/termwright-artifacts/e2e-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$ARTIFACTS"

cleanup() {
  pkill -f "termwright daemon.*$SOCKET" 2>/dev/null || true
  rm -f "$SOCKET"
}
trap cleanup EXIT

echo "=== Starting TUI E2E Test ==="
echo "Binary: $BINARY"
echo "Artifacts: $ARTIFACTS"

# Start daemon
termwright daemon --socket "$SOCKET" --cols 120 --rows 40 --background "$BINARY"
sleep 5

# Helper functions
send_raw() {
  # Send each character individually for Bubbletea compatibility
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

get_screen() {
  termwright exec --socket "$SOCKET" --method screen --params '{}' 2>/dev/null | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('result',''))"
}

screenshot() {
  termwright exec --socket "$SOCKET" --method screenshot --params "{\"path\": \"$ARTIFACTS/$1.png\"}" >/dev/null 2>&1 || true
}

assert_text() {
  local text="$1"
  local screen=$(get_screen)
  if echo "$screen" | grep -q "$text"; then
    echo "  PASS: found '$text'"
  else
    echo "  FAIL: '$text' not found on screen"
    echo "$screen" > "$ARTIFACTS/failure-screen.txt"
    exit 1
  fi
}

PASS=0
FAIL=0

run_test() {
  echo ""
  echo "--- Test: $1 ---"
}

pass() {
  PASS=$((PASS + 1))
}

# ===== TESTS =====

run_test "1. TUI boots with roster showing founding team"
# Wait for Bubbletea to render — needs WindowSizeMsg first
sleep 5
screenshot "01-boot"
assert_text "AGENTS"
assert_text "CEO"
assert_text "Backend Engineer"
assert_text "Frontend Engineer"
assert_text "AI Engineer"
assert_text "Type a message"
echo "  PASS: TUI boot verified"
pass

run_test "2. Slash autocomplete shows commands"
send_raw "/"
sleep 1
screenshot "02-autocomplete"
assert_text "/ask"
assert_text "/object"
assert_text "/record"
assert_text "/note"
echo "  PASS: Autocomplete shows commands"
pass

run_test "3. /help shows available commands"
send_ctrl_u
sleep 0.3
send_raw "/help"
sleep 0.5
send_enter
sleep 1
screenshot "03-help"
assert_text "/help"
assert_text "/init"
assert_text "/quit"
echo "  PASS: /help output shows commands"
pass

run_test "4. /agents lists founding team"
send_raw "/agents"
sleep 0.3
send_enter
sleep 1
screenshot "04-agents"
assert_text "Active agents"
assert_text "CEO"
echo "  PASS: /agents shows founding team"
pass

run_test "5. Plain message submission"
send_raw "hello team"
sleep 0.5
send_enter
sleep 1
screenshot "05-message"
assert_text "You"
assert_text "hello team"
echo "  PASS: Message submitted and displayed"
pass

run_test "6. /init autocomplete visible"
send_raw "/in"
sleep 0.5
screenshot "06-init-autocomplete"
assert_text "init"
echo "  PASS: /init in autocomplete"
pass

run_test "7. /provider autocomplete visible"
send_ctrl_u
sleep 0.3
send_raw "/pr"
sleep 0.5
screenshot "07-provider-autocomplete"
assert_text "provider"
echo "  PASS: /provider in autocomplete"
pass

run_test "8. Clean exit with Ctrl+C"
send_ctrl_u
sleep 0.3
screenshot "08-final"
send_ctrl_c
sleep 1
send_ctrl_c
sleep 1
echo "  PASS: Exit signal sent"
pass

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
echo "Screenshots: $ARTIFACTS/"
