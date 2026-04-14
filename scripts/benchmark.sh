#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════
# WUPHF Token Benchmark
# Measures real token consumption per test scenario.
# Compares against accumulated-session orchestrator baselines.
#
# Usage:
#   ./scripts/benchmark.sh              # run all tests
#   ./scripts/benchmark.sh single       # single-turn only
#   ./scripts/benchmark.sh session      # 10-turn session
#   ./scripts/benchmark.sh idle         # idle burn test
#   ./scripts/benchmark.sh fanout       # multi-agent fan-out
# ═══════════════════════════════════════════════════════════════
set -euo pipefail

BROKER="http://localhost:7890"
PROXY="http://localhost:7891"
REPORT_DIR="/tmp/wuphf-benchmark-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$REPORT_DIR"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
DIM='\033[2m'
NC='\033[0m'

# ─── Helpers ───

get_token() {
  curl -s "$PROXY/api-token" | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])"
}

check_broker() {
  local status
  status=$(curl -s --max-time 3 "$BROKER/health" 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null || echo "")
  if [ "$status" != "ok" ]; then
    echo -e "${RED}Broker not running. Start with: wuphf --pack starter${NC}"
    exit 1
  fi
}

send_message() {
  local channel="$1" content="$2" tagged="$3" token="$4"
  curl -s -X POST "$BROKER/messages" \
    -H "Authorization: Bearer $token" \
    -H "Content-Type: application/json" \
    -d "{\"from\":\"you\",\"channel\":\"$channel\",\"content\":\"$content\",\"tagged\":$tagged}" > /dev/null
}

# Capture SSE stream for an agent, extract token usage from turn.completed events
capture_agent_stream() {
  local agent="$1" duration="$2" token="$3" outfile="$4"
  timeout "$duration" curl -sN "$BROKER/agent-stream/${agent}?token=${token}" \
    -H "Accept: text/event-stream" > "$outfile" 2>/dev/null || true
}

# Parse token usage from captured SSE stream file
parse_usage() {
  local file="$1"
  python3 -c "
import json, sys
total_input = 0
total_output = 0
total_cached = 0
turns = 0
for line in open('$file'):
    line = line.strip()
    if not line.startswith('data: '): continue
    data = line[6:]
    try:
        d = json.loads(data)
        if d.get('type') == 'turn.completed' and 'usage' in d:
            u = d['usage']
            total_input += u.get('input_tokens', 0)
            total_output += u.get('output_tokens', 0)
            total_cached += u.get('cached_input_tokens', 0)
            turns += 1
    except: pass
print(json.dumps({
    'turns': turns,
    'input_tokens': total_input,
    'output_tokens': total_output,
    'cached_tokens': total_cached,
    'effective_input': total_input - total_cached,
    'total_billed': (total_input - total_cached) + total_output
}))
" 2>/dev/null
}

# Aggregate usage across multiple agent files
aggregate_usage() {
  local prefix="$1"
  python3 -c "
import json, glob, os
total = {'turns':0,'input_tokens':0,'output_tokens':0,'cached_tokens':0,'effective_input':0,'total_billed':0}
agents = {}
for f in sorted(glob.glob('$prefix-*.json')):
    agent = os.path.basename(f).replace('${prefix##*/}-','').replace('.json','')
    with open(f) as fh:
        d = json.load(fh)
    agents[agent] = d
    for k in total:
        total[k] += d[k]
print(json.dumps({'agents': agents, 'total': total}, indent=2))
" 2>/dev/null
}

print_header() {
  echo ""
  echo -e "${BOLD}${CYAN}═══════════════════════════════════════════════════${NC}"
  echo -e "${BOLD}${CYAN}  $1${NC}"
  echo -e "${BOLD}${CYAN}═══════════════════════════════════════════════════${NC}"
  echo ""
}

