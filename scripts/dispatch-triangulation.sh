#!/usr/bin/env bash
# Dispatch read-only Codex agents with orthogonal lenses and synthesize overlap.

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/.." && pwd)"
invocation_dir="$PWD"

problem_file=""
lenses_csv="security,perf,api,sre,architecture"
output_dir=""
sandbox="${CODEX_SANDBOX:-read-only}"
profile="${CODEX_PROFILE:-auto}"
timeout_seconds="${CODEX_TRIANGULATION_TIMEOUT_SECONDS:-300}"

pids=()
lenses=()
safe_lenses=()
reports=()
logs=()
status_files=()
timeout_markers=()
start_times=()
done_flags=()
statuses=()

usage() {
  cat <<'USAGE'
Usage:
  bash scripts/dispatch-triangulation.sh \
    --problem-file /path/to/problem.md \
    --lenses "security,perf,api,sre" \
    --output-dir /tmp/triangulation-XYZ

Options:
  --problem-file PATH      Problem statement to review from multiple lenses.
  --lenses CSV            Comma-separated lenses. Defaults to security,perf,api,sre,architecture.
  --output-dir DIR        Directory for <lens>-report.md and SYNTHESIS.md.
  --sandbox MODE          Codex sandbox mode. Defaults to read-only.
  --profile NAME          Codex config profile. Defaults to auto.
  --timeout-seconds N     Per-agent timeout. Defaults to 300.
  -h, --help              Show this help.
USAGE
}

die() {
  echo "dispatch-triangulation: $*" >&2
  exit 2
}

resolve_user_path() {
  case "$1" in
    /*) printf '%s\n' "$1" ;;
    *) printf '%s/%s\n' "$invocation_dir" "$1" ;;
  esac
}

trim() {
  printf '%s' "$1" | sed -E 's/^[[:space:]]+//; s/[[:space:]]+$//'
}

safe_name() {
  printf '%s' "$1" | tr -c 'A-Za-z0-9_.-' '-'
}

lens_preamble() {
  case "$1" in
    security)
      printf '%s\n' "Find every way an adversary could exploit this. Assume the input is hostile."
      ;;
    perf)
      printf '%s\n' "Find every hot path. What's the cost shape on inputs 100x larger? What's allocated per-call vs. amortized?"
      ;;
    api)
      printf '%s\n' "What's the wire-shape stability story? What does a Go/Rust implementer need? What's the upgrade path?"
      ;;
    sre)
      printf '%s\n' "What does on-call see at 3am when this breaks? Is the failure mode parseable? What's the recovery story?"
      ;;
    types)
      printf '%s\n' "Are the brands actually structurally distinct, or decorative? Where can \`as\` casts forge invariants?"
      ;;
    architecture)
      printf '%s\n' "Is this cohesive? Does it follow existing patterns? Where will future contributors get confused?"
      ;;
    distsys)
      printf '%s\n' "Cross-process invariants? Idempotency? Race windows? Recovery semantics on partial failure?"
      ;;
    *)
      return 1
      ;;
  esac
}

cleanup_children() {
  local pid
  for pid in "${pids[@]}"; do
    kill "$pid" 2>/dev/null || true
  done
}

compose_prompt() {
  local lens="$1"
  local preamble="$2"
  local prompt_path="$3"

  cat > "$prompt_path" <<PROMPT
You are one member of a triangulation review. Several agents are reviewing
the same problem from orthogonal lenses. Work independently and optimize for
the strongest findings from your assigned frame.

## Your lens
${lens}

${preamble}

## Problem statement
${problem}

## Instructions
- Run in review mode only. Do not edit files.
- Prefer concrete, reviewable findings over broad advice.
- Cite exact file:line references whenever the problem statement or repository
  context gives you enough information. The synthesis step groups reports by
  matching file:line strings.
- Call out uncertainty instead of inventing missing context.

## Output format

For each finding:

### Finding N: <one-line title>
- **Severity**: BLOCK / HIGH / MEDIUM / LOW
- **Evidence**: include the file:line reference or concrete behavior
- **Reasoning**: why this matters through your lens
- **Suggestion**: minimal fix or next check

End with:
- **Verdict**: Ready / Ship with follow-ups / Hold for fixes

Cap output at 1200 words. If you find nothing from your lens, say so explicitly.
PROMPT
}

write_failure_report() {
  local report="$1"
  local lens="$2"
  local log="$3"
  local status="$4"

  cat > "$report" <<FAIL
### Finding 1: ${lens} agent failed to complete
- **Severity**: HIGH
- **Evidence**: codex exec exited with status ${status}. See ${log}.
- **Reasoning**: The triangulation result is missing this independent lens.
- **Suggestion**: Re-run this lens or inspect the Codex execution log.

- **Verdict**: Hold for fixes
FAIL
}

write_timeout_report() {
  local report="$1"
  local lens="$2"
  local timeout="$3"

  cat > "$report" <<TIMEOUT
### Finding 1: ${lens} agent timed out
- **Severity**: HIGH
- **Evidence**: No final report was produced within ${timeout}s.
- **Reasoning**: The triangulation result is missing this independent lens.
- **Suggestion**: Re-run this lens with a narrower prompt or a longer timeout.

- **Verdict**: Hold for fixes
TIMEOUT
}

severity_for_location() {
  local report="$1"
  local location="$2"

  awk -v loc="$location" '
    /^- \*\*Severity\*\*:/ { severity = $0 }
    index($0, loc) {
      found = 1
      if (severity == "") {
        print "UNKNOWN"
      } else {
        sub(/^.*\*\*Severity\*\*:[[:space:]]*/, "", severity)
        print severity
      }
      exit
    }
    END {
      if (!found) {
        print "UNKNOWN"
      }
    }
  ' "$report"
}

