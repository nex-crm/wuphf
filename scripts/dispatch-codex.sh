#!/usr/bin/env bash
# dispatch-codex.sh - run codex exec with the protocol review preamble.
#
# Usage (works from any CWD):
#   bash "$(git rev-parse --show-toplevel)/scripts/dispatch-codex.sh" \
#     --worktree /path/to/worktree \
#     --prompt /path/to/task-prompt.md \
#     --output /path/to/output.md \
#     [--profile auto] [--sandbox workspace-write]
#
# Or, from the repo root:
#   bash scripts/dispatch-codex.sh ...

set -euo pipefail

# Anchor relative paths in the preamble to the repo root, regardless of CWD.
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"

usage() {
  cat >&2 <<'USAGE'
Usage (works from any CWD):
  bash "$(git rev-parse --show-toplevel)/scripts/dispatch-codex.sh" \
    --worktree /path/to/worktree \
    --prompt /path/to/task-prompt.md \
    --output /path/to/output.md \
    [--profile auto] [--sandbox workspace-write]
USAGE
}

require_value() {
  if [ "$#" -lt 2 ] || [ -z "${2:-}" ]; then
    echo "dispatch-codex: missing value for $1" >&2
    usage
    exit 2
  fi
}

worktree=""
prompt_path=""
output_path=""
profile=""
sandbox="workspace-write"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --worktree)
      require_value "$@"
      worktree="${2:-}"
      shift 2
      ;;
    --prompt)
      require_value "$@"
      prompt_path="${2:-}"
      shift 2
      ;;
    --output)
      require_value "$@"
      output_path="${2:-}"
      shift 2
      ;;
    --profile)
      require_value "$@"
      profile="${2:-}"
      shift 2
      ;;
    --sandbox)
      require_value "$@"
      sandbox="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "dispatch-codex: unknown argument: $1" >&2
      usage
      exit 2
      ;;
  esac
done

if [ -z "$worktree" ] || [ -z "$prompt_path" ] || [ -z "$output_path" ]; then
  echo "dispatch-codex: --worktree, --prompt, and --output are required" >&2
  usage
  exit 2
fi

if [ ! -d "$worktree" ]; then
  echo "dispatch-codex: worktree does not exist: $worktree" >&2
  exit 2
fi

if [ ! -f "$prompt_path" ]; then
  echo "dispatch-codex: prompt file does not exist: $prompt_path" >&2
  exit 2
fi

if ! git -C "$worktree" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "dispatch-codex: not a git worktree: $worktree" >&2
  exit 2
fi

if [ -n "$(git -C "$worktree" status --porcelain)" ]; then
  echo "dispatch-codex: worktree must be clean before dispatch: $worktree" >&2
  git -C "$worktree" status --short >&2
  exit 1
fi

standard_preamble="$(cat <<'PREAMBLE'
You are working in a git worktree. Read packages/protocol/AGENTS.md first
for the package conventions, AND the root AGENTS.md for the multi-round
review rhythm and sub-agent dispatch contract.

Before fixing any finding, verify it still applies against the current
code state — earlier rounds may have addressed it. Skip with reason
rather than fixing twice.

When you encounter design ambiguity:
- Pick the simpler option unless the prompt names a preferred choice
- Document your choice in the commit body

Verification (must all be green before commit):
- `cd packages/protocol && bunx tsc --noEmit`
- `cd packages/protocol && bunx biome check src/ tests/ scripts/`
- `bash scripts/test-protocol.sh` (from repo root)
- `bun run packages/protocol/scripts/demo.ts` (extend the demo if your
  changes added a new public-API surface)
- `cd packages/protocol/testdata && go run verifier-reference.go` (if
  you touched audit-event.ts, canonical-json.ts, event-lsn.ts, or testdata)

End your summary with a per-finding disposition table:
| # | Finding | Status | Notes |
|---|---------|--------|-------|
| 1 | <short> | FIXED   | commit X |
| 2 | <short> | SKIPPED | already addressed in commit Y |
| 3 | <short> | DEFERRED | tracking issue #N |
PREAMBLE
)"

combined_prompt="$(mktemp -t wuphf-dispatch-codex.XXXXXX)"
trap 'rm -f "$combined_prompt"' EXIT

{
  printf '%s\n\n' "$standard_preamble"
  printf '%s\n\n' "--- Task-specific prompt ---"
  sed -n '1,$p' "$prompt_path"
} > "$combined_prompt"

codex_args=(exec --cd "$worktree" --sandbox "$sandbox" --output-last-message "$output_path")
if [ -n "$profile" ]; then
  codex_args+=(--profile "$profile")
fi

status=0
codex "${codex_args[@]}" - < "$combined_prompt" || status=$?

latest_commit="$(git -C "$worktree" log --oneline -1 2>/dev/null || true)"
echo "dispatch-codex: latest commit in $worktree: ${latest_commit:-<none>}"

exit "$status"
