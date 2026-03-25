#!/bin/bash
# E2E Test: "Let's build an AI notetaker company"
# Tests the single-window tmux team flow: broker, channel, live agent panes, and agent replies.
set -e

BINARY="$(cd "$(dirname "$0")/../.." && pwd)/wuphf"
ARTIFACTS="$(cd "$(dirname "$0")/../.." && pwd)/termwright-artifacts/notetaker-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$ARTIFACTS"

TOTAL=0; PASSED=0; FAILED=0
log_test() { TOTAL=$((TOTAL + 1)); echo "  [$TOTAL] $1"; }
pass() { PASSED=$((PASSED + 1)); echo "    PASS"; }
fail() { FAILED=$((FAILED + 1)); echo "    FAIL: $1"; }

echo "============================================"
echo "  AI Notetaker Company — E2E Test"
echo "============================================"

echo "--- Phase 1: Launch ---"

log_test "Binary exists"
if [ -x "$BINARY" ]; then pass; else fail "not found"; exit 1; fi

log_test "Version"
if "$BINARY" --version 2>&1 | grep -q "wuphf v"; then pass; else fail "bad version"; fi

log_test "Start wuphf"
"$BINARY" >"$ARTIFACTS/stdout.txt" 2>"$ARTIFACTS/stderr.txt" &
NEX_PID=$!
sleep 8
pass

log_test "Broker running"
BROKER_OK=false
for i in 1 2 3 4 5; do
  if curl -s http://127.0.0.1:7890/health 2>/dev/null | grep -q "ok"; then
    BROKER_OK=true
    break
  fi
  sleep 2
done
if $BROKER_OK; then pass; else fail "broker down"; fi

# Read broker auth token from temp file
BROKER_TOKEN=$(cat /tmp/wuphf-broker-token 2>/dev/null || echo "")
if [ -z "$BROKER_TOKEN" ]; then
  echo "    WARNING: no broker token found, authenticated requests will fail"
fi

log_test "tmux session exists"
if tmux -L wuphf list-sessions 2>&1 | grep -q "wuphf-team"; then pass; else fail "no session"; fi

log_test "Single team window exists"
WIN_NAMES=$(tmux -L wuphf list-windows -t wuphf-team -F "#{window_name}" 2>&1)
echo "$WIN_NAMES" > "$ARTIFACTS/windows.txt"
if echo "$WIN_NAMES" | grep -q "^team$"; then
  pass
  echo "    Windows: $(echo "$WIN_NAMES" | tr '\n' ', ')"
else
  fail "unexpected windows: $WIN_NAMES"
fi

log_test "Team window has channel + agent panes"
TOTAL_PANES=$(tmux -L wuphf list-panes -t wuphf-team:team 2>&1 | wc -l | tr -d ' ')
if [ "$TOTAL_PANES" -ge 5 ]; then
  pass
  echo "    $TOTAL_PANES panes"
else
  fail "only $TOTAL_PANES panes"
fi

echo "--- Phase 2: Channel View ---"

log_test "Channel pane running"
CHAN=$(tmux -L wuphf capture-pane -p -t wuphf-team:team.0 2>&1)
echo "$CHAN" > "$ARTIFACTS/channel-boot.txt"
if echo "$CHAN" | grep -qi "channel\|waiting\|wuphf\|team"; then pass; else fail "empty"; fi

log_test "Channel has input area"
if echo "$CHAN" | grep -qi "type\|message\|quit"; then pass; else fail "no input"; fi

echo "--- Phase 3: Post to Channel ---"

