#!/usr/bin/env bash
# demo-entity-synthesis.sh — Karpathy LLM-Wiki demo
#
# Shows the full pipeline in one terminal session:
#   1. agent records 5 facts via entity_fact_record
#   2. fact log hits threshold → EntitySynthesizer fires automatically
#   3. broker shells out to your LLM CLI (claude / codex / openclaw)
#   4. result commits to wiki git repo under "archivist" identity
#   5. git log shows every author in the chain
#
# Usage:
#   ./scripts/demo-entity-synthesis.sh                        # dev broker :7899
#   BROKER=http://127.0.0.1:7890 ./scripts/demo-entity-synthesis.sh   # prod
#   ENTITY_KIND=people ENTITY_SLUG=ada-lovelace ./scripts/demo-entity-synthesis.sh
#   THRESHOLD=1 ./scripts/demo-entity-synthesis.sh            # fire on every fact
#
# Requirements: curl, python3, a running wuphf instance with --memory-backend markdown

set -euo pipefail

BROKER="${BROKER:-http://127.0.0.1:7899}"
ENTITY_KIND="${ENTITY_KIND:-companies}"
ENTITY_SLUG="${ENTITY_SLUG:-anthropic}"
AGENT_SLUG="${AGENT_SLUG:-demo-agent}"
THRESHOLD="${THRESHOLD:-5}"
SYNTH_TIMEOUT="${SYNTH_TIMEOUT:-45}"

# ── Colors ───────────────────────────────────────────────────────────────────
BOLD=$'\033[1m'; DIM=$'\033[2m'; GREEN=$'\033[32m'; YELLOW=$'\033[33m'
CYAN=$'\033[36m'; RED=$'\033[31m'; RESET=$'\033[0m'

step()  { printf "\n%s▶%s  %s%s%s\n" "$CYAN" "$RESET" "$BOLD" "$*" "$RESET"; }
ok()    { printf "   %s✓%s  %s\n" "$GREEN" "$RESET" "$*"; }
info()  { printf "   %s·%s  %s\n" "$DIM" "$RESET" "$*"; }
warn()  { printf "   %s!%s  %s\n" "$YELLOW" "$RESET" "$*"; }
die()   { printf "\n%s✗%s  %s\n" "$RED" "$RESET" "$*" >&2; exit 1; }

command -v curl    >/dev/null || die "curl is required"
command -v python3 >/dev/null || die "python3 is required"
PY=python3

# ── Resolve wiki repo root from broker home dir ───────────────────────────────
# Dev broker runs with HOME=~/.wuphf-dev-home (port 7899).
# Prod runs with the real HOME (port 7890).
if [[ "$BROKER" =~ :7899 ]]; then
  WUPHF_HOME="${HOME}/.wuphf-dev-home/.wuphf"
else
  WUPHF_HOME="${HOME}/.wuphf"
fi
WIKI_REPO="${WUPHF_REPO:-${WUPHF_HOME}/wiki}"

# ── Helpers ───────────────────────────────────────────────────────────────────
get_token() {
  curl -fsS "$BROKER/web-token" 2>/dev/null \
    | "$PY" -c "import sys,json;print(json.load(sys.stdin).get('token',''))"
}

post_json() {
  local path=$1 payload=$2
  curl -fsS -X POST "${BROKER}${path}" \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Content-Type: application/json" \
    -d "$payload"
}

get_json() {
  local path=$1
  curl -fsS -G "${BROKER}${path}" \
    -H "Authorization: Bearer ${TOKEN}" \
    "$@"
}

jq_field() {
  "$PY" -c "import sys,json; d=json.load(sys.stdin); print($1)"
}

# ── 0. Check broker is alive ─────────────────────────────────────────────────
printf "\n%sWUPHF Entity Synthesis Demo%s\n" "$BOLD" "$RESET"
printf "%sentity: %s/%s   broker: %s%s\n\n" "$DIM" "$ENTITY_KIND" "$ENTITY_SLUG" "$BROKER" "$RESET"

