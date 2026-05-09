#!/usr/bin/env bash
# Dispatch a read-only Codex verification agent to stress-test a proposed fix.

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/.." && pwd)"
invocation_dir="$PWD"

solution_file=""
target_files=""
output_file=""
lens="general adversarial review"
sandbox="${CODEX_SANDBOX:-read-only}"
profile="${CODEX_PROFILE:-auto}"

usage() {
  cat <<'USAGE'
Usage:
  bash scripts/dispatch-verification-agent.sh \
    --solution-file /path/to/proposed-solution.md \
    --target-files "packages/protocol/src/audit-event.ts,packages/protocol/src/canonical-json.ts" \
    --output /path/to/verification-output.md \
    [--lens "security|perf|api|sre|types|architecture"]

Options:
  --solution-file PATH  Proposed solution, diff, snippet, or design note.
  --target-files CSV    Comma-separated existing files to include as context.
  --output PATH         File that receives the final verification report.
  --lens TEXT           Optional adversarial review lens.
  --sandbox MODE        Codex sandbox mode. Defaults to read-only.
  --profile NAME        Codex config profile. Defaults to auto.
  -h, --help            Show this help.
USAGE
}

die() {
  echo "dispatch-verification-agent: $*" >&2
  exit 2
}

resolve_user_path() {
  case "$1" in
    /*) printf '%s\n' "$1" ;;
    *) printf '%s/%s\n' "$invocation_dir" "$1" ;;
  esac
}

resolve_repo_path() {
  case "$1" in
    /*) printf '%s\n' "$1" ;;
    *) printf '%s/%s\n' "$repo_root" "$1" ;;
  esac
}

trim() {
  printf '%s' "$1" | sed -E 's/^[[:space:]]+//; s/[[:space:]]+$//'
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --solution-file)
      [[ $# -ge 2 ]] || die "--solution-file requires a value"
      solution_file="$2"
      shift 2
      ;;
    --target-files)
      [[ $# -ge 2 ]] || die "--target-files requires a value"
      target_files="$2"
      shift 2
      ;;
    --output)
      [[ $# -ge 2 ]] || die "--output requires a value"
      output_file="$2"
      shift 2
      ;;
    --lens)
      [[ $# -ge 2 ]] || die "--lens requires a value"
      lens="$2"
      shift 2
      ;;
    --sandbox)
      [[ $# -ge 2 ]] || die "--sandbox requires a value"
      sandbox="$2"
      shift 2
      ;;
    --profile)
      [[ $# -ge 2 ]] || die "--profile requires a value"
      profile="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

[[ -n "$solution_file" ]] || die "--solution-file is required"
[[ -n "$target_files" ]] || die "--target-files is required"
[[ -n "$output_file" ]] || die "--output is required"

command -v codex >/dev/null 2>&1 || die "codex CLI is required"

solution_path="$(resolve_user_path "$solution_file")"
output_path="$(resolve_user_path "$output_file")"

[[ -f "$solution_path" ]] || die "solution file not found: $solution_file"

solution="$(< "$solution_path")"
target_bundle=""
target_count=0

IFS=',' read -r -a target_list <<< "$target_files"
for raw_target in "${target_list[@]}"; do
  target="$(trim "$raw_target")"
  [[ -n "$target" ]] || continue

  target_path="$(resolve_repo_path "$target")"
  [[ -f "$target_path" ]] || die "target file not found: $target"

  target_contents="$(< "$target_path")"
  target_bundle="${target_bundle}
===== FILE: ${target} =====
${target_contents}
===== END FILE: ${target} =====
"
  target_count=$((target_count + 1))
done

[[ "$target_count" -gt 0 ]] || die "--target-files did not name any files"

prompt_file="$(mktemp "${TMPDIR:-/tmp}/verification-agent-prompt.XXXXXX")"
agent_output="$(mktemp "${TMPDIR:-/tmp}/verification-agent-output.XXXXXX")"
run_log="$(mktemp "${TMPDIR:-/tmp}/verification-agent-log.XXXXXX")"
trap 'rm -f "$prompt_file" "$agent_output" "$run_log"' EXIT

cat > "$prompt_file" <<PROMPT
You are a verification agent. Your job is to STRESS-TEST a proposed
solution by finding what it doesn't cover, what edge cases break it,
and what a thoughtful reviewer would flag.

You are NOT here to confirm. You are here to break.

## The proposed solution
${solution}

## The target files
${target_bundle}

## Your specific lens
${lens}

## What to look for
1. Threats the solution doesn't close — input the author didn't think of
2. Invariants that could be bypassed — \`Object.create(...prototype)\` style
3. Sequence races — what happens between step N and step N+1
4. Resource exhaustion — bounded? what's the unbound case?
5. Cross-cutting drift — does this solution match the rules in
   packages/protocol/AGENTS.md? Does it document its own assumptions?
6. Test gaps — what's NOT covered by the included tests?
7. Failure mode taxonomy — typed errors? structured? human-only?

## Output format

For each finding:

### Finding N: <one-line title>
- **Severity**: BLOCK / HIGH / MEDIUM / LOW
- **Evidence**: code snippet or behavior
- **Bypass**: how an adversary would exploit
- **Suggestion**: minimal fix

End with a one-sentence verdict:
- "Ready to ship — no bypass found in N minutes of stress-testing"
- "Ship with follow-ups — N findings, none blocking"
- "Hold for fixes — M findings would land in review"

Cap output at 1500 words. If you find nothing, say so explicitly. Don't pad.
PROMPT

mkdir -p "$(dirname "$output_path")"

if ! codex exec \
  --sandbox "$sandbox" \
  --profile "$profile" \
  --cd "$repo_root" \
  --output-last-message "$agent_output" \
  - < "$prompt_file" > "$run_log" 2>&1; then
  echo "dispatch-verification-agent: codex exec failed" >&2
  cat "$run_log" >&2
  exit 1
fi

if [[ ! -s "$agent_output" ]]; then
  echo "dispatch-verification-agent: codex completed without a final message" >&2
  cat "$run_log" >&2
  exit 1
fi

tee "$output_path" < "$agent_output"
