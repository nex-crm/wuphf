#!/bin/bash
# E2E test for embedded terminal panes mode.
# Verifies: boot, roster, CEO pane, input routing, clean shutdown.
#
# Prerequisites:
# - termwright daemon in PATH
# - claude binary in PATH (for embedded mode)
# - wuphf binary built at repo root
#
# Usage: ./terminal-panes-e2e.sh
set -e

SOCKET="/tmp/wuphf-panes-e2e-$$.sock"
BINARY="$(cd "$(dirname "$0")/../.." && pwd)/wuphf"
ARTIFACTS="$(cd "$(dirname "$0")/../.." && pwd)/termwright-artifacts/panes-e2e-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$ARTIFACTS"

PASS=0
FAIL=0

cleanup() {
  pkill -f "termwright daemon.*$SOCKET" 2>/dev/null || true
  rm -f "$SOCKET"
  echo ""
  echo "=== Results: $PASS passed, $FAIL failed ==="
  echo "Artifacts: $ARTIFACTS"
}
trap cleanup EXIT

check() {
  local name="$1"
  local pattern="$2"
  local file="$3"
  if grep -q "$pattern" "$file" 2>/dev/null; then
    echo "  PASS: $name"
    PASS=$((PASS + 1))
  else
    echo "  FAIL: $name (pattern '$pattern' not found)"
    FAIL=$((FAIL + 1))
  fi
}

check_not() {
  local name="$1"
  local pattern="$2"
  local file="$3"
  if ! grep -q "$pattern" "$file" 2>/dev/null; then
    echo "  PASS: $name"
    PASS=$((PASS + 1))
  else
    echo "  FAIL: $name (pattern '$pattern' should NOT be present)"
    FAIL=$((FAIL + 1))
  fi
}

echo "=== Terminal Panes E2E Test ==="
echo "Binary: $BINARY"

# Check prerequisites.
if [ ! -f "$BINARY" ]; then
  echo "SKIP: wuphf binary not found at $BINARY (run: go build -o wuphf ./cmd/wuphf)"
  exit 0
fi

if ! command -v termwright &>/dev/null; then
  echo "SKIP: termwright not in PATH"
  exit 0
fi

if ! command -v claude &>/dev/null; then
  echo "SKIP: claude not in PATH (embedded mode requires claude binary)"
  exit 0
fi

# --- Test 1: Boot and verify roster ---
echo ""
echo "--- Test 1: Boot and verify pane layout ---"

termwright daemon --socket "$SOCKET" --cols 120 --rows 40 --background "$BINARY"
sleep 8  # Allow time for claude processes to start

termwright screenshot --socket "$SOCKET" --output "$ARTIFACTS/01-boot.txt" --format text
termwright screenshot --socket "$SOCKET" --output "$ARTIFACTS/01-boot.png" --format png

# Verify CEO pane is visible (leader is first pane).
check "CEO pane visible" "ceo\|CEO" "$ARTIFACTS/01-boot.txt"

# Verify at least one pane border is rendered.
check "Pane border rendered" "\\[" "$ARTIFACTS/01-boot.txt"

# --- Test 2: Verify multiple panes exist ---
echo ""
echo "--- Test 2: Multiple agent panes ---"

# Check for specialist pane slugs in the layout.
check "Multiple panes rendered" "fe\|be\|pm\|designer" "$ARTIFACTS/01-boot.txt"

# --- Test 3: Keyboard input reaches focused pane ---
echo ""
echo "--- Test 3: Input routing to focused pane ---"

# Type some text (will go to focused CEO pane).
termwright type --socket "$SOCKET" --text "hello team"
sleep 2

termwright screenshot --socket "$SOCKET" --output "$ARTIFACTS/02-after-type.txt" --format text

# The typed text should appear somewhere in the terminal.
check "Typed text appears in pane" "hello" "$ARTIFACTS/02-after-type.txt"

# --- Test 4: Clean shutdown with double Ctrl+C ---
echo ""
echo "--- Test 4: Clean shutdown ---"

# Send Ctrl+C twice.
termwright key --socket "$SOCKET" --key ctrl+c
sleep 1
termwright key --socket "$SOCKET" --key ctrl+c
sleep 3

# After shutdown, the daemon process should be gone.
if ! termwright screenshot --socket "$SOCKET" --output /dev/null --format text 2>/dev/null; then
  echo "  PASS: Process exited after double Ctrl+C"
  PASS=$((PASS + 1))
else
  echo "  FAIL: Process still running after double Ctrl+C"
  FAIL=$((FAIL + 1))
fi

echo ""
echo "=== Terminal Panes E2E Complete ==="