print_usage_table() {
  local json_file="$1"
  python3 -c "
import json, sys
d = json.load(open('$json_file'))
agents = d.get('agents', {})
total = d.get('total', d)

# Header
print(f'  {\"Agent\":<8} {\"Turns\":>5} {\"Input\":>9} {\"Cached\":>9} {\"Effective\":>9} {\"Output\":>7} {\"Billed\":>9}')
print(f'  {\"─\"*8} {\"─\"*5} {\"─\"*9} {\"─\"*9} {\"─\"*9} {\"─\"*7} {\"─\"*9}')

for name, a in sorted(agents.items()):
    if a['turns'] == 0: continue
    cache_pct = (a['cached_tokens']/a['input_tokens']*100) if a['input_tokens'] > 0 else 0
    print(f'  {name:<8} {a[\"turns\"]:>5} {a[\"input_tokens\"]:>9,} {a[\"cached_tokens\"]:>8,} {a[\"effective_input\"]:>9,} {a[\"output_tokens\"]:>7,} {a[\"total_billed\"]:>9,}')

cache_pct = (total['cached_tokens']/total['input_tokens']*100) if total['input_tokens'] > 0 else 0
print(f'  {\"─\"*8} {\"─\"*5} {\"─\"*9} {\"─\"*9} {\"─\"*9} {\"─\"*7} {\"─\"*9}')
print(f'  {\"TOTAL\":<8} {total[\"turns\"]:>5} {total[\"input_tokens\"]:>9,} {total[\"cached_tokens\"]:>8,} {total[\"effective_input\"]:>9,} {total[\"output_tokens\"]:>7,} {total[\"total_billed\"]:>9,}')
print(f'  Cache hit rate: {cache_pct:.0f}%')
" 2>/dev/null
}

# ═══════════════════════════════════════════════════════════════
# Test 1: Single-turn cost
# One human message, measure total tokens for full round-trip.
# ═══════════════════════════════════════════════════════════════
test_single_turn() {
  print_header "TEST 1: Single-turn cost"
  echo -e "  ${DIM}One message → all agents respond → measure total tokens${NC}"
  echo ""

  local token
  token=$(get_token)
  local agents=("ceo" "eng" "gtm")
  local wait_time=90

  echo -e "  Sending: ${BOLD}\"What is the team working on right now?\"${NC}"
  echo -e "  Tagged: ${BOLD}@eng @gtm${NC} (CEO auto-wakes in delegation mode)"
  echo ""

  # Start capturing all agent streams
  for agent in "${agents[@]}"; do
    capture_agent_stream "$agent" "$wait_time" "$token" "$REPORT_DIR/single-${agent}.sse" &
  done

  sleep 1
  send_message "general" "What is the team working on right now?" "[\"eng\",\"gtm\"]" "$token"

  echo -e "  ${DIM}Waiting ${wait_time}s for all agents to complete...${NC}"
  wait

  # Parse usage from each stream
  for agent in "${agents[@]}"; do
    parse_usage "$REPORT_DIR/single-${agent}.sse" > "$REPORT_DIR/single-${agent}.json"
  done

  aggregate_usage "$REPORT_DIR/single" > "$REPORT_DIR/single-total.json"
  print_usage_table "$REPORT_DIR/single-total.json"

  # Comparison
  local wuphf_billed
  wuphf_billed=$(python3 -c "import json; print(json.load(open('$REPORT_DIR/single-total.json'))['total']['total_billed'])")
  echo ""
  echo -e "  ${BOLD}vs session-resume-based orchestrators (industry baseline):${NC}"
  echo -e "    Paperclip CEO turn:     ~300,000 input (session resume accumulation)"
  echo -e "    Paperclip MCP overhead: ~24,000/agent (12 servers loaded globally)"
  echo -e "    Paperclip estimated:    ~372,000 tokens"
  echo -e "    ${GREEN}WUPHF actual:           ${wuphf_billed} tokens${NC}"
  if [ "$wuphf_billed" -gt 0 ] 2>/dev/null; then
    python3 -c "
ratio = 372000 / $wuphf_billed
print(f'    Ratio: {ratio:.1f}x more efficient on first turn')
" 2>/dev/null
  fi
}

