#!/usr/bin/env bash
# OpenClaw bootstrap registration helper for WUPHF.
# Usage: wuphf-openclaw-register.sh <email> [name] [company_name] [jq-filter]
set -euo pipefail

BASE_URL="${WUPHF_REGISTER_URL:-${WUPHF_BASE_URL:-https://app.nex.ai}/api/v1/agents/register}"
TIMEOUT="${WUPHF_API_TIMEOUT:-120}"
SOURCE="${WUPHF_AGENT_SOURCE:-openclaw}"

EMAIL="${1:-}"
NAME="${2:-}"
COMPANY_NAME="${3:-}"
JQ_FILTER="${4:-}"

if [[ -z "$EMAIL" ]]; then
  echo "Usage: wuphf-openclaw-register.sh <email> [name] [company_name] [jq-filter]" >&2
  exit 2
fi

PAYLOAD=$(jq -cn \
  --arg email "$EMAIL" \
  --arg source "$SOURCE" \
  --arg name "$NAME" \
  --arg company_name "$COMPANY_NAME" \
  '{email: $email, source: $source}
   + (if $name != "" then {name: $name} else {} end)
   + (if $company_name != "" then {company_name: $company_name} else {} end)')

RESPONSE=$(curl -s -w '\n%{http_code}' --max-time "$TIMEOUT"   -X POST   -H "Content-Type: application/json"   -H "Accept: application/json"   --data-binary "$PAYLOAD"   "$BASE_URL") || {
  echo "Error: curl request failed" >&2
  exit 4
}

HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | sed '$d')

if [[ "$HTTP_CODE" -ge 400 ]] 2>/dev/null; then
  echo "Error: HTTP $HTTP_CODE" >&2
  echo "$BODY" >&2
  exit 4
fi

if [[ -n "$JQ_FILTER" ]]; then
  echo "$BODY" | jq "$JQ_FILTER" || {
    echo "Error: jq filter failed" >&2
    exit 5
  }
else
  echo "$BODY"
fi