write_synthesis() {
  local synthesis="$output_dir/SYNTHESIS.md"
  local locations_file="$status_dir/locations.tsv"
  local severities_file="$status_dir/severities.tsv"
  local location_regex='[A-Za-z0-9_./@+-]+\.[A-Za-z0-9_+-]+:[0-9]+(:[0-9]+)?'
  local i report lens found_locations location high_locations unique_locations

  : > "$locations_file"
  : > "$severities_file"

  for i in "${!reports[@]}"; do
    report="${reports[$i]}"
    lens="${lenses[$i]}"
    [[ -f "$report" ]] || continue

    found_locations="$(grep -Eo "$location_regex" "$report" 2>/dev/null | sort -u || true)"
    while IFS= read -r location; do
      [[ -n "$location" ]] || continue
      printf '%s\t%s\t%s\n' "$location" "$lens" "$report" >> "$locations_file"
      printf '%s\t%s\t%s\n' "$location" "$lens" "$(severity_for_location "$report" "$location")" >> "$severities_file"
    done <<< "$found_locations"
  done

  {
    echo "# Triangulation Synthesis"
    echo
    echo "- Problem file: ${problem_path}"
    echo "- Lenses: ${lenses[*]}"
    echo "- Comparator: exact file:line text matches across reports"
    echo
    echo "## High-confidence findings (2+ reports)"

    if [[ -s "$locations_file" ]]; then
      high_locations="$(cut -f1 "$locations_file" | sort | uniq -c | awk '$1 >= 2 {print $2}')"
      if [[ -n "$high_locations" ]]; then
        while IFS= read -r location; do
          [[ -n "$location" ]] || continue
          count="$(awk -F '\t' -v loc="$location" '$1 == loc {print $2}' "$locations_file" | sort -u | wc -l | tr -d ' ')"
          lens_list="$(awk -F '\t' -v loc="$location" '$1 == loc {print $2}' "$locations_file" | sort -u | tr '\n' ',' | sed 's/,$//; s/,/, /g')"
          echo "- ${location} (${count} reports: ${lens_list})"
          awk -F '\t' -v loc="$location" '$1 == loc {print $2 "\t" $3}' "$locations_file" | sort -u | while IFS=$'\t' read -r matching_lens matching_report; do
            excerpt="$(grep -F "$location" "$matching_report" | head -1 | sed -E 's/^[[:space:]]+//')"
            echo "  - ${matching_lens}: ${excerpt}"
          done
        done <<< "$high_locations"
      else
        echo "- None found by file:line matching."
      fi
    else
      echo "- None found. Reports may lack file:line references; inspect individual reports."
    fi

    echo
    echo "## Unique findings (one report)"

    if [[ -s "$locations_file" ]]; then
      unique_locations="$(cut -f1 "$locations_file" | sort | uniq -c | awk '$1 == 1 {print $2}')"
      if [[ -n "$unique_locations" ]]; then
        while IFS= read -r location; do
          [[ -n "$location" ]] || continue
          lens_list="$(awk -F '\t' -v loc="$location" '$1 == loc {print $2}' "$locations_file" | sort -u | tr '\n' ',' | sed 's/,$//; s/,/, /g')"
          echo "- ${location} (${lens_list})"
        done <<< "$unique_locations"
      else
        echo "- None found by file:line matching."
      fi
    else
      echo "- None found by file:line matching."
    fi

    echo
    echo "## Direct disagreements"

    disagreement_count=0
    if [[ -s "$severities_file" ]]; then
      while IFS= read -r location; do
        [[ -n "$location" ]] || continue
        severity_count="$(awk -F '\t' -v loc="$location" '$1 == loc {print $3}' "$severities_file" | sort -u | wc -l | tr -d ' ')"
        if [[ "$severity_count" -gt 1 ]]; then
          severity_list="$(awk -F '\t' -v loc="$location" '$1 == loc {print $2 ":" $3}' "$severities_file" | sort -u | tr '\n' ',' | sed 's/,$//; s/,/, /g')"
          echo "- ${location} has severity divergence: ${severity_list}"
          disagreement_count=$((disagreement_count + 1))
        fi
      done < <(cut -f1 "$locations_file" | sort -u)
    fi

    marker_hits="$(grep -ERHin 'disagree|contradict|conflict|false positive|not an issue|safe because|acceptable because|no bypass' "${reports[@]}" 2>/dev/null || true)"
    if [[ -n "$marker_hits" ]]; then
      echo "$marker_hits" | sed 's/^/- marker: /'
      disagreement_count=$((disagreement_count + 1))
    fi

    if [[ "$disagreement_count" -eq 0 ]]; then
      echo "- None detected by severity or disagreement-marker grep."
    fi

    echo
    echo "## Agent statuses"
    for i in "${!lenses[@]}"; do
      echo "- ${lenses[$i]}: status ${statuses[$i]} (${reports[$i]})"
    done
  } > "$synthesis"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --problem-file)
      [[ $# -ge 2 ]] || die "--problem-file requires a value"
      problem_file="$2"
      shift 2
      ;;
    --lenses)
      [[ $# -ge 2 ]] || die "--lenses requires a value"
      lenses_csv="$2"
      shift 2
      ;;
    --output-dir)
      [[ $# -ge 2 ]] || die "--output-dir requires a value"
      output_dir="$2"
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
    --timeout-seconds)
      [[ $# -ge 2 ]] || die "--timeout-seconds requires a value"
      timeout_seconds="$2"
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

[[ -n "$problem_file" ]] || die "--problem-file is required"
[[ -n "$output_dir" ]] || die "--output-dir is required"
[[ "$timeout_seconds" =~ ^[0-9]+$ ]] || die "--timeout-seconds must be an integer"
[[ "$timeout_seconds" -gt 0 ]] || die "--timeout-seconds must be greater than zero"

command -v codex >/dev/null 2>&1 || die "codex CLI is required"

problem_path="$(resolve_user_path "$problem_file")"
output_dir="$(resolve_user_path "$output_dir")"

[[ -f "$problem_path" ]] || die "problem file not found: $problem_file"

problem="$(< "$problem_path")"

IFS=',' read -r -a raw_lenses <<< "$lenses_csv"
for raw_lens in "${raw_lenses[@]}"; do
  lens="$(trim "$raw_lens")"
  [[ -n "$lens" ]] || continue
  lens_preamble "$lens" >/dev/null || die "unknown lens: $lens"
  case " ${lenses[*]} " in
    *" ${lens} "*) die "duplicate lens: $lens" ;;
  esac
  lenses+=("$lens")
  safe_lenses+=("$(safe_name "$lens")")
done

[[ "${#lenses[@]}" -gt 0 ]] || die "--lenses did not name any lenses"

mkdir -p "$output_dir"
prompt_dir="$output_dir/.prompts"
log_dir="$output_dir/.logs"
status_dir="$output_dir/.status"
mkdir -p "$prompt_dir" "$log_dir" "$status_dir"

trap cleanup_children INT TERM

for i in "${!lenses[@]}"; do
  lens="${lenses[$i]}"
  safe_lens="${safe_lenses[$i]}"
  preamble="$(lens_preamble "$lens")"
  prompt="$prompt_dir/${safe_lens}-prompt.md"
  report="$output_dir/${safe_lens}-report.md"
  log="$log_dir/${safe_lens}.log"
  status_file="$status_dir/${safe_lens}.status"
  timeout_marker="$status_dir/${safe_lens}.timeout"

  rm -f "$status_file" "$timeout_marker"
  compose_prompt "$lens" "$preamble" "$prompt"

  reports+=("$report")
  logs+=("$log")
  status_files+=("$status_file")
  timeout_markers+=("$timeout_marker")
  start_times+=("$(date +%s)")
  done_flags+=("0")
  statuses+=("running")

  echo "dispatch-triangulation: starting ${lens} agent" >&2
  (
    set +e
    child_pid=""
    trap 'if [[ -n "$child_pid" ]]; then kill "$child_pid" 2>/dev/null || true; fi' TERM INT

    codex exec \
      --sandbox "$sandbox" \
      --profile "$profile" \
      --cd "$repo_root" \
      --output-last-message "$report" \
      - < "$prompt" > "$log" 2>&1 &
    child_pid=$!
    wait "$child_pid"
    status=$?

    if [[ "$status" -eq 0 && ! -s "$report" ]]; then
      status=1
      write_failure_report "$report" "$lens" "$log" "$status"
    elif [[ "$status" -ne 0 && ! -f "$timeout_marker" ]]; then
      write_failure_report "$report" "$lens" "$log" "$status"
    fi

    printf '%s\n' "$status" > "$status_file"
    exit "$status"
  ) &
  pids+=("$!")
done

remaining="${#pids[@]}"
while [[ "$remaining" -gt 0 ]]; do
  now="$(date +%s)"

  for i in "${!pids[@]}"; do
    [[ "${done_flags[$i]}" == "0" ]] || continue

    if [[ -f "${status_files[$i]}" ]]; then
      statuses[$i]="$(< "${status_files[$i]}")"
      wait "${pids[$i]}" 2>/dev/null || true
      done_flags[$i]="1"
      remaining=$((remaining - 1))
      echo "dispatch-triangulation: ${lenses[$i]} agent finished with status ${statuses[$i]}" >&2
      continue
    fi

    elapsed=$((now - start_times[$i]))
    if [[ "$elapsed" -ge "$timeout_seconds" ]]; then
      echo "dispatch-triangulation: ${lenses[$i]} agent timed out after ${timeout_seconds}s" >&2
      touch "${timeout_markers[$i]}"
      kill "${pids[$i]}" 2>/dev/null || true
      sleep 1
      kill -9 "${pids[$i]}" 2>/dev/null || true
      wait "${pids[$i]}" 2>/dev/null || true
      write_timeout_report "${reports[$i]}" "${lenses[$i]}" "$timeout_seconds"
      printf '124\n' > "${status_files[$i]}"
      statuses[$i]="124"
      done_flags[$i]="1"
      remaining=$((remaining - 1))
    fi
  done

  [[ "$remaining" -eq 0 ]] || sleep 2
done

write_synthesis
cat "$output_dir/SYNTHESIS.md"
