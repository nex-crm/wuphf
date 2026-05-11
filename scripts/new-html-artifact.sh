#!/usr/bin/env bash
# Create a dated self-contained HTML artifact from the repo template.

set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  bash scripts/new-html-artifact.sh <slug> [title]

Examples:
  bash scripts/new-html-artifact.sh runtime-explainer "Runtime explainer"
  ARTIFACT_DATE=2026-05-11 bash scripts/new-html-artifact.sh pr-42-review
USAGE
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

slug="${1:-}"
title="${2:-}"

if [[ -z "$slug" ]]; then
  usage >&2
  exit 2
fi

if [[ ! "$slug" =~ ^[a-z0-9][a-z0-9-]*$ ]]; then
  echo "new-html-artifact: slug must be lowercase kebab-case" >&2
  exit 2
fi

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/.." && pwd)"
template="$repo_root/docs/agent-artifacts/html-artifact-template.html"

if [[ ! -f "$template" ]]; then
  echo "new-html-artifact: missing template at $template" >&2
  exit 1
fi

date_part="${ARTIFACT_DATE:-$(date +%F)}"
out_dir="$repo_root/docs/agent-artifacts"
out="$out_dir/${date_part}-${slug}.html"

if [[ -e "$out" ]]; then
  echo "new-html-artifact: refusing to overwrite $out" >&2
  exit 1
fi

if [[ -z "$title" ]]; then
  title="$(printf '%s' "$slug" | tr '-' ' ')"
fi

escape_sed() {
  printf '%s' "$1" | sed 's/[\/&]/\\&/g'
}

summary="Replace with a one-sentence summary."
owner="${USER:-agent}"
sources="Replace with source files, commands, links, or prompts."

sed \
  -e "s/{{TITLE}}/$(escape_sed "$title")/g" \
  -e "s/{{SUMMARY}}/$(escape_sed "$summary")/g" \
  -e "s/{{DATE}}/$(escape_sed "$date_part")/g" \
  -e "s/{{OWNER}}/$(escape_sed "$owner")/g" \
  -e "s/{{SOURCES}}/$(escape_sed "$sources")/g" \
  "$template" > "$out"

echo "$out"
