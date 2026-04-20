#!/usr/bin/env bash
# demo-wiki-live.sh — trigger live wiki writes across multiple agents
# so you can see the /wiki UI update in real time.
#
# Usage:
#   ./scripts/demo-wiki-live.sh                                    # dev broker :7899
#   BROKER=http://127.0.0.1:7890 ./scripts/demo-wiki-live.sh       # prod ports
#   DELAY=5 ./scripts/demo-wiki-live.sh                            # slower pacing
#   DRY_RUN=1 ./scripts/demo-wiki-live.sh                          # print, don't write
#
# Pair with the web UI open at  http://localhost:7900/#/wiki  (dev, or :7891 prod).
# You will see:
#   • the rolling edit-log footer pulse amber as each entry lands
#   • the Team Wiki catalog ticking up
#   • the Referenced By panel filling in as linking articles land

set -u

BROKER="${BROKER:-http://127.0.0.1:7899}"
DELAY="${DELAY:-3}"
DRY_RUN="${DRY_RUN:-0}"

# ── Colors ──────────────────────────────────────────────────────────
GREEN=$'\033[32m'; RED=$'\033[31m'; DIM=$'\033[2m'; BOLD=$'\033[1m'; RESET=$'\033[0m'
ok()   { printf "%s✓%s %s\n" "$GREEN" "$RESET" "$*"; }
fail() { printf "%s✗%s %s\n" "$RED" "$RESET" "$*" >&2; }
step() { printf "%s→%s %s%s%s\n" "$DIM" "$RESET" "$BOLD" "$*" "$RESET"; }

command -v curl >/dev/null || { fail "curl is required"; exit 1; }
PY=$(command -v python3 || command -v python || true)
[ -z "$PY" ] && { fail "python3 is required for JSON parsing"; exit 1; }

step "fetching broker token from $BROKER/web-token"
TOKEN=$(curl -fsS "$BROKER/web-token" 2>/dev/null | "$PY" -c "import sys,json;print(json.load(sys.stdin).get('token',''))")
if [ -z "$TOKEN" ]; then
  fail "broker not reachable at $BROKER (is wuphf-dev running?)"
  fail "try:  ./wuphf-dev --broker-port 7899 --web-port 7900 --memory-backend markdown"
  exit 1
fi
ok "token acquired (len ${#TOKEN})"

write() {
  local slug=$1 path=$2 mode=$3 msg=$4 content=$5
  step "${slug} → ${path}  (${mode})"
  if [ "$DRY_RUN" = "1" ]; then
    printf "%s    [dry-run]%s\n" "$DIM" "$RESET"
    return 0
  fi
  local payload
  payload=$("$PY" -c '
import json, sys
print(json.dumps({
  "slug": sys.argv[1],
  "path": sys.argv[2],
  "mode": sys.argv[3],
  "commit_message": sys.argv[4],
  "content": sys.argv[5],
}))
' "$slug" "$path" "$mode" "$msg" "$content")
  local response http_code
  response=$(curl -sS -w '\nHTTP=%{http_code}' -X POST "$BROKER/wiki/write" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d "$payload" 2>&1)
  http_code=$(printf '%s' "$response" | awk -F'=' '/^HTTP=/{print $2}' | tail -1)
  response=$(printf '%s' "$response" | sed '/^HTTP=/d')
  case "$http_code" in
    2*)
      local sha
      sha=$(printf '%s' "$response" | "$PY" -c "import sys,json;print(json.load(sys.stdin).get('commit_sha',''))" 2>/dev/null || true)
      ok "committed ${sha}"
      ;;
    *)
      local detail
      detail=$(printf '%s' "$response" | "$PY" -c "import sys,json;print(json.load(sys.stdin).get('error',''))" 2>/dev/null || true)
      if [ -z "$detail" ]; then detail="$response"; fi
      fail "write failed (HTTP ${http_code}): ${detail}"
      sleep "$DELAY"
      return 1
      ;;
  esac
  sleep "$DELAY"
}

printf "\n%sWUPHF LIVE WIKI DEMO%s\n" "$BOLD" "$RESET"
printf "%sOpen http://localhost:7900/#/wiki in a browser BEFORE this script finishes.%s\n" "$DIM" "$RESET"
printf "%s5-second head start so you can switch to the browser...%s\n\n" "$DIM" "$RESET"
sleep 5

# ── Beat 1: CEO creates a new customer brief ────────────────────────
write "operator" "team/customers/meridian-freight.md" "create" \
  "first meridian brief" \
  "# Meridian Freight

**Meridian Freight** is a 120-person logistics company in Columbus. Warm intro via [[people/sarah-chen]] who ran ops for them 2019-2022.

## Current state

Sarah forwarded a note from their COO flagging dispatcher burnout. Six open reqs they can't fill. Same pattern as [[customers/customer-x]] — manual AM schedule-rebuild every day.

## Next

Discovery call scheduled 2026-04-24. See [[playbooks/mid-market-first-call]]."

