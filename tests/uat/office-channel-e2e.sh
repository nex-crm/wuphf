#!/bin/bash
# Focused termwright smoke test for the office channel REPL.
# This targets --channel-view directly, which is the reliable capture surface.
set -euo pipefail

SOCKET="/tmp/wuphf-office-$$.sock"
BINARY="$(cd "$(dirname "$0")/../.." && pwd)/wuphf"
ARTIFACTS="$(cd "$(dirname "$0")/../.." && pwd)/termwright-artifacts/office-channel-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$ARTIFACTS"

cleanup() {
  pkill -f "termwright daemon.*$SOCKET" 2>/dev/null || true
  rm -f "$SOCKET"
}
trap cleanup EXIT

if [ ! -x "$BINARY" ]; then
  echo "SKIP: wuphf binary not found at $BINARY"
  exit 0
fi

if ! command -v termwright >/dev/null 2>&1; then
  echo "SKIP: termwright not installed"
  exit 0
fi

screen() {
  termwright exec --socket "$SOCKET" --method screen --params '{}' 2>/dev/null | \
    python3 -c "import sys,json; print(json.load(sys.stdin).get('result',''))"
}

send_raw() {
  local text="$1"
  for (( i=0; i<${#text}; i++ )); do
    local ch="${text:$i:1}"
    local b64
    b64=$(printf '%s' "$ch" | base64)
    termwright exec --socket "$SOCKET" --method raw --params "{\"bytes_base64\":\"$b64\"}" >/dev/null 2>&1
    sleep 0.03
  done
}

assert_contains() {
  local needle="$1"
  local label="$2"
  local content=""
  for _ in 1 2 3 4 5 6 7 8; do
    content="$(screen)"
    printf '%s\n' "$content" > "$ARTIFACTS/$label.txt"
    if printf '%s\n' "$content" | grep -Fq "$needle"; then
      return 0
    fi
    sleep 1
  done
  echo "FAIL: expected '$needle' in $label"
  exit 1
}

echo "=== Office Channel E2E ==="
echo "Binary: $BINARY"
echo "Artifacts: $ARTIFACTS"

termwright daemon --socket "$SOCKET" --cols 120 --rows 40 --background -- "$BINARY" --channel-view
sleep 5

assert_contains "# general" "boot"
assert_contains "Founding Team" "boot"
assert_contains "Message #general" "boot"

send_raw "/"
sleep 1
assert_contains "/integrate" "slash-autocomplete"
assert_contains "/reset" "slash-autocomplete"

send_raw "hello team"
sleep 1
assert_contains "hello team" "typed-input"

echo "PASS: office channel renders and accepts termwright input"
