#!/usr/bin/env bash
# Nex file scanner — discover text files and ingest new/changed ones
# ENV: NEX_API_KEY (required), NEX_SCAN_* (optional config)
# WRITES: ~/.nex/file-scan-manifest.json, POST /v1/context/text
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# --- Defaults from env ---
SCAN_DIR="${1:-.}"
EXTENSIONS="${NEX_SCAN_EXTENSIONS:-.md,.txt,.rtf,.html,.htm,.csv,.tsv,.json,.yaml,.yml,.toml,.xml,.js,.ts,.jsx,.tsx,.py,.rb,.go,.rs,.java,.sh,.bash,.zsh,.fish,.org,.rst,.adoc,.tex,.log,.env,.ini,.cfg,.conf,.properties}"
MAX_FILES="${NEX_SCAN_MAX_FILES:-5}"
MAX_DEPTH="${NEX_SCAN_DEPTH:-20}"
FORCE=false
DRY_RUN=false
MANIFEST_PATH="${HOME}/.nex/file-scan-manifest.json"

# --- Parse arguments ---
shift 2>/dev/null || true
while [[ $# -gt 0 ]]; do
  case "$1" in
    --dir)       SCAN_DIR="$2"; shift 2 ;;
    --extensions) EXTENSIONS="$2"; shift 2 ;;
    --max-files) MAX_FILES="$2"; shift 2 ;;
    --depth)     MAX_DEPTH="$2"; shift 2 ;;
    --force)     FORCE=true; shift ;;
    --dry-run)   DRY_RUN=true; shift ;;
    *)           echo "Unknown option: $1" >&2; exit 2 ;;
  esac
done

# --- Check scan enabled ---
if [[ "${NEX_SCAN_ENABLED:-true}" == "false" ]]; then
  echo '{"status":"disabled"}'
  exit 0
fi

# --- Validate environment ---
if [[ "$DRY_RUN" == "false" && -z "${NEX_API_KEY:-}" ]]; then
  echo "Error: NEX_API_KEY environment variable is not set" >&2
  exit 1
fi

# --- Ensure jq is available ---
if ! command -v jq &>/dev/null; then
  echo "Error: jq is required but not installed" >&2
  exit 1
fi

# --- Ensure manifest directory exists ---
mkdir -p "$(dirname "$MANIFEST_PATH")"

# --- Load or initialize manifest ---
if [[ -f "$MANIFEST_PATH" ]] && [[ "$FORCE" == "false" ]]; then
  MANIFEST=$(cat "$MANIFEST_PATH")
else
  MANIFEST='{"version":1,"files":{}}'
fi

# --- Build find extensions filter ---
SCAN_DIR="$(cd "$SCAN_DIR" && pwd)"

# Build -name patterns from extensions
IFS=',' read -ra EXT_ARR <<< "$EXTENSIONS"
FIND_NAMES=()
for ext in "${EXT_ARR[@]}"; do
  ext="$(echo "$ext" | xargs)"  # trim whitespace
  [[ "$ext" != .* ]] && ext=".$ext"
  FIND_NAMES+=(-name "*${ext}" -o)
done
# Remove trailing -o
unset 'FIND_NAMES[${#FIND_NAMES[@]}-1]'

# Skip directories
PRUNE_DIRS=(-name node_modules -o -name .git -o -name dist -o -name build -o -name .next -o -name __pycache__ -o -name .venv -o -name .cache -o -name .turbo -o -name coverage)

# --- Discover files ---
DISCOVERED=$(find "$SCAN_DIR" -maxdepth "$MAX_DEPTH" \
  \( "${PRUNE_DIRS[@]}" \) -prune -o \
  -type f \( "${FIND_NAMES[@]}" \) -print0 2>/dev/null | \
  xargs -0 stat -f '%m %z %N' 2>/dev/null | \
  sort -rn | head -n "$MAX_FILES")

if [[ -z "$DISCOVERED" ]]; then
  echo '{"scanned":0,"skipped":0,"errors":0,"files":[]}'
  exit 0
fi

