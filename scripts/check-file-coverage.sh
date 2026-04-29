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
# input.

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
  go test ${GO_TEST_FLAGS:-} -coverprofile="$profile" -coverpkg="$pkg" "$pkg" >/dev/null
fi

# Module path is the prefix `go tool cover -func` prints before each file
# entry, so we need it to match against the relative paths the caller
# passes in --files.
module="$(go list -m 2>/dev/null)"
if [ -z "$module" ]; then
  echo "check-file-coverage: could not determine module (run inside a Go module)" >&2
  exit 2
fi

failures=0
total=0

# Convert "a,b,c" to a newline-separated list and iterate.
echo "$files_csv" | tr ',' '\n' | while IFS= read -r rel; do
  [ -z "$rel" ] && continue
  total=$((total + 1))
  # `go tool cover -func` lines look like:
  #   github.com/nex-crm/wuphf/internal/team/prompt_builder.go:42:    Build         100.0%
  # The trailing "total:" line is per-package, not per-file, so we filter
  # it out by requiring a colon-delimited line number.
  awk_out="$(go tool cover -func="$profile" \
    | awk -v prefix="${module}/${rel}:" '
        index($1, prefix) == 1 {
          gsub("%", "", $NF)
          covered += $NF
          n += 1
        }
        END {
          if (n == 0) { print "MISSING"; exit }
          printf "%.1f %d\n", covered / n, n
        }')"

  if [ "$awk_out" = "MISSING" ]; then
    printf '  %-60s  no funcs in profile (file not covered by --pkg?)\n' "$rel" >&2
    failures=$((failures + 1))
    # Subshell — write progress to a tempfile so the parent can see it.
    echo "$failures" > /tmp/wuphf-cov-failures
    continue
  fi

  pct="$(echo "$awk_out" | awk '{print $1}')"
  funcs="$(echo "$awk_out" | awk '{print $2}')"
  if awk -v p="$pct" -v m="$min" 'BEGIN { exit !(p + 0 < m + 0) }'; then
    printf '  %-60s  %s%%  (%s funcs)  BELOW %s%%\n' "$rel" "$pct" "$funcs" "$min" >&2
    failures=$((failures + 1))
  else
    printf '  %-60s  %s%%  (%s funcs)  OK\n' "$rel" "$pct" "$funcs"
  fi
  echo "$failures" > /tmp/wuphf-cov-failures
done

failures="$(cat /tmp/wuphf-cov-failures 2>/dev/null || echo 0)"
rm -f /tmp/wuphf-cov-failures

if [ "$failures" -gt 0 ]; then
  echo "check-file-coverage: $failures file(s) below ${min}% floor" >&2
  exit 1
fi
echo "check-file-coverage: all listed files >= ${min}%"
