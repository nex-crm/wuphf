#!/usr/bin/env bash
# Nex API wrapper — safe HTTP client for OpenClaw skills
# ENV: NEX_API_KEY (required)
# ENDPOINTS: https://app.nex.ai/api/developers/v1/*
# WRITES: Depends on method (POST/PUT/PATCH/DELETE modify data)
set -euo pipefail

BASE_URL="https://app.nex.ai/api/developers"
TIMEOUT=120

# --- Validate environment ---
if [[ -z "${NEX_API_KEY:-}" ]]; then
  echo "Error: NEX_API_KEY environment variable is not set" >&2
  exit 1
fi

# --- Parse arguments ---
METHOD="${1:-}"
API_PATH="${2:-}"

if [[ -z "$METHOD" ]]; then
  echo "Usage: nex-api.sh <METHOD|sse> <path>" >&2
  echo "  METHOD: GET, POST, PUT, PATCH, DELETE, or sse" >&2
  exit 2
fi

if [[ -z "$API_PATH" ]]; then
  echo "Error: API path is required" >&2
  exit 3
fi

# --- Validate path starts with /v1/ and has no traversal ---
if [[ "$API_PATH" != /v1/* && "$API_PATH" != /v1 ]]; then
  echo "Error: API path must start with /v1/" >&2
  exit 3
fi
if [[ "$API_PATH" == *".."* ]]; then
  echo "Error: API path must not contain '..'" >&2
  exit 3
fi

# --- SSE mode ---
if [[ "$METHOD" == "sse" ]]; then
  exec curl -N -s --max-time "$TIMEOUT" \
    -H "Authorization: Bearer ${NEX_API_KEY}" \
    -H "Accept: text/event-stream" \
    "${BASE_URL}${API_PATH}"
fi

# --- Validate HTTP method ---
case "$METHOD" in
  GET|POST|PUT|PATCH|DELETE) ;;
  *)
    echo "Error: Invalid method '$METHOD'. Must be GET, POST, PUT, PATCH, DELETE, or sse" >&2
    exit 2
    ;;
esac

# --- Build curl arguments ---
CURL_ARGS=(
  -s
  -w '\n%{http_code}'
  --max-time "$TIMEOUT"
  -X "$METHOD"
  -H "Authorization: Bearer ${NEX_API_KEY}"
  -H "Content-Type: application/json"
  -H "Accept: application/json"
)

# Read body from stdin for methods that accept a body
if [[ "$METHOD" != "GET" && "$METHOD" != "DELETE" ]]; then
  if ! [ -t 0 ]; then
    CURL_ARGS+=(--data-binary @-)
  fi
fi

# --- Execute request ---
RESPONSE=$(curl "${CURL_ARGS[@]}" "${BASE_URL}${API_PATH}") || {
  echo "Error: curl request failed" >&2
  exit 4
}

# --- Split response body and HTTP status code ---
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | sed '$d')

# --- Check for HTTP errors ---
if [[ "$HTTP_CODE" -ge 400 ]] 2>/dev/null; then
  echo "Error: HTTP $HTTP_CODE" >&2
  echo "$BODY" >&2
  exit 4
fi

# --- Output response ---
echo "$BODY"