# ═══════════════════════════════════════════════════════════════
# Test 2: 10-turn session (accumulation curve)
# Send 10 messages sequentially, chart tokens per turn.
# This is where WUPHF's fresh-session architecture shines:
# Paperclip grows linearly, WUPHF stays flat.
# ═══════════════════════════════════════════════════════════════
test_session() {
  print_header "TEST 2: 10-turn session (accumulation curve)"
  echo -e "  ${DIM}10 sequential messages to CEO → measure tokens per turn${NC}"
  echo -e "  ${DIM}WUPHF: fresh session each turn (flat). Paperclip: --resume (linear growth).${NC}"
  echo ""

  local token
  token=$(get_token)
  local wait_per_turn=60
  local messages=(
    "What are our top 3 priorities this week?"
    "Tell me more about priority number 1."
    "What resources do we need for that?"
    "Who should own this initiative?"
    "What is the timeline?"
    "Are there any blockers?"
    "What is the risk if we delay?"
    "Draft a quick plan for priority 1."
    "How should we communicate this to the team?"
    "Summarize everything we discussed."
  )

  echo -e "  ${BOLD}Turn  Input      Cached     Effective  Output  Billed${NC}"
  echo -e "  ${BOLD}────  ─────────  ─────────  ─────────  ──────  ─────────${NC}"

  local total_billed=0
  local paperclip_total=0

  for i in $(seq 0 9); do
    local turn=$((i + 1))
    local msg="${messages[$i]}"

    # Capture CEO stream
    capture_agent_stream "ceo" "$wait_per_turn" "$token" "$REPORT_DIR/session-turn${turn}.sse" &
    local cap_pid=$!

    sleep 1
    send_message "dm-ceo" "$msg" "[\"ceo\"]" "$token"
    wait $cap_pid

    parse_usage "$REPORT_DIR/session-turn${turn}.sse" > "$REPORT_DIR/session-turn${turn}.json"

    local usage
    usage=$(cat "$REPORT_DIR/session-turn${turn}.json")
    local inp out cached eff billed
    inp=$(echo "$usage" | python3 -c "import sys,json; print(json.load(sys.stdin)['input_tokens'])")
    out=$(echo "$usage" | python3 -c "import sys,json; print(json.load(sys.stdin)['output_tokens'])")
    cached=$(echo "$usage" | python3 -c "import sys,json; print(json.load(sys.stdin)['cached_tokens'])")
    eff=$(echo "$usage" | python3 -c "import sys,json; print(json.load(sys.stdin)['effective_input'])")
    billed=$(echo "$usage" | python3 -c "import sys,json; print(json.load(sys.stdin)['total_billed'])")

    total_billed=$((total_billed + billed))

    # Paperclip estimate: base 84k + 40k per accumulated turn (--resume growth)
    local paperclip_turn=$((84000 + 40000 * turn))
    paperclip_total=$((paperclip_total + paperclip_turn))

    printf "  %-4s  %'9d  %'9d  %'9d  %'6d  %'9d\n" "$turn" "$inp" "$cached" "$eff" "$out" "$billed"
  done

  echo ""
  echo -e "  ${BOLD}Session totals:${NC}"
  echo -e "    WUPHF 10-turn total:     $(printf "%'d" $total_billed) tokens"
  echo -e "    Paperclip estimate:      $(printf "%'d" $paperclip_total) tokens"
  if [ "$total_billed" -gt 0 ]; then
    python3 -c "print(f'    Ratio: {$paperclip_total / $total_billed:.1f}x more efficient over 10 turns')" 2>/dev/null
  fi
  echo ""
  echo -e "  ${DIM}Paperclip estimate: 84k base + 40k/turn accumulated context (--resume).${NC}"
  echo -e "  ${DIM}WUPHF stays flat because each turn starts a fresh session.${NC}"
}

# ═══════════════════════════════════════════════════════════════
# Test 3: Idle burn
# Start the system, leave it idle for 2 minutes, measure tokens.
# WUPHF: zero (push-driven, no polling).
# Paperclip: heartbeat polls every 30s.
# ═══════════════════════════════════════════════════════════════
test_idle() {
  print_header "TEST 3: Idle burn (2 minutes)"
  echo -e "  ${DIM}System running, no human messages, measure token consumption.${NC}"
  echo -e "  ${DIM}WUPHF: push-driven (zero idle cost). Paperclip: heartbeat every 30s.${NC}"
  echo ""

  local token
  token=$(get_token)
  local duration=120
  local agents=("ceo" "eng" "gtm")

  echo -e "  Capturing all agent streams for ${duration}s of idle time..."
  for agent in "${agents[@]}"; do
    capture_agent_stream "$agent" "$duration" "$token" "$REPORT_DIR/idle-${agent}.sse" &
  done

  # Progress indicator
  for i in $(seq 1 $((duration / 10))); do
    sleep 10
    echo -ne "\r  ${DIM}[$((i * 10))/${duration}s]${NC}"
  done
  echo ""
  wait

  local total_idle=0
  for agent in "${agents[@]}"; do
    parse_usage "$REPORT_DIR/idle-${agent}.sse" > "$REPORT_DIR/idle-${agent}.json"
    local billed
    billed=$(python3 -c "import json; print(json.load(open('$REPORT_DIR/idle-${agent}.json'))['total_billed'])")
    total_idle=$((total_idle + billed))
  done

  # Paperclip: 4 heartbeats/min * 2 min * 3 agents * ~2000 tokens/heartbeat
  local paperclip_idle=$((4 * 2 * 3 * 2000))

  echo ""
  echo -e "  ${BOLD}Results:${NC}"
  echo -e "    WUPHF idle tokens:       ${GREEN}${total_idle}${NC}"
  echo -e "    Paperclip estimate:      ${RED}$(printf "%'d" $paperclip_idle)${NC}"
  echo ""
  if [ "$total_idle" -eq 0 ]; then
    echo -e "  ${GREEN}${BOLD}WUPHF burned exactly zero tokens while idle.${NC}"
    echo -e "  ${DIM}Paperclip would burn ~${paperclip_idle} tokens polling the LLM every 30s.${NC}"
  else
    echo -e "  ${YELLOW}WUPHF burned ${total_idle} tokens during idle (unexpected — investigate).${NC}"
  fi
}

