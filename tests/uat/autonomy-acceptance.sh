#!/bin/bash
# Focused acceptance run for the autonomy contract.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BINARY="$ROOT/wuphf"
ARTIFACTS="$ROOT/termwright-artifacts/autonomy-acceptance-$(date +%Y%m%d-%H%M%S)"
SOCKET="/tmp/wuphf-acceptance-$$.sock"
mkdir -p "$ARTIFACTS"

cleanup() {
  pkill -f "termwright daemon.*$SOCKET" 2>/dev/null || true
  "$BINARY" shred >/dev/null 2>&1 || true
  rm -f "$SOCKET"
}
trap cleanup EXIT

stop_channel_view() {
  pkill -f "termwright daemon.*$SOCKET" 2>/dev/null || true
  rm -f "$SOCKET"
}

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

capture_channel_pane() {
  local label="$1"
  tmux -L wuphf capture-pane -p -t wuphf-team:team.0 > "$ARTIFACTS/$label.txt"
}

pane_contains() {
  local needle="$1"
  local label="$2"
  for _ in 1 2 3 4 5 6 7 8; do
    capture_channel_pane "$label"
    if grep -Fq "$needle" "$ARTIFACTS/$label.txt"; then
      return 0
    fi
    sleep 1
  done
  echo "FAIL: expected '$needle' in $label"
  exit 1
}

send_channel_line() {
  local text="$1"
  tmux -L wuphf send-keys -t wuphf-team:team.0 C-c
  sleep 0.2
  tmux -L wuphf send-keys -t wuphf-team:team.0 "$text" Enter
  sleep 1
}

raw_bytes() {
  local b64="$1"
  termwright exec --socket "$SOCKET" --method raw --params "{\"bytes_base64\":\"$b64\"}" >/dev/null 2>&1
}

