#!/usr/bin/env bash
# check-file-coverage.sh — enforce a per-file coverage floor on a list of
# Go source files. Used during the launcher.go decomposition (PLAN.md) to
# gate that newly extracted files (prompt_builder.go, office_targets.go,
# scheduler.go, etc.) ship at >= 85% statement coverage while the residual
# launcher.go is exempt.
#
# Why per-file (not per-package): see PLAN.md §4. We want the new code to
# be high-coverage without holding it hostage to launcher.go's debt as
# functions migrate out. Once the migration is done this script retires
# in favor of a package-level floor.
#
# Usage:
#   scripts/check-file-coverage.sh \
#       --pkg ./internal/team \
#       --min 85 \
#       --files internal/team/prompt_builder.go,internal/team/office_targets.go
#
# Optional:
#   COVER_PROFILE=/path/to/cover.out  # reuse an existing profile instead of
#                                       running `go test -coverprofile`.
#   GO_TEST_FLAGS="-count=1 -timeout 5m"  # extra flags for go test.
#
# Exit codes: 0 = all listed files >= min, 1 = at least one below, 2 = bad
# input or coverage-generation failure.

set -uo pipefail

min=""
pkg=""
files_csv=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --min) min="$2"; shift 2 ;;
    --pkg) pkg="$2"; shift 2 ;;
    --files) files_csv="$2"; shift 2 ;;
    -h|--help)
      sed -n '2,28p' "$0"
      exit 0
      ;;
    *)
      echo "check-file-coverage: unknown flag: $1" >&2
      exit 2
      ;;
  esac
done

if [ -z "$min" ] || [ -z "$pkg" ] || [ -z "$files_csv" ]; then
  echo "check-file-coverage: --min, --pkg, and --files are required" >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root" || exit 2

profile="${COVER_PROFILE:-}"
if [ -z "$profile" ]; then
  profile="$(mktemp -t wuphf-cover.XXXXXX)"
  trap 'rm -f "$profile"' EXIT
  # shellcheck disable=SC2086
  if ! go test ${GO_TEST_FLAGS:-} -coverprofile="$profile" -coverpkg="$pkg" "$pkg" >/dev/null; then
    echo "check-file-coverage: failed to generate coverage profile (go test exit $?)" >&2
    exit 2
  fi
fi

# Module path is the prefix coverprofile entries carry before each file
# entry, so we need it to match against the relative paths the caller
# passes in --files.
module="$(go list -m 2>/dev/null)"
if [ -z "$module" ]; then
  echo "check-file-coverage: could not determine module (run inside a Go module)" >&2
  exit 2
fi

failures=0
total=0

# Run the loop in the parent shell via process substitution so updates to
# `failures` and `total` are visible after the loop ends. Avoids a shared
# /tmp sentinel that would clobber across concurrent invocations.
while IFS= read -r rel; do
  [ -z "$rel" ] && continue
  total=$((total + 1))
  # Coverprofile rows look like:
  #   github.com/nex-crm/wuphf/internal/team/prompt_builder.go:42.2,44.13 3 1
  # That's: file:start_line.col,end_line.col  numStmts  count.  We sum
  # numStmts per file (covered when count>0, total always) so the gate
  # measures *statement* coverage rather than averaged function %.
  awk_out="$(awk -v prefix="${module}/${rel}:" '
      $1 == "mode:" { next }
      index($1, prefix) == 1 {
        total += $2
        if ($3 > 0) { covered += $2 }
      }
      END {
        if (total == 0) { print "MISSING"; exit }
        printf "%.1f %d\n", (covered / total) * 100, total
      }
    ' "$profile")"

  if [ "$awk_out" = "MISSING" ]; then
    printf '  %-60s  no statements in profile (file not covered by --pkg?)\n' "$rel" >&2
    failures=$((failures + 1))
    continue
  fi

  pct="$(echo "$awk_out" | awk '{print $1}')"
  stmts="$(echo "$awk_out" | awk '{print $2}')"
  if awk -v p="$pct" -v m="$min" 'BEGIN { exit !(p + 0 < m + 0) }'; then
    printf '  %-60s  %s%%  (%s stmts)  BELOW %s%%\n' "$rel" "$pct" "$stmts" "$min" >&2
    failures=$((failures + 1))
  else
    printf '  %-60s  %s%%  (%s stmts)  OK\n' "$rel" "$pct" "$stmts"
  fi
done < <(printf '%s\n' "$files_csv" | tr ',' '\n')

if [ "$failures" -gt 0 ]; then
  echo "check-file-coverage: $failures file(s) below ${min}% floor" >&2
  exit 1
fi
echo "check-file-coverage: all listed files >= ${min}%"