# --- Hash function (macOS shasum / Linux sha256sum) ---
hash_file() {
  if command -v shasum &>/dev/null; then
    shasum -a 256 "$1" | cut -d' ' -f1
  else
    sha256sum "$1" | cut -d' ' -f1
  fi
}

# --- Process files ---
SCANNED=0
SKIPPED=0
ERRORS=0
FILES_JSON="[]"

while IFS= read -r line; do
  # Parse: mtime size path
  MTIME=$(echo "$line" | awk '{print $1}')
  SIZE=$(echo "$line" | awk '{print $2}')
  FILE_PATH=$(echo "$line" | awk '{$1=$2=""; print}' | sed 's/^ *//')

  [[ -z "$FILE_PATH" ]] && continue
  [[ ! -f "$FILE_PATH" ]] && continue

  # Hash
  HASH="sha256-$(hash_file "$FILE_PATH")"

  # Check manifest
  EXISTING_HASH=$(echo "$MANIFEST" | jq -r --arg p "$FILE_PATH" '.files[$p].hash // ""')

  if [[ "$EXISTING_HASH" == "$HASH" && "$FORCE" == "false" ]]; then
    SKIPPED=$((SKIPPED + 1))
    FILES_JSON=$(echo "$FILES_JSON" | jq --arg p "$FILE_PATH" '. + [{"path":$p,"status":"skipped","reason":"unchanged"}]')
    continue
  fi

  # Check non-empty
  CONTENT=$(cat "$FILE_PATH")
  if [[ -z "${CONTENT// /}" ]]; then
    SKIPPED=$((SKIPPED + 1))
    FILES_JSON=$(echo "$FILES_JSON" | jq --arg p "$FILE_PATH" '. + [{"path":$p,"status":"skipped","reason":"empty"}]')
    continue
  fi

  if [[ "$DRY_RUN" == "true" ]]; then
    REASON="new"
    [[ -n "$EXISTING_HASH" ]] && REASON="changed"
    SCANNED=$((SCANNED + 1))
    FILES_JSON=$(echo "$FILES_JSON" | jq --arg p "$FILE_PATH" --arg r "$REASON" '. + [{"path":$p,"status":"would_ingest","reason":$r}]')
    continue
  fi

  # Ingest via API
  CONTEXT="file-scan:${FILE_PATH}"
  BODY=$(jq -n --arg c "$CONTENT" --arg ctx "$CONTEXT" '{content:$c,context:$ctx}')

  if printf '%s' "$BODY" | bash "$SCRIPT_DIR/nex-api.sh" POST /v1/context/text >/dev/null 2>&1; then
    SCANNED=$((SCANNED + 1))
    REASON="new"
    [[ -n "$EXISTING_HASH" ]] && REASON="changed"
    FILES_JSON=$(echo "$FILES_JSON" | jq --arg p "$FILE_PATH" --arg r "$REASON" '. + [{"path":$p,"status":"ingested","reason":$r}]')

    # Update manifest
    NOW=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    MANIFEST=$(echo "$MANIFEST" | jq \
      --arg p "$FILE_PATH" \
      --arg h "$HASH" \
      --arg s "$SIZE" \
      --arg t "$NOW" \
      '.files[$p] = {hash:$h, size:($s|tonumber), scanned_at:$t}')
  else
    ERRORS=$((ERRORS + 1))
    FILES_JSON=$(echo "$FILES_JSON" | jq --arg p "$FILE_PATH" '. + [{"path":$p,"status":"error","reason":"ingest failed"}]')
  fi
done <<< "$DISCOVERED"

# --- Save manifest ---
if [[ "$DRY_RUN" == "false" ]]; then
  echo "$MANIFEST" > "$MANIFEST_PATH"
fi

# --- Output ---
jq -n \
  --argjson scanned "$SCANNED" \
  --argjson skipped "$SKIPPED" \
  --argjson errors "$ERRORS" \
  --argjson files "$FILES_JSON" \
  '{scanned:$scanned, skipped:$skipped, errors:$errors, files:$files}'