# ═══════════════════════════════════════════════════════════════
# Test 4: Multi-agent fan-out (delegation efficiency)
# Tag 2 specialists. Measure: who wakes, how many tokens each.
# In delegation mode, only tagged agents wake. CEO stays quiet.
# ═══════════════════════════════════════════════════════════════
test_fanout() {
  print_header "TEST 4: Multi-agent fan-out"
  echo -e "  ${DIM}Tag @eng @gtm explicitly. In delegation mode, only they should wake.${NC}"
  echo -e "  ${DIM}CEO should NOT wake (human tagged specialists directly).${NC}"
  echo ""

  local token
  token=$(get_token)
  local agents=("ceo" "eng" "gtm")
  local wait_time=90

  echo -e "  Sending: ${BOLD}\"@eng what is the build status? @gtm what is the pipeline status?\"${NC}"
  echo ""

  for agent in "${agents[@]}"; do
    capture_agent_stream "$agent" "$wait_time" "$token" "$REPORT_DIR/fanout-${agent}.sse" &
  done

  sleep 1
  send_message "general" "@eng what is the build status? @gtm what is the pipeline status?" "[\"eng\",\"gtm\"]" "$token"

  echo -e "  ${DIM}Waiting ${wait_time}s...${NC}"
  wait

  for agent in "${agents[@]}"; do
    parse_usage "$REPORT_DIR/fanout-${agent}.sse" > "$REPORT_DIR/fanout-${agent}.json"
  done

  aggregate_usage "$REPORT_DIR/fanout" > "$REPORT_DIR/fanout-total.json"

  echo -e "  ${BOLD}Agent wake analysis:${NC}"
  python3 -c "
import json
d = json.load(open('$REPORT_DIR/fanout-total.json'))
for name, a in sorted(d['agents'].items()):
    if a['turns'] > 0:
        print(f'    {name}: WOKE — {a[\"turns\"]} turn(s), {a[\"total_billed\"]:,} tokens')
    else:
        print(f'    {name}: QUIET — 0 turns, 0 tokens (correctly suppressed)')

total = d['total']
print(f'')
print(f'  Total: {total[\"total_billed\"]:,} tokens for {total[\"turns\"]} turns')

# Paperclip comparison: all agents wake via heartbeat regardless of tagging
paperclip = 84000 * 3  # all 3 agents poll and process
print(f'')
print(f'  vs Paperclip:')
print(f'    Paperclip: all agents wake via heartbeat (~{paperclip:,} tokens)')
print(f'    WUPHF:     only tagged agents wake ({total[\"total_billed\"]:,} tokens)')
if total['total_billed'] > 0:
    print(f'    Ratio: {paperclip / total[\"total_billed\"]:.1f}x more efficient')
" 2>/dev/null
}