# ── Beat 2: PM updates existing playbook with meridian reference ───
write "planner" "team/playbooks/churn-prevention.md" "replace" \
  "add meridian link + dispatcher-burnout signal" \
  "# Churn prevention

References [[customers/customer-x]] and [[customers/meridian-freight]] as renewal-risk cases.

## When to trigger

- Mid-market pilots approaching 90-day renewal
- Champion at customer flags timing pressure
- Contract under \$100k (smaller deals churn faster)
- **NEW:** dispatcher-burnout signal (seen at Customer X, forming at Meridian)

## Playbook steps

1. Two weeks before renewal: renewal-prep email to champion
2. One week before: 20-min check-in with champion + their manager
3. Day of: confirmation of next year or negotiated extension
4. **NEW:** if the dispatcher-burnout signal fires, escalate to CEO +7 days"

# ── Beat 3: Growth drops a pricing decision ─────────────────────────
write "growth" "team/decisions/2026-q2-pricing.md" "create" \
  "q2 pricing decision" \
  "# 2026-Q2 pricing

Read-only dispatcher seats are 30% of full seat price. This unblocks [[customers/customer-x]] seat-expansion and removes the same objection for [[customers/meridian-freight]].

## Rationale

Two pilots hit the same wall in Q1. Viewer seats were hacked around with shared logins. 30% gives customers a clear upgrade ladder without cannibalizing the primary seat.

## Scope

- Effective 2026-05-01
- Legacy contracts grandfathered for 6 months
- New pilots signed after 2026-05-01 use the new sheet"

# ── Beat 4: CEO creates the champion's bio ──────────────────────────
write "operator" "team/people/sarah-chen.md" "create" \
  "sarah chen bio" \
  "# Sarah Chen

Director of Ops at [[customers/customer-x]]. Former VP Operations at [[customers/meridian-freight]] (2019-2022).

## Why she matters

Our strongest champion at Customer X. Quarterly performance review on 2026-Q3 — see [[playbooks/churn-prevention]] for the timing-pressure play.

## Preferences

- Prefers Tuesday mornings for check-ins
- Replies faster in Slack than email
- Does NOT want the CEO cold-called"

# ── Beat 5: Reviewer appends a note to existing Customer X ──────────
write "reviewer" "team/customers/customer-x.md" "append_section" \
  "note dispatcher-burnout signal" \
  "

## Update — 2026-04-19

Seeing the same dispatcher-burnout signal we just noted at [[customers/meridian-freight]]. Means the [[playbooks/onboarding-wedge-thesis]] is landing consistently across mid-market logistics. See the new entry in [[playbooks/churn-prevention]]."

# ── Beat 6: Builder writes a tech decision ──────────────────────────
write "builder" "team/decisions/wiki-as-default.md" "create" \
  "wiki as default memory" \
  "# Wiki as default memory

As of this week, new installs default to the markdown wiki instead of the Nex knowledge graph.

## Why

- Users can \`cat\` and \`git clone\` their team memory
- No API key required to try it
- File-over-app story matches WUPHF's MIT/self-hosted posture

## What it means

- Existing Nex/GBrain users unaffected (backend switch is config-pinned)
- Four new MCP tools (\`team_wiki_read/search/list/write\`) replace \`team_memory_*\` when \`WUPHF_MEMORY_BACKEND=markdown\`

See [[decisions/2026-q2-pricing]] for the related commercial thinking."

# ── Beat 7: PM updates renewal playbook ─────────────────────────────
write "planner" "team/playbooks/renewal.md" "replace" \
  "renewal playbook concrete next steps" \
  "# Renewal and retention

Our goal: 90%+ gross retention on mid-market pilots.

## Signals to watch

- Champion career pressure (see [[people/sarah-chen]])
- Dispatcher-burnout proxy metrics (see [[customers/customer-x]])
- Delayed response to renewal-prep email (≥ 3 days = escalate)

## Steps

1. 30 days before renewal: champion + sponsor mapping refreshed
2. 14 days: prep email + calendar hold
3. 7 days: check-in call
4. Day of: close

Cross-link: [[playbooks/churn-prevention]]."

# ── Beat 8: CEO wraps up with David Kim bio ─────────────────────────
write "operator" "team/people/david-kim.md" "create" \
  "dk profile" \
  "# David Kim

VP Engineering at [[customers/customer-x]]. Reports into Mike Reyes (VPO). Not a day-to-day contact — but reviews every MSA before [[customers/customer-x]]'s legal signs off.

## Relationship

Met at the March logistics summit. Technical; asks good questions about data residency and training boundaries. See [[decisions/2026-q2-pricing]] and the data-handling addendum work."

echo
printf "%s✓ demo complete — 8 writes across 5 agents%s\n" "$GREEN$BOLD" "$RESET"
printf "%s  Refresh /#/wiki to see all the new articles and backlinks.%s\n" "$DIM" "$RESET"
