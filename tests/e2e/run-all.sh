#!/bin/bash
# E2E acceptance tests for WUPHF CLI Go TUI
# Uses termwright daemon mode for reliable alt-screen testing

set -e

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TERMWRIGHT="${TERMWRIGHT:-$(command -v termwright || true)}"
if [ -z "$TERMWRIGHT" ]; then
  echo "termwright not found in PATH; set TERMWRIGHT=/abs/path/to/termwright" >&2
  exit 1
fi
SOCKET="/tmp/wuphf-go-e2e.sock"
NEX="${WUPHF_BIN:-$REPO_ROOT/wuphf}"
ARTIFACTS="${ARTIFACTS:-$REPO_ROOT/termwright-artifacts/e2e-$(date +%Y%m%d-%H%M%S)}"
mkdir -p "$ARTIFACTS"

PASS=0
FAIL=0
TOTAL=0

cleanup() {
  pkill -f "termwright.*wuphf-go-e2e" 2>/dev/null || true
  rm -f "$SOCKET"
  sleep 1
}

start_daemon() {
  cleanup
  "$TERMWRIGHT" daemon --socket "$SOCKET" --cols 120 --rows 40 -- "$NEX" &
  sleep 2
}

screen() {
  "$TERMWRIGHT" exec --socket "$SOCKET" --method screen --params '{}' 2>&1
}

screen_text() {
  screen | python3 -c "import json,sys; print(json.load(sys.stdin).get('result',''))" 2>/dev/null
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

wait_for() {
  local text="$1"
  local timeout="${2:-10}"
  local elapsed=0
  while [ $elapsed -lt $timeout ]; do
    if screen_text | grep -q "$text" 2>/dev/null; then
      return 0
    fi
    sleep 0.5
    elapsed=$((elapsed + 1))
  done
  return 1
}

assert_screen_contains() {
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

assert_screen_not_contains() {
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

echo "=== WUPHF CLI E2E Acceptance Tests ==="
echo "Binary: $NEX"
echo "Artifacts: $ARTIFACTS"
echo ""

# ────────────────────────────────────────────────────
echo "AC-1: Launch -> welcome message -> input available"
start_daemon
wait_for "Type a message" 10
assert_screen_contains "Welcome" "Welcome message shown"
assert_screen_contains "Type a message" "Input placeholder visible"
assert_screen_contains "INSERT" "Insert mode active"
assert_screen_contains "AGENTS" "Agent roster visible"
assert_screen_contains "Team Lead" "Team Lead agent shown"
screenshot "ac01-launch"
cleanup

# ────────────────────────────────────────────────────
echo ""
echo "AC-2: Slash autocomplete"
start_daemon
wait_for "Type a message" 10
type_text "/"
sleep 1
screenshot "ac02-slash-typed"
assert_screen_contains "help" "Autocomplete shows /help"
assert_screen_contains "quit" "Autocomplete shows /quit"
type_text "hel"
sleep 1
screenshot "ac02-filter"
press_key "Tab"
sleep 1
screenshot "ac02-tab-accepted"
cleanup

# ────────────────────────────────────────────────────
echo ""
echo "AC-3: /help command"
start_daemon
wait_for "Type a message" 10
type_text "/help"
press_key "Enter"
sleep 1
screenshot "ac03-help"
assert_screen_contains "help" "/help shows output"
cleanup

# ────────────────────────────────────────────────────
echo ""
echo "AC-4: /clear command"
start_daemon
wait_for "Type a message" 10
type_text "/clear"
press_key "Enter"
sleep 1
screenshot "ac04-clear"
cleanup

# ────────────────────────────────────────────────────
echo ""
echo "AC-5: /agents command"
start_daemon
wait_for "Type a message" 10
type_text "/agents"
press_key "Enter"
sleep 1
screenshot "ac05-agents"
assert_screen_contains "Team Lead" "/agents lists Team Lead"
cleanup

# ────────────────────────────────────────────────────
echo ""
echo "AC-6: Agent roster shows live status"
start_daemon
wait_for "Type a message" 10
assert_screen_contains "idle" "Team Lead shows idle status"
screenshot "ac06-roster-idle"
cleanup

# ────────────────────────────────────────────────────
echo ""
echo "AC-7: Vim mode switching (Esc/i)"
start_daemon
wait_for "Type a message" 10
assert_screen_contains "INSERT" "Starts in INSERT mode"
press_key "Escape"
sleep 0.5
screenshot "ac07-normal"
assert_screen_contains "NORMAL" "Esc switches to NORMAL"
type_text "i"
sleep 0.5
assert_screen_contains "INSERT" "i switches back to INSERT"
screenshot "ac07-insert"
cleanup

# ────────────────────────────────────────────────────
echo ""
echo "AC-8: Help view (? in normal mode)"
start_daemon
wait_for "Type a message" 10
press_key "Escape"
sleep 0.5
type_text "?"
sleep 1
screenshot "ac08-help-view"
assert_screen_contains "Keybindings" "? shows help view"
assert_screen_contains "Scroll" "Help shows keybinding docs"
type_text "q"
sleep 0.5
assert_screen_contains "Type a message" "q returns to stream"
screenshot "ac08-back"
cleanup

# ────────────────────────────────────────────────────
echo ""
echo "AC-9: Ctrl+C double-press exit (SKIP — termwright cannot send Ctrl+C signal)"
echo "  SKIP: Ctrl+C is a signal, not a key — verified via unit test instead"
# Ctrl+C sends SIGINT which termwright's press API doesn't support.
# The double-press behavior is covered by TestDoublePress in keybindings_test.go.

# ────────────────────────────────────────────────────
echo ""
echo "AC-10: /quit exits"
start_daemon
wait_for "Type a message" 10
type_text "/quit"
press_key "Enter"
sleep 2
screenshot "ac10-quit"
# Process should have exited; daemon socket may be gone
cleanup

# ────────────────────────────────────────────────────
echo ""
echo "=== Results ==="
echo "Passed: $PASS / $TOTAL"
if [ $FAIL -gt 0 ]; then
  echo "Failed: $FAIL"
  echo "Artifacts: $ARTIFACTS"
  exit 1
else
  echo "All E2E tests passed!"
  exit 0
fi