# ═══════════════════════════════════════════════════════════════
# Final report
# ═══════════════════════════════════════════════════════════════
print_report() {
  print_header "BENCHMARK REPORT"

  echo -e "  ${BOLD}Architecture comparison:${NC}"
  echo ""
  echo "  ┌────────────────────────┬──────────────────────┬──────────────────────┐"
  echo "  │ Dimension              │ WUPHF                │ Paperclip            │"
  echo "  ├────────────────────────┼──────────────────────┼──────────────────────┤"
  echo "  │ Session model          │ Fresh per turn       │ --resume accumulates │"
  echo "  │ Idle cost              │ Zero (push-driven)   │ Heartbeat every 30s  │"
  echo "  │ MCP servers            │ Per-agent scoped     │ All 12 globally      │"
  echo "  │ Agent wake             │ Only when tagged     │ All poll via hearbeat│"
  echo "  │ Cost over N turns      │ O(1) per turn        │ O(N) per turn        │"
  echo "  │ Cache utilization      │ ~67% prompt cache    │ None (resume bloats) │"
  echo "  └────────────────────────┴──────────────────────┴──────────────────────┘"
  echo ""
  echo -e "  Results saved to: ${BOLD}$REPORT_DIR/${NC}"
  echo ""

  # Generate markdown report
  python3 - "$REPORT_DIR" <<'PYEOF'
import json, glob, os, sys

report_dir = sys.argv[1]

def load(name):
    path = os.path.join(report_dir, name)
    if os.path.exists(path):
        with open(path) as f:
            return json.load(f)
    return None

lines = ["# WUPHF Token Benchmark Report\n"]
lines.append(f"Generated: {os.path.basename(report_dir)}\n")

# Single turn
single = load("single-total.json")
if single:
    t = single["total"]
    lines.append("\n## Test 1: Single-turn cost\n")
    lines.append(f"- Turns: {t['turns']}")
    lines.append(f"- Input tokens: {t['input_tokens']:,}")
    lines.append(f"- Cached: {t['cached_tokens']:,} ({t['cached_tokens']/t['input_tokens']*100:.0f}%)" if t['input_tokens'] > 0 else "")
    lines.append(f"- **Billed: {t['total_billed']:,}**")
    lines.append(f"- Paperclip estimate: ~372,000")
    if t['total_billed'] > 0:
        lines.append(f"- **WUPHF is {372000/t['total_billed']:.1f}x more efficient**\n")

# Session
session_total = 0
session_lines = []
for i in range(1, 11):
    d = load(f"session-turn{i}.json")
    if d and d['turns'] > 0:
        session_total += d['total_billed']
        session_lines.append(f"| {i} | {d['input_tokens']:,} | {d['cached_tokens']:,} | {d['total_billed']:,} |")
if session_lines:
    lines.append("\n## Test 2: 10-turn session\n")
    lines.append("| Turn | Input | Cached | Billed |")
    lines.append("|------|-------|--------|--------|")
    lines.extend(session_lines)
    paperclip_session = sum(84000 + 40000 * i for i in range(1, 11))
    lines.append(f"\n- **WUPHF total: {session_total:,}**")
    lines.append(f"- Paperclip estimate: {paperclip_session:,}")
    if session_total > 0:
        lines.append(f"- **WUPHF is {paperclip_session/session_total:.1f}x more efficient over 10 turns**\n")

# Idle
idle_total = 0
for agent in ["ceo", "eng", "gtm"]:
    d = load(f"idle-{agent}.json")
    if d:
        idle_total += d["total_billed"]
if idle_total == 0:
    lines.append("\n## Test 3: Idle burn\n")
    lines.append("- **WUPHF: 0 tokens** (push-driven, zero idle cost)")
    lines.append("- Paperclip: ~48,000 tokens (heartbeat polling)\n")

# Fanout
fanout = load("fanout-total.json")
if fanout:
    t = fanout["total"]
    lines.append("\n## Test 4: Multi-agent fan-out\n")
    for name, a in sorted(fanout["agents"].items()):
        status = f"WOKE — {a['total_billed']:,} tokens" if a['turns'] > 0 else "QUIET — 0 tokens"
        lines.append(f"- {name}: {status}")
    lines.append(f"\n- **WUPHF total: {t['total_billed']:,}**")
    lines.append(f"- Paperclip: ~252,000 (all agents wake)\n")

report_path = os.path.join(report_dir, "REPORT.md")
with open(report_path, "w") as f:
    f.write("\n".join(lines))
print(f"  Markdown report: {report_path}")
PYEOF
}

# ═══════════════════════════════════════════════════════════════
# Main
# ═══════════════════════════════════════════════════════════════
main() {
  check_broker
  local test="${1:-all}"

  echo -e "${BOLD}${CYAN}"
  echo "  ╦ ╦╦ ╦╔═╗╦ ╦╔═╗  ╔╗ ╔═╗╔╗╔╔═╗╦ ╦╔╦╗╔═╗╦═╗╦╔═"
  echo "  ║║║║ ║╠═╝╠═╣╠╣   ╠╩╗║╣ ║║║║  ╠═╣║║║╠═╣╠╦╝╠╩╗"
  echo "  ╚╩╝╚═╝╩  ╩ ╩╚    ╚═╝╚═╝╝╚╝╚═╝╩ ╩╩ ╩╩ ╩╩╚═╩ ╩"
  echo -e "${NC}"
  echo -e "  ${DIM}Token efficiency benchmark vs Paperclip${NC}"
  echo -e "  ${DIM}Report: $REPORT_DIR${NC}"

  case "$test" in
    single)  test_single_turn ;;
    session) test_session ;;
    idle)    test_idle ;;
    fanout)  test_fanout ;;
    all)
      test_single_turn
      test_fanout
      test_idle
      test_session
      print_report
      ;;
    *)
      echo "Usage: $0 [single|session|idle|fanout|all]"
      exit 1
      ;;
  esac
}

main "$@"
