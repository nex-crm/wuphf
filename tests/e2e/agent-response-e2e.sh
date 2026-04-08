#!/bin/bash
# E2E test: verify agents actually respond to messages
# Tests message delivery → agent processing → response appears in channel

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TERMWRIGHT="${TERMWRIGHT:-$(command -v termwright || true)}"
if [ -z "$TERMWRIGHT" ]; then
  echo "termwright not found in PATH; set TERMWRIGHT=/abs/path/to/termwright" >&2
  exit 1
fi
WUPHF="${WUPHF_BIN:-$REPO_ROOT/wuphf}"
ARTIFACTS="${ARTIFACTS:-$REPO_ROOT/termwright-artifacts/agent-response-$(date +%Y%m%d-%H%M%S)}"
mkdir -p "$ARTIFACTS"

SOCKET="/tmp/wuphf-agent-resp-$$.sock"
DAEMON_PID=""

cleanup() {
  [ -n "$DAEMON_PID" ] && kill "$DAEMON_PID" 2>/dev/null
  tmux -L wuphf kill-session -t wuphf-team 2>/dev/null || true
  rm -f "$SOCKET"
  sleep 2
}
trap cleanup EXIT

echo "=== Agent Response E2E Test ==="
echo "Artifacts: $ARTIFACTS"

# Start fresh
cleanup
rm -f ~/.wuphf/team/broker-state.json 2>/dev/null

"$TERMWRIGHT" daemon --socket "$SOCKET" --cols 140 --rows 40 -- "$WUPHF" -no-nex 2>/dev/null &
DAEMON_PID=$!
sleep 22

BROKER_TOKEN=$(cat /tmp/wuphf-broker-token)
echo "Broker token: ${BROKER_TOKEN:0:8}..."

# Verify broker is alive
HEALTH=$(curl -s "http://127.0.0.1:7890/health" 2>/dev/null)
echo "Broker health: $HEALTH"

# Verify agents are running
echo ""
echo "=== Agent Panes ==="
tmux -L wuphf list-panes -t wuphf-team -F "pane #{pane_index}: #{pane_current_command}" 2>/dev/null

echo ""
echo "=== TEST 1: Untagged message (CEO should triage) ==="
curl -s -X POST "http://127.0.0.1:7890/messages" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $BROKER_TOKEN" \
  -d '{"channel":"general","from":"you","content":"What is the team working on right now?"}' 2>/dev/null

echo "Message sent. Waiting for CEO response..."