step "Connecting to broker"
TOKEN=$(get_token) || true
if [[ -z "$TOKEN" ]]; then
  die "Broker unreachable at ${BROKER}

  Start the dev broker first:
    wuphf-dev --broker-port 7899 --web-port 7900 --memory-backend markdown"
fi
ok "token acquired"

# ── 1. Record N facts ─────────────────────────────────────────────────────────
step "Recording ${THRESHOLD} facts about ${ENTITY_KIND}/${ENTITY_SLUG}"
info "Each fact = one git commit authored by '${AGENT_SLUG}'"
info "After fact #${THRESHOLD}, EntitySynthesizer fires automatically"

FACTS=(
  "Founded in 2021 by Dario Amodei, Daniela Amodei, and others who previously worked at OpenAI."
  "Headquarters are in San Francisco, California."
  "Creator of the Claude family of AI assistants (Claude 1, 2, 3, and Sonnet/Opus/Haiku variants)."
  "Raised over \$7 billion in funding as of early 2024, with Google and Amazon as major investors."
  "Research focus includes Constitutional AI (CAI) and responsible scaling policy (RSP)."
)

THRESHOLD_CROSSED=false
FACT_COUNT=0

for i in "${!FACTS[@]}"; do
  fact_num=$((i + 1))
  fact_text="${FACTS[$i]}"
  payload=$("$PY" -c "
import json, sys
print(json.dumps({
  'entity_kind': sys.argv[1],
  'entity_slug': sys.argv[2],
  'fact':        sys.argv[3],
  'recorded_by': sys.argv[4],
}))
" "$ENTITY_KIND" "$ENTITY_SLUG" "$fact_text" "$AGENT_SLUG")

  response=$(post_json "/entity/fact" "$payload") || die "POST /entity/fact failed on fact ${fact_num}"
  FACT_COUNT=$(printf '%s' "$response" | jq_field "d['fact_count']")
  crossed=$(printf '%s' "$response" | jq_field "str(d['threshold_crossed']).lower()")

  if [[ "$crossed" == "true" ]]; then
    ok "fact ${fact_num}/${THRESHOLD} — total=${FACT_COUNT}  ✦ threshold crossed — synthesis queued"
    THRESHOLD_CROSSED=true
  else
    ok "fact ${fact_num}/${THRESHOLD} — total=${FACT_COUNT}"
  fi
  info "${fact_text:0:70}..."
done

# ── 2. Trigger synthesis if not auto-fired (e.g. facts already existed) ───────
if [[ "$THRESHOLD_CROSSED" == "false" ]]; then
  warn "Threshold not crossed in this run — requesting synthesis explicitly"
  payload=$("$PY" -c "
import json, sys
print(json.dumps({'entity_kind': sys.argv[1], 'entity_slug': sys.argv[2], 'actor_slug': sys.argv[3]}))
" "$ENTITY_KIND" "$ENTITY_SLUG" "$AGENT_SLUG")
  post_json "/entity/brief/synthesize" "$payload" >/dev/null || warn "explicit synthesize request failed (broker may be unavailable)"
fi

# ── 3. Poll until synthesis commits (pending_delta → 0) ───────────────────────
step "Waiting for archivist to commit the synthesized brief"
info "Polling /entity/briefs every 2 s (timeout ${SYNTH_TIMEOUT}s)"
info "The broker shells out to your LLM CLI — this takes a few seconds"

deadline=$(( $(date +%s) + SYNTH_TIMEOUT ))
synth_start=$(date +%s)
synthesized=false
last_ts=""

while [[ "$(date +%s)" -lt "$deadline" ]]; do
  sleep 2
  response=$(get_json "/entity/briefs" 2>/dev/null) || continue
  # Find our entity in the briefs array
  row=$("$PY" -c "
import json, sys
briefs = json.load(sys.stdin).get('briefs', [])
for b in briefs:
    if b['kind'] == sys.argv[1] and b['slug'] == sys.argv[2]:
        print(json.dumps(b))
        sys.exit(0)
print('null')
" "$ENTITY_KIND" "$ENTITY_SLUG" <<< "$response") || continue

  if [[ "$row" == "null" ]]; then
    info "brief not yet visible …"
    continue
  fi

  pending=$("$PY" -c "import json,sys; print(json.loads(sys.argv[1])['pending_delta'])" "$row")
  ts=$("$PY" -c "import json,sys; print(json.loads(sys.argv[1]).get('last_synthesized_ts',''))" "$row")

  elapsed=$(( $(date +%s) - synth_start ))
  if [[ -n "$ts" && "$ts" != "$last_ts" ]]; then
    synthesized=true
    last_ts="$ts"
    sha=$("$PY" -c "import json,sys; print(json.loads(sys.argv[1]).get('last_synthesized_sha',''))" "$row")
    ok "[synthesizing] ${ENTITY_KIND}/${ENTITY_SLUG} — ${FACT_COUNT} facts → done → sha=${sha} (${elapsed}s)"
    break
  fi
  info "[synthesizing] ${ENTITY_KIND}/${ENTITY_SLUG} — ${FACT_COUNT} facts … (${elapsed}s elapsed)"
done

if [[ "$synthesized" == "false" ]]; then
  warn "Synthesis did not complete within ${SYNTH_TIMEOUT}s"
  warn "The LLM call may still be in progress — check broker logs"
  warn "Run: git -C ${WIKI_REPO} log --oneline -10"
  exit 1
fi

# ── 4. Show the synthesized brief ─────────────────────────────────────────────
step "Synthesized brief — team/${ENTITY_KIND}/${ENTITY_SLUG}.md"
BRIEF_PATH="${WIKI_REPO}/team/${ENTITY_KIND}/${ENTITY_SLUG}.md"
if [[ -f "$BRIEF_PATH" ]]; then
  printf "%s%s%s\n" "$DIM" "─────────────────────────────────────────────────────────────" "$RESET"
  # Strip YAML frontmatter for clean display
  "$PY" -c "
import sys
content = open(sys.argv[1]).read()
if content.startswith('---'):
    end = content.find('\n---\n', 3)
    if end >= 0:
        content = content[end+5:]
print(content.strip())
" "$BRIEF_PATH"
  printf "%s%s%s\n" "$DIM" "─────────────────────────────────────────────────────────────" "$RESET"
else
  warn "Brief not found at ${BRIEF_PATH} (wiki repo path may differ)"
fi

# ── 5. Show the git log chain ─────────────────────────────────────────────────
step "Git log — wiki repo at ${WIKI_REPO}"
info "Each commit has a real author: the recording agent or 'archivist'"
printf "\n"

if [[ -d "${WIKI_REPO}/.git" ]]; then
  git -C "$WIKI_REPO" log --oneline --author="$AGENT_SLUG\|archivist" \
    --grep="$ENTITY_SLUG" -20 --color=always 2>/dev/null || \
  git -C "$WIKI_REPO" log --oneline -20 --color=always
else
  warn "Wiki git repo not found at ${WIKI_REPO}"
  warn "Set WUPHF_REPO=/path/to/wiki to override"
fi

# ── 6. Summary ────────────────────────────────────────────────────────────────
printf "\n%s━━━ What just happened ━━━%s\n" "$BOLD" "$RESET"
cat <<'EOF'

  1. demo-agent  recorded 5 facts → each landed as a git commit
     authored by demo-agent@wuphf.local

  2. Fact #5 crossed the synthesis threshold (WUPHF_ENTITY_BRIEF_THRESHOLD=5)
     → EntitySynthesizer.EnqueueSynthesis() fired automatically

  3. The broker shelled out to your LLM CLI (claude / codex / openclaw)
     with the existing brief + new facts as prompt input

  4. LLM output was committed under the "archivist" git identity
     commit message: "archivist: update companies/anthropic brief (5 facts)"

  5. The wiki article is now live — wikilinks resolve, git history is auditable
     Every agent edit and every synthesis is a real, attributable commit.

  This is Karpathy's LLM-Wiki made real: append-only facts, threshold-triggered
  LLM synthesis, git as the knowledge graph, no SDK lock-in.

EOF
printf "%sRun again to add more facts and trigger another synthesis cycle.%s\n\n" "$DIM" "$RESET"
