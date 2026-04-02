#!/bin/bash
set -euo pipefail

SOCKET="/tmp/wuphf-1o1-$$.sock"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BINARY="$ROOT/wuphf"
ARTIFACTS="$ROOT/termwright-artifacts/one-on-one-channel-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$ARTIFACTS"

WUPHF_PID=""
PHASE=""

cleanup() {
  pkill -f "termwright daemon.*$SOCKET" 2>/dev/null || true
  rm -f "$SOCKET"
  "$BINARY" kill >/dev/null 2>&1 || true
  if [ -n "${WUPHF_PID:-}" ]; then
    kill "$WUPHF_PID" >/dev/null 2>&1 || true
    wait "$WUPHF_PID" 2>/dev/null || true
  fi
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

save_screen() {
  local label="$1"
  screen > "$ARTIFACTS/$label.txt"
}

send_raw() {
  local text="$1"
  for (( i=0; i<${#text}; i++ )); do
    local ch="${text:$i:1}"
    local b64
    b64=$(printf '%s' "$ch" | base64 | tr -d '\n')
    termwright exec --socket "$SOCKET" --method raw --params "{\"bytes_base64\":\"$b64\"}" >/dev/null 2>&1
    sleep 0.03
  done
}

send_enter() {
  termwright exec --socket "$SOCKET" --method raw --params '{"bytes_base64":"DQ=="}' >/dev/null 2>&1
}

send_ctrl_u() {
  termwright exec --socket "$SOCKET" --method raw --params '{"bytes_base64":"FQ=="}' >/dev/null 2>&1
}

send_escape() {
  termwright exec --socket "$SOCKET" --method key --params '{"key":"Escape"}' >/dev/null 2>&1
}

send_key() {
  local key="$1"
  termwright exec --socket "$SOCKET" --method key --params "{\"key\":\"$key\"}" >/dev/null 2>&1
}

assert_screen_contains() {
  local needle="$1"
  local label="$2"
  local content=""
  for _ in 1 2 3 4 5 6 7 8 9 10 11 12; do
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

assert_screen_not_contains() {
  local needle="$1"
  local label="$2"
  local content
  content="$(screen)"
  printf '%s\n' "$content" > "$ARTIFACTS/$label.txt"
  if printf '%s\n' "$content" | grep -Fq "$needle"; then
    echo "FAIL: did not expect '$needle' in $label"
    exit 1
  fi
}

wait_for_health() {
  local expected_mode="$1"
  local expected_agent="$2"
  local label="$3"
  local health_file="$ARTIFACTS/$label.json"

  for _ in 1 2 3 4 5 6 7 8 9 10 11 12; do
    curl -s http://127.0.0.1:7890/health > "$health_file" || true
    if python3 - "$health_file" "$expected_mode" "$expected_agent" <<'PY'
import json, sys
path, mode, agent = sys.argv[1:]
try:
    data = json.load(open(path))
except Exception:
    sys.exit(1)
if data.get("status") == "ok" and data.get("session_mode") == mode and data.get("one_on_one_agent") == agent:
    sys.exit(0)
sys.exit(1)
PY
    then
      return 0
    fi
    sleep 1
  done

  echo "FAIL: expected health mode=$expected_mode agent=$expected_agent in $label"
  cat "$health_file" || true
  exit 1
}

wait_for_pane_count() {
  local expected="$1"
  local label="$2"
  local pane_file="$ARTIFACTS/$label.txt"

  for _ in 1 2 3 4 5 6 7 8 9 10; do
    tmux -L wuphf list-panes -t wuphf-team:team -F '#{pane_index} #{pane_dead} #{pane_title} #{pane_current_command}' > "$pane_file" || true
    local count
    count=$(wc -l < "$pane_file" | tr -d ' ')
    if [ "$count" = "$expected" ] && awk '{ if ($2 != 0) bad=1 } END { exit bad }' "$pane_file"; then
      return 0
    fi
    sleep 1
  done

  echo "FAIL: expected $expected live panes in $label"
  cat "$pane_file" || true
  exit 1
}

start_runtime() {
  PHASE="$1"
  shift

  cleanup
  "$BINARY" "$@" > "$ARTIFACTS/$PHASE-wuphf-stdout.txt" 2> "$ARTIFACTS/$PHASE-wuphf-stderr.txt" &
  WUPHF_PID=$!
  sleep 8
  local attached=false
  for _ in 1 2 3; do
    if termwright daemon --socket "$SOCKET" --cols 120 --rows 40 --background -- tmux -L wuphf attach -t wuphf-team >/dev/null 2>&1; then
      attached=true
      break
    fi
    pkill -f "termwright daemon.*$SOCKET" 2>/dev/null || true
    rm -f "$SOCKET"
    sleep 2
  done
  if [ "$attached" != true ]; then
    echo "FAIL: termwright could not attach during $PHASE"
    exit 1
  fi
  sleep 3
}

echo "=== One-on-One Channel E2E ==="
echo "Binary: $BINARY"
echo "Artifacts: $ARTIFACTS"

echo "--- Phase 1: Direct boot with explicit agent ---"
start_runtime "phase1" --1o1 pm
wait_for_health "1o1" "pm" "phase1-health"
wait_for_pane_count 2 "phase1-panes"
assert_screen_contains "1:1 with Product Manager" "phase1-boot"
assert_screen_contains "Talk directly to your agent here..." "phase1-boot"
assert_screen_not_contains "The WUPHF Office" "phase1-boot"
assert_screen_not_contains "Channels" "phase1-boot"

send_raw "/"
sleep 1
assert_screen_contains "/1o1" "phase1-autocomplete"
assert_screen_contains "/reset" "phase1-autocomplete"
assert_screen_not_contains "/channels" "phase1-autocomplete"
send_ctrl_u

send_raw "/channels"
send_enter
sleep 2
assert_screen_contains "1:1 mode disables office collaboration commands." "phase1-blocked"
send_ctrl_u

echo "--- Phase 2: Invalid agent falls back to CEO ---"
send_raw "/1o1 ghost"
send_enter
wait_for_health "1o1" "ceo" "phase2-health"
wait_for_pane_count 2 "phase2-panes"
assert_screen_contains "1:1 with CEO" "phase2-screen"
assert_screen_contains "Direct 1:1 with CEO" "phase2-screen"

echo "--- Phase 3: Switch back to PM and verify message isolation ---"
send_raw "/1o1 pm"
send_enter
wait_for_health "1o1" "pm" "phase3-health"
wait_for_pane_count 2 "phase3-panes"
assert_screen_contains "1:1 with Product Manager" "phase3-screen"

TOKEN=$(cat /tmp/wuphf-broker-token 2>/dev/null || echo "")
if [ -z "$TOKEN" ]; then
  echo "FAIL: missing broker token"
  exit 1
fi
curl -s -X POST http://127.0.0.1:7890/messages \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"from":"ai","content":"AI should be hidden in PM mode."}' >/dev/null
curl -s -X POST http://127.0.0.1:7890/messages \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"from":"pm","content":"PM visible check."}' >/dev/null
sleep 3
assert_screen_contains "PM visible check." "phase3-isolation"
assert_screen_not_contains "AI should be hidden in PM mode." "phase3-isolation"

echo "--- Phase 4: Reset preserves direct mode and selected agent ---"
send_raw "/reset"
send_enter
wait_for_health "1o1" "pm" "phase4-health"
wait_for_pane_count 2 "phase4-panes"
assert_screen_contains "1:1 with Product Manager" "phase4-screen"
assert_screen_contains "Direct 1:1 with Product Manager." "phase4-screen"

echo "--- Phase 5: Disable 1:1 mode from the picker ---"
send_raw "/1o1"
send_enter
sleep 1
assert_screen_contains "Enable 1:1 mode" "phase5-picker"
assert_screen_contains "Disable 1:1 mode" "phase5-picker"
send_raw "2"
wait_for_health "office" "ceo" "phase5-health"
wait_for_pane_count 6 "phase5-panes"
assert_screen_contains "The WUPHF Office" "phase5-screen"
assert_screen_contains "Message #general" "phase5-screen"
assert_screen_not_contains "1:1 with Product Manager" "phase5-screen"

echo "--- Phase 6: Live switch from office into 1:1 mode ---"
send_raw "/1o1 be"
send_enter
wait_for_health "1o1" "be" "phase6-health"
wait_for_pane_count 2 "phase6-panes"
assert_screen_contains "1:1 with Backend Engineer" "phase6-screen"
assert_screen_not_contains "The WUPHF Office" "phase6-screen"

echo "--- Phase 7: Fresh default launch returns to office ---"
start_runtime "phase7"
wait_for_health "office" "ceo" "phase7-health"
assert_screen_contains "The WUPHF Office" "phase7-screen"
assert_screen_contains "Message #general" "phase7-screen"
assert_screen_not_contains "1:1 with Backend Engineer" "phase7-screen"

echo "--- Phase 8: Direct launch without an agent defaults to CEO ---"
start_runtime "phase8" --1o1
wait_for_health "1o1" "ceo" "phase8-health"
wait_for_pane_count 2 "phase8-panes"
assert_screen_contains "1:1 with CEO" "phase8-screen"

echo "PASS: one-on-one runtime behaves like a real direct session"