# Poll for 60 seconds checking if any agent responded
RESPONDED=false
for i in $(seq 1 30); do
  sleep 2

  # Check messages for any non-human response
  MSGS=$(curl -s "http://127.0.0.1:7890/messages?channel=general&limit=10" \
    -H "Authorization: Bearer $BROKER_TOKEN" 2>/dev/null)

  AGENT_REPLIES=$(echo "$MSGS" | python3 -c "
import json,sys
d=json.load(sys.stdin)
replies = [m for m in d.get('messages',[]) if m['from'] not in ('you','human','system')]
for r in replies[-3:]:
    print(f'  @{r[\"from\"]}: {r[\"content\"][:80]}')
" 2>/dev/null)

  if [ -n "$AGENT_REPLIES" ]; then
    echo "RESPONSE at ${i}*2s:"
    echo "$AGENT_REPLIES"
    RESPONDED=true
    "$TERMWRIGHT" exec --socket "$SOCKET" --method screenshot \
      --params "{\"path\":\"$ARTIFACTS/test1-response.png\"}" 2>/dev/null
    break
  fi

  # Check pane activity
  PANE1=$(tmux -L wuphf capture-pane -p -J -S -3 -t wuphf-team:team.1 2>/dev/null | grep -v '^$' | tail -1)
  echo "  poll $i (${i}*2s): pane1=[$PANE1]"
done

if [ "$RESPONDED" = true ]; then
  echo "PASS: Agent responded to untagged message"
else
  echo "FAIL: No agent response after 60s"
  # Dump diagnostics
  echo ""
  echo "=== Diagnostics ==="
  echo "--- CEO pane (last 10 lines) ---"
  tmux -L wuphf capture-pane -p -J -S -10 -t wuphf-team:team.1 2>/dev/null
  echo ""
  echo "--- All messages ---"
  curl -s "http://127.0.0.1:7890/messages?channel=general&limit=20" \
    -H "Authorization: Bearer $BROKER_TOKEN" 2>/dev/null | python3 -c "
import json,sys
d=json.load(sys.stdin)
for m in d.get('messages',[]):
    print(f'  {m[\"id\"]} @{m[\"from\"]} [{m.get(\"kind\",\"\")}]: {m[\"content\"][:100]}')
" 2>/dev/null
  echo ""
  echo "--- Pending requests ---"
  curl -s "http://127.0.0.1:7890/requests?channel=general" \
    -H "Authorization: Bearer $BROKER_TOKEN" 2>/dev/null
  echo ""
  echo "--- Members ---"
  curl -s "http://127.0.0.1:7890/members?channel=general" \
    -H "Authorization: Bearer $BROKER_TOKEN" 2>/dev/null | python3 -c "
import json,sys
d=json.load(sys.stdin)
for m in d.get('members',[]):
    print(f'  {m[\"slug\"]}: last={m.get(\"lastMessage\",\"\")[:40]} time={m.get(\"lastTime\",\"\")} live={m.get(\"liveActivity\",\"\")}')
" 2>/dev/null
fi

echo ""
echo "=== TEST 2: Direct @ceo mention ==="
curl -s -X POST "http://127.0.0.1:7890/messages" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $BROKER_TOKEN" \
  -d '{"channel":"general","from":"you","content":"@ceo give me a one sentence status update","tagged":["ceo"]}' 2>/dev/null

RESPONDED2=false
for i in $(seq 1 30); do
  sleep 2
  MSGS=$(curl -s "http://127.0.0.1:7890/messages?channel=general&limit=5" \
    -H "Authorization: Bearer $BROKER_TOKEN" 2>/dev/null)

  CEO_REPLY=$(echo "$MSGS" | python3 -c "
import json,sys
d=json.load(sys.stdin)
for m in d.get('messages',[]):
    if m['from'] == 'ceo' and 'status' in m.get('content','').lower():
        print(m['content'][:120])
        break
" 2>/dev/null)

  if [ -n "$CEO_REPLY" ]; then
    echo "CEO RESPONSE at ${i}*2s: $CEO_REPLY"
    RESPONDED2=true
    "$TERMWRIGHT" exec --socket "$SOCKET" --method screenshot \
      --params "{\"path\":\"$ARTIFACTS/test2-ceo-response.png\"}" 2>/dev/null
    break
  fi

  PANE1=$(tmux -L wuphf capture-pane -p -J -S -2 -t wuphf-team:team.1 2>/dev/null | grep -v '^$' | tail -1)
  echo "  poll $i: pane=[$PANE1]"
done

if [ "$RESPONDED2" = true ]; then
  echo "PASS: CEO responded to direct mention"
else
  echo "FAIL: CEO did not respond to direct mention after 60s"
  echo "--- CEO pane ---"
  tmux -L wuphf capture-pane -p -J -S -15 -t wuphf-team:team.1 2>/dev/null
fi

echo ""
echo "=== TEST 3: Direct @fe mention (should skip CEO via direct routing) ==="
# Check which pane is FE
echo "Agent panes:"
tmux -L wuphf list-panes -t wuphf-team -F "  pane #{pane_index}" 2>/dev/null

curl -s -X POST "http://127.0.0.1:7890/messages" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $BROKER_TOKEN" \
  -d '{"channel":"general","from":"you","content":"@fe what frontend framework are you using?","tagged":["fe"]}' 2>/dev/null

RESPONDED3=false
for i in $(seq 1 30); do
  sleep 2
  MSGS=$(curl -s "http://127.0.0.1:7890/messages?channel=general&limit=5" \
    -H "Authorization: Bearer $BROKER_TOKEN" 2>/dev/null)

  FE_REPLY=$(echo "$MSGS" | python3 -c "
import json,sys
d=json.load(sys.stdin)
for m in d.get('messages',[]):
    if m['from'] == 'fe':
        print(m['content'][:120])
        break
" 2>/dev/null)

  if [ -n "$FE_REPLY" ]; then
    echo "FE RESPONSE at ${i}*2s: $FE_REPLY"
    RESPONDED3=true
    break
  fi
  echo "  poll $i: waiting..."
done

if [ "$RESPONDED3" = true ]; then
  echo "PASS: FE responded directly (bypassed CEO)"
else
  echo "FAIL: FE did not respond after 60s"
  echo "--- FE pane (pane 3) ---"
  tmux -L wuphf capture-pane -p -J -S -15 -t wuphf-team:team.3 2>/dev/null
fi

# Final screenshot
"$TERMWRIGHT" exec --socket "$SOCKET" --method screenshot \
  --params "{\"path\":\"$ARTIFACTS/final-state.png\"}" 2>/dev/null

echo ""
echo "=== SUMMARY ==="
echo "Test 1 (untagged → CEO triage): $([ "$RESPONDED" = true ] && echo 'PASS' || echo 'FAIL')"
echo "Test 2 (direct @ceo): $([ "$RESPONDED2" = true ] && echo 'PASS' || echo 'FAIL')"
echo "Test 3 (direct @fe, skip CEO): $([ "$RESPONDED3" = true ] && echo 'PASS' || echo 'FAIL')"
echo "Artifacts: $ARTIFACTS"
