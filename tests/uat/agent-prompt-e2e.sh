#!/bin/bash
set -euo pipefail

BINARY="$(cd "$(dirname "$0")/../.." && pwd)/wuphf"
ARTIFACTS="$(cd "$(dirname "$0")/../.." && pwd)/termwright-artifacts/agent-prompt-$(date +%Y%m%d-%H%M%S)"
SOCKET="/tmp/wuphf-agent-prompt-$$.sock"
STUB='{"slug":"devrel","name":"Developer Relations","role":"Developer Relations","expertise":["developer relations","technical content","feedback loops"],"personality":"Developer-relations operator who turns product work into community momentum and brings real user feedback back to the office.","permission_mode":"plan"}'
mkdir -p "$ARTIFACTS"

cleanup() {
  pkill -f "termwright daemon.*$SOCKET" 2>/dev/null || true
  rm -f "$SOCKET"
  "$BINARY" kill >/dev/null 2>&1 || true
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
    sleep 0.02
  done
}

assert_contains() {
  local needle="$1"
  local label="$2"
  local content=""
  for _ in 1 2 3 4 5 6 7 8 9 10; do
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

echo "=== Agent Prompt E2E ==="
echo "Binary: $BINARY"
echo "Artifacts: $ARTIFACTS"

"$BINARY" >"$ARTIFACTS/full-stdout.txt" 2>"$ARTIFACTS/full-stderr.txt" &
APP_PID=$!
sleep 8

BROKER_TOKEN=$(cat /tmp/wuphf-broker-token 2>/dev/null || true)
if [ -z "$BROKER_TOKEN" ]; then
  echo "FAIL: missing broker token"
  exit 1
fi

termwright daemon --socket "$SOCKET" --cols 120 --rows 40 --background -- env \
  WUPHF_BROKER_TOKEN="$BROKER_TOKEN" \
  WUPHF_AGENT_TEMPLATE_STUB="$STUB" \
  "$BINARY" --channel-view
sleep 5

assert_contains "# general" "boot"
assert_contains "Message #general" "boot"

send_raw "/agent prompt someone who owns devrel and technical content"
termwright exec --socket "$SOCKET" --method raw --params '{"bytes_base64":"DQ=="}' >/dev/null 2>&1

assert_contains "Developer Relations" "after-prompt"

MEMBERS_JSON=$(curl -s -H "Authorization: Bearer $BROKER_TOKEN" http://127.0.0.1:7890/office-members)
printf '%s\n' "$MEMBERS_JSON" > "$ARTIFACTS/office-members.json"
if ! printf '%s\n' "$MEMBERS_JSON" | grep -Fq '"slug":"devrel"'; then
  echo "FAIL: devrel member not persisted"
  exit 1
fi

CHANNEL_JSON=$(curl -s -H "Authorization: Bearer $BROKER_TOKEN" http://127.0.0.1:7890/channel-members?channel=general)
printf '%s\n' "$CHANNEL_JSON" > "$ARTIFACTS/channel-members.json"
if ! printf '%s\n' "$CHANNEL_JSON" | grep -Fq '"slug":"devrel"'; then
  echo "FAIL: devrel not added to #general"
  exit 1
fi

kill "$APP_PID" >/dev/null 2>&1 || true
wait "$APP_PID" >/dev/null 2>&1 || true

echo "PASS: /agent prompt creates a teammate and adds them to the office channel"