log_test "Post via broker API"
RESULT=$(curl -s -X POST http://127.0.0.1:7890/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $BROKER_TOKEN" \
  -d '{"from":"you","content":"Let'\''s build an AI notetaker company. @ceo what'\''s our strategy? @pm what features should v1 have?","tagged":["ceo","pm"]}')
if echo "$RESULT" | grep -q "id"; then pass; else fail "$RESULT"; fi

log_test "Message in broker"
sleep 1
MSGS=$(curl -s -H "Authorization: Bearer $BROKER_TOKEN" http://127.0.0.1:7890/messages?limit=5)
echo "$MSGS" > "$ARTIFACTS/broker-messages.txt"
if echo "$MSGS" | grep -q "notetaker"; then pass; else fail "not stored"; fi

log_test "Channel shows message"
sleep 3
CHAN2=$(tmux -L wuphf capture-pane -p -t wuphf-team:team.0 2>&1)
echo "$CHAN2" > "$ARTIFACTS/channel-after-post.txt"
if echo "$CHAN2" | grep -qi "notetaker\|you\|strategy"; then pass; else fail "not visible"; fi

echo "--- Phase 4: Agent Panes ---"

log_test "Visible agent panes boot"
echo "    Waiting up to 20s for Claude boot..."
BOOTED=0
for attempt in 1 2 3 4 5; do
  BOOTED=0
  for i in 1 2 3 4; do
    PANE_CMD=$(tmux -L wuphf display-message -p -t "wuphf-team:team.$i" "#{pane_current_command}" 2>/dev/null || echo "")
    CONTENT=$(tmux -L wuphf capture-pane -p -t "wuphf-team:team.$i" 2>/dev/null || echo "")
    echo "$CONTENT" > "$ARTIFACTS/pane-$i.txt"
    if echo "$CONTENT" | grep -qi "trust this folder"; then
      continue
    fi
    if echo "$PANE_CMD" | grep -qi "2.1.81\|claude"; then
      BOOTED=$((BOOTED + 1))
    fi
  done
  if [ "$BOOTED" -ge 3 ]; then break; fi
  sleep 4
done
if [ "$BOOTED" -ge 3 ]; then
  pass
  echo "    $BOOTED/4 visible agents booted"
else
  fail "$BOOTED/4 visible agents booted"
fi

log_test "No visible agent is stuck on trust prompt"
TRUST_STUCK=0
for i in 1 2 3 4; do
  CONTENT=$(tmux -L wuphf capture-pane -p -t "wuphf-team:team.$i" 2>/dev/null || echo "")
  if echo "$CONTENT" | grep -qi "trust this folder"; then
    TRUST_STUCK=$((TRUST_STUCK + 1))
  fi
done
if [ "$TRUST_STUCK" -eq 0 ]; then pass; else fail "$TRUST_STUCK panes blocked on trust"; fi

echo "--- Phase 5: Collaboration ---"

log_test "Notification loop reaches an agent pane (best effort)"
sleep 5
NOTIFIED=0
for i in 1 2 3 4; do
  CONTENT=$(tmux -L wuphf capture-pane -p -t "wuphf-team:team.$i" 2>/dev/null || echo "")
  if echo "$CONTENT" | grep -qi "Channel update\|notetaker\|team_poll"; then
    NOTIFIED=$((NOTIFIED + 1))
  fi
done
if [ "$NOTIFIED" -ge 1 ]; then
  pass
  echo "    $NOTIFIED panes show notification text"
else
  pass
  echo "    No pane text visible yet, continuing based on broker replies"
fi

log_test "At least one agent replies in the broker"
AGENT_REPLIED=false
for i in 1 2 3 4 5 6 7 8 9 10 11 12; do
  FINAL=$(curl -s -H "Authorization: Bearer $BROKER_TOKEN" http://127.0.0.1:7890/messages?limit=100 2>/dev/null)
  echo "$FINAL" > "$ARTIFACTS/broker-final.json"
  if echo "$FINAL" | python3 -c "import sys,json; msgs=json.load(sys.stdin).get('messages',[]); print('yes' if any(m.get('from') not in ('you','') for m in msgs) else 'no')" 2>/dev/null | grep -q yes; then
    AGENT_REPLIED=true
    break
  fi
  sleep 5
done
if $AGENT_REPLIED; then
  pass
else
  fail "no agent reply observed in broker"
fi

log_test "Channel eventually shows an agent reply"
sleep 3
CHAN3=$(tmux -L wuphf capture-pane -p -t wuphf-team:team.0 2>&1)
echo "$CHAN3" > "$ARTIFACTS/channel-final.txt"
if echo "$CHAN3" | grep -qi "@ceo\|@pm\|@fe\|@be\|@designer\|@cmo\|@cro"; then
  pass
else
  fail "channel never rendered an agent reply"
fi

echo "--- Phase 6: Quality Checks ---"

log_test "No raw JSON in channel"
if echo "$CHAN3" | grep -qE '^\{.*"type"'; then fail "raw JSON"; else pass; fi

log_test "No panics/tracebacks"
if echo "$CHAN3" | grep -qi "panic:\|traceback\|goroutine"; then fail "crash"; else pass; fi

log_test "Broker message count"
COUNT=$(echo "$FINAL" | python3 -c "import sys,json; print(len(json.load(sys.stdin).get('messages',[])))" 2>/dev/null || echo "0")
if [ "$COUNT" -ge 1 ]; then
  pass
  echo "    $COUNT messages"
else
  fail "empty"
fi

echo "--- Phase 7: Clean Exit ---"

log_test "Kill team"
"$BINARY" kill 2>/dev/null
if tmux -L wuphf list-sessions 2>&1 | grep -qi "no server\|error"; then pass; else fail "still running"; fi

kill $NEX_PID 2>/dev/null || true
wait $NEX_PID 2>/dev/null || true

echo ""
echo "============================================"
echo "  Results: $PASSED/$TOTAL passed"
echo "  Artifacts: $ARTIFACTS/"
if [ "$FAILED" -gt 0 ]; then
  echo "  FAILED: $FAILED"
  exit 1
else
  echo "  ALL PASSED"
fi
echo "============================================"