send_raw() {
  local text="$1"
  for (( i=0; i<${#text}; i++ )); do
    local ch="${text:$i:1}"
    local b64
    b64=$(printf '%s' "$ch" | base64)
    raw_bytes "$b64"
    sleep 0.03
  done
}

send_enter() {
  raw_bytes "$(printf '\r' | base64)"
  sleep 0.3
}

send_escape() {
  raw_bytes "$(printf '\033' | base64)"
  sleep 0.3
}

send_ctrl_o() {
  raw_bytes "$(printf '\017' | base64)"
  sleep 0.3
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

start_channel_view() {
  local app="${1:-messages}"
  stop_channel_view
  termwright daemon --socket "$SOCKET" --cols 120 --rows 40 --background -- "$BINARY" --channel-view --channel-app "$app" >/dev/null
  sleep 6
}

echo "=== WUPHF Autonomy Acceptance ==="
echo "Artifacts: $ARTIFACTS"

echo "--- Focused Go checks ---"
go test ./internal/team -run 'TestPersistOfficeSignalsCreatesOwnedTaskAndLedger|TestPersistHumanDirectiveRecordsLedger|TestRecordWatchdogLedgerCreatesSignalAndDecision|TestTaskNotificationTargetsWakeOwnerOnWatchdog|TestTaskNotificationTargetsDoNotRewakeCEOForOwnCreatedTask'
go test ./internal/teammcp -run 'TestSuppressBroadcastReason'
go test ./cmd/wuphf -run 'TestHumanFacingMessageSwitchesBackToMessages|TestChannelViewRendersHumanFacingMessageCard|TestInsightsViewRendersSignalsDecisionsAndWatchdogs|TestBlockingRequestSwitchesBackToMessages|TestBlockingRequestCannotBeSnoozedWithEsc|TestBlockingRequestCannotBeSnoozedByCommand|TestCalendarViewRendersSchedulerAndActions'

echo "--- Build current binary ---"
go build -o "$BINARY" ./cmd/wuphf

echo "--- Launch live office ---"
"$BINARY" >/dev/null 2>"$ARTIFACTS/wuphf-stderr.txt" &
sleep 8
BROKER_TOKEN="$(cat /tmp/wuphf-broker-token)"

tmux -L wuphf has-session -t wuphf-team
curl -s -H "Authorization: Bearer $BROKER_TOKEN" http://127.0.0.1:7890/health > "$ARTIFACTS/health.json"
curl -s -X POST -H "Authorization: Bearer $BROKER_TOKEN" http://127.0.0.1:7890/reset > "$ARTIFACTS/reset.json"
sleep 5

echo "--- Create blocking request ---"
REQUEST=$(curl -s -X POST http://127.0.0.1:7890/requests \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $BROKER_TOKEN" \
  -d '{"kind":"approval","from":"ceo","channel":"general","title":"Approval needed","question":"Ship the current direction?","blocking":true,"required":true,"reply_to":"msg-1"}')
printf '%s\n' "$REQUEST" > "$ARTIFACTS/request-create.json"
REQ_ID=$(printf '%s\n' "$REQUEST" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))')
sleep 3
tmux -L wuphf capture-pane -p -t wuphf-team:team.0 > "$ARTIFACTS/channel-blocking.txt"
grep -Eq 'Human decision needed|Approval needed|Ship the current direction|Answer @ceo|Request pending' "$ARTIFACTS/channel-blocking.txt"
HTTP_CODE=$(curl -s -o "$ARTIFACTS/request-blocked-response.txt" -w "%{http_code}" -X POST http://127.0.0.1:7890/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $BROKER_TOKEN" \
  -d '{"from":"you","content":"this should be blocked"}')
test "$HTTP_CODE" = "409"

echo "--- Answer blocking request and inject human-facing note ---"
curl -s -X POST http://127.0.0.1:7890/requests/answer \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $BROKER_TOKEN" \
  -d "{\"id\":\"$REQ_ID\",\"choice_id\":\"approve_with_note\",\"custom_text\":\"Proceed, but keep the scope narrow and the plan reviewable.\"}" > "$ARTIFACTS/request-answer.json"
sleep 2
curl -s -X POST http://127.0.0.1:7890/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $BROKER_TOKEN" \
  -d '{"from":"pm","channel":"general","kind":"human_report","title":"V1 for you","content":"Keep v1 to searchable notes, shareable summaries, and crisp follow-up tasks."}' > "$ARTIFACTS/human-message.json"
sleep 3
tmux -L wuphf capture-pane -p -t wuphf-team:team.0 > "$ARTIFACTS/channel-human-facing.txt"
grep -Eq 'V1 for you|has something for you' "$ARTIFACTS/channel-human-facing.txt"

echo "--- Inject explicit human directive ---"
curl -s -X POST http://127.0.0.1:7890/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $BROKER_TOKEN" \
  -d '{"from":"you","channel":"general","content":"CEO, triage this request first and have PM suggest the tightest v1 scope.","tagged":["ceo","pm"]}' > "$ARTIFACTS/human-directive.json"
sleep 3
SIGNAL_ID=$(curl -s -H "Authorization: Bearer $BROKER_TOKEN" http://127.0.0.1:7890/signals | python3 -c 'import sys,json; data=json.load(sys.stdin); signals=data.get("signals",[]); print(signals[-1]["id"] if signals else "")')
DECISION_ID=$(curl -s -H "Authorization: Bearer $BROKER_TOKEN" http://127.0.0.1:7890/decisions | python3 -c 'import sys,json; data=json.load(sys.stdin); decisions=data.get("decisions",[]); print(decisions[-1]["id"] if decisions else "")')
curl -s -X POST http://127.0.0.1:7890/tasks \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $BROKER_TOKEN" \
  -d "{\"action\":\"create\",\"channel\":\"general\",\"title\":\"Tighten v1 scope\",\"details\":\"Own the initial scope cut and keep it focused.\",\"owner\":\"pm\",\"created_by\":\"ceo\",\"thread_id\":\"msg-1\",\"task_type\":\"feature\",\"source_signal_id\":\"$SIGNAL_ID\",\"source_decision_id\":\"$DECISION_ID\"}" > "$ARTIFACTS/task-create.json"
sleep 2

echo "--- Create bridge target and bridge context ---"
curl -s -X POST http://127.0.0.1:7890/channels \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $BROKER_TOKEN" \
  -d '{"action":"create","slug":"launch","name":"Launch","description":"Launch planning, messaging, and rollout work.","members":["pm","cmo"],"created_by":"ceo"}' > "$ARTIFACTS/create-launch.json"
curl -s -X POST http://127.0.0.1:7890/bridges \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $BROKER_TOKEN" \
  -d '{"actor":"ceo","source_channel":"general","target_channel":"launch","summary":"Use the sharper product narrative from #general before drafting launch messaging.","tagged":["cmo"]}' > "$ARTIFACTS/bridge.json"

echo "--- Live office surface checks ---"
curl -s -H "Authorization: Bearer $BROKER_TOKEN" "http://127.0.0.1:7890/messages?limit=100&channel=launch" > "$ARTIFACTS/launch-messages.json"
grep -Fq "Bridge from #general" "$ARTIFACTS/launch-messages.json"

start_channel_view messages
assert_contains "# general" "messages-general"
assert_contains "Message #general" "messages-general"

start_channel_view tasks
assert_contains "Triggered by signal" "tasks-view"
assert_contains "stage" "tasks-view"

start_channel_view insights
assert_contains "Human directive" "insights-general"
assert_contains "Decisions" "insights-general"
assert_contains "bridge_channel" "insights-general"

start_channel_view calendar
assert_contains "task_created" "calendar-view"

echo "PASS: autonomy acceptance criteria focused run succeeded"
