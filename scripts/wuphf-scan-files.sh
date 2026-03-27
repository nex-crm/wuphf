#!/usr/bin/env bash
# WUPHF file scanner — discovers project files and ingests changed ones
# Uses manifest-based change detection via ~/.wuphf/file-scan-manifest.json
# ENV: WUPHF_API_KEY (preferred, NEX_API_KEY fallback)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MANIFEST_FILE="$HOME/.wuphf/file-scan-manifest.json"
MAX_FILE_SIZE=102400  # 100KB
DEFAULT_EXTENSIONS=".md,.txt,.csv,.json,.yaml,.yml"
DEFAULT_IGNORE="node_modules,.git,dist,build,.next,__pycache__,vendor,.venv,.claude,coverage,.turbo,.cache"
DEFAULT_MAX_FILES=5
DEFAULT_MAX_DEPTH=2

# Counters
SCANNED=0
INGESTED=0
SKIPPED=0
ERRORS=0

# --- Parse arguments ---
DIR="."
MAX_FILES="$DEFAULT_MAX_FILES"
MAX_DEPTH="$DEFAULT_MAX_DEPTH"
EXTENSIONS="$DEFAULT_EXTENSIONS"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dir) DIR="$2"; shift 2 ;;
    --max-files) MAX_FILES="$2"; shift 2 ;;
    --max-depth) MAX_DEPTH="$2"; shift 2 ;;
    --extensions) EXTENSIONS="$2"; shift 2 ;;
    -h|--help)
      echo "Usage: wuphf-scan-files.sh [OPTIONS]"
      echo "  --dir PATH        Directory to scan (default: .)"
      echo "  --max-files N     Max files to ingest per scan (default: 5)"
      echo "  --max-depth N     Max directory depth (default: 2)"
      echo "  --extensions LIST Comma-separated extensions (default: .md,.txt,.csv,.json,.yaml,.yml)"
      echo "  -h, --help        Show this help"
      exit 0
      ;;
    *) echo "Unknown option: $1. Use --help for usage." >&2; exit 1 ;;
  esac
done

# Validate directory
if [[ ! -d "$DIR" ]]; then
  echo "Error: Directory '$DIR' does not exist" >&2
  exit 1
fi
DIR="$(cd "$DIR" && pwd)"

# --- Ensure manifest directory exists ---
mkdir -p "$HOME/.wuphf"
if [[ ! -f "$MANIFEST_FILE" ]]; then
  echo '{"version":1,"files":{}}' > "$MANIFEST_FILE"
fi

# --- Validate and parse extensions ---
IFS=',' read -ra EXT_ARRAY <<< "$EXTENSIONS"
for ext in "${EXT_ARRAY[@]}"; do
  ext="$(echo "$ext" | xargs)"
  if [[ ! "$ext" =~ ^\.[a-zA-Z0-9]+$ ]]; then
    echo "Error: Invalid extension '$ext'. Must match pattern '.<alphanumeric>'" >&2
    exit 1
  fi
done

# --- Build find args as arrays (safe from injection) ---
IFS=',' read -ra IGNORE_ARRAY <<< "$DEFAULT_IGNORE"

FIND_ARGS=("$DIR" "-maxdepth" "$MAX_DEPTH")

# Add prune patterns
for ign in "${IGNORE_ARRAY[@]}"; do
  ign="$(echo "$ign" | xargs)"
  FIND_ARGS+=("-name" "$ign" "-prune" "-o")
done

# Add extension patterns
FIND_ARGS+=("-type" "f" "(")
first=true
for ext in "${EXT_ARRAY[@]}"; do
  ext="$(echo "$ext" | xargs)"
  if [ "$first" = true ]; then
    first=false
  else
    FIND_ARGS+=("-o")
  fi
  FIND_ARGS+=("-name" "*${ext}")
done
FIND_ARGS+=(")" "-print")

# --- Find files ---
FILES=$(find "${FIND_ARGS[@]}" 2>/dev/null || true)

if [[ -z "$FILES" ]]; then
  echo '{"scanned":0,"ingested":0,"skipped":0,"errors":0}'
  exit 0
fi

# --- Check each file against manifest ---
while IFS= read -r filepath; do
  SCANNED=$((SCANNED + 1))

  # Get file stats
  if [[ "$(uname)" == "Darwin" ]]; then
    FILE_SIZE=$(stat -f%z "$filepath" 2>/dev/null || echo 0)
    FILE_MTIME=$(stat -f%m "$filepath" 2>/dev/null || echo 0)
  else
    FILE_SIZE=$(stat -c%s "$filepath" 2>/dev/null || echo 0)
    FILE_MTIME=$(stat -c%Y "$filepath" 2>/dev/null || echo 0)
  fi

  # Check manifest for existing entry
  MANIFEST_MTIME=$(jq -r --arg p "$filepath" '.files[$p].mtime // 0' "$MANIFEST_FILE" 2>/dev/null || echo 0)
  MANIFEST_SIZE=$(jq -r --arg p "$filepath" '.files[$p].size // 0' "$MANIFEST_FILE" 2>/dev/null || echo 0)

  # Skip if unchanged (manifest stores mtime in ms)
  FILE_MTIME_MS_CHECK=$((FILE_MTIME * 1000))
  if [[ "$FILE_MTIME_MS_CHECK" == "$MANIFEST_MTIME" && "$FILE_SIZE" == "$MANIFEST_SIZE" ]]; then
    SKIPPED=$((SKIPPED + 1))
    continue
  fi

  # Stop if we've hit max files
  if [[ $INGESTED -ge $MAX_FILES ]]; then
    SKIPPED=$((SKIPPED + 1))
    continue
  fi

  # Read file content (truncate if too large)
  CONTENT=$(head -c "$MAX_FILE_SIZE" "$filepath" 2>/dev/null || true)
  if [[ ${#CONTENT} -ge $MAX_FILE_SIZE ]]; then
    CONTENT="$CONTENT
[...truncated]"
  fi

  # Get relative path for context tag
  REL_PATH="${filepath#$DIR/}"
  CONTEXT="file-scan:$REL_PATH"

  # Ingest via API
  JSON_BODY=$(jq -n --arg content "$CONTENT" --arg context "$CONTEXT" '{content: $content, context: $context}')
  if printf '%s' "$JSON_BODY" | bash "$SCRIPT_DIR/wuphf-api.sh" POST /v1/context/text >/dev/null 2>&1; then
    INGESTED=$((INGESTED + 1))

    # Update manifest (mtime in ms to match Node.js format)
    FILE_MTIME_MS=$((FILE_MTIME * 1000))
    TMP=$(mktemp)
    jq --arg p "$filepath" --argjson mt "$FILE_MTIME_MS" --argjson sz "$FILE_SIZE" --arg ctx "$CONTEXT" --argjson now "$(date +%s)000" \
      '.files[$p] = {mtime: $mt, size: $sz, ingestedAt: $now, context: $ctx}' \
      "$MANIFEST_FILE" > "$TMP" && mv "$TMP" "$MANIFEST_FILE"
  else
    ERRORS=$((ERRORS + 1))
    echo "Failed to ingest: $REL_PATH" >&2
  fi
done <<< "$FILES"

# --- Output summary ---
echo "{\"scanned\":$SCANNED,\"ingested\":$INGESTED,\"skipped\":$SKIPPED,\"errors\":$ERRORS}"
