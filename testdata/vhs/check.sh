#!/usr/bin/env bash
# Visual regression check for wuphf.
#
# Re-runs each VHS tape and fails if the generated .txt golden drifts from
# the committed one. Pre-commit friendly — leaves the committed golden
# untouched on drift, and saves the new render next to it as
# <name>.txt.actual for inspection.
#
# Tapes covered:
#   help.tape     — `wuphf --help` output
#   version.tape  — `wuphf --version` output
#
# Only `.txt` is diffed. `.gif` files are regenerated on every run and are
# NOT tracked in git (see .gitignore). They exist only as a human-viewable
# byproduct of the VHS recording.
#
# When help.txt drifts in CI:
#   1. Download the `vhs-drift` artifact from the failing CI run.
#   2. Inspect testdata/vhs/<name>.txt.actual vs testdata/vhs/<name>.txt.
#   3. If the drift is intentional, replace the golden:
#        mv testdata/vhs/<name>.txt.actual testdata/vhs/<name>.txt
#        git add testdata/vhs/<name>.txt
#
# Version bumps in cmd/wuphf/buildinfo drift version.txt — update the golden
# as part of the version-bump commit.

set -euo pipefail

cd "$(dirname "$0")/../.."

TAPES=(help version)

# Compile once outside the tape so VHS recording time is just exec.
go build -o /tmp/wuphf-vhs ./cmd/wuphf

# VHS captures multiple scrollback frames separated by '────' lines.
# The PS1 prompt ('>') at the top of each scrollback frame is captured
# nondeterministically (sometimes blank, sometimes '>'), while the
# final rendered frame is byte-stable. Normalize by keeping only the
# content between the last two separators — that's the visible render
# we actually care about.
normalize() {
  local f="$1"
  if ! awk '
    NR==FNR {
      if (/^────/) { total++; prev_sep = last_sep; last_sep = NR }
      next
    }
    FNR > prev_sep && FNR < last_sep
    END { if (total < 2) exit 1 }
  ' "$f" "$f" > "${f}.norm"; then
    rm -f "${f}.norm"
    echo "normalize: $f has fewer than 2 '────' separators — VHS output malformed?" >&2
    return 1
  fi
  mv "${f}.norm" "$f"
}

check_one() {
  local name="$1"
  local golden_txt="testdata/vhs/${name}.txt"
  local actual_txt="testdata/vhs/${name}.txt.actual"

  if [[ ! -f "$golden_txt" ]]; then
    echo "golden missing: $golden_txt" >&2
    return 1
  fi

  local backup_txt
  backup_txt="$(mktemp)"
  cp "$golden_txt" "$backup_txt"

  vhs "testdata/vhs/${name}.tape" >/dev/null
  if ! normalize "$golden_txt"; then
    cp "$backup_txt" "$golden_txt"
    rm -f "$backup_txt"
    echo "wuphf visual regression: ${name} normalize failed — golden restored" >&2
    return 1
  fi

  if ! diff -q "$backup_txt" "$golden_txt" >/dev/null; then
    cp "$golden_txt" "$actual_txt"
    cp "$backup_txt" "$golden_txt"
    rm -f "$backup_txt"
    echo "wuphf visual regression: ${name}.txt drifted" >&2
    diff -u "$golden_txt" "$actual_txt" >&2 || true
    echo >&2
    echo "To accept this drift as the new baseline:" >&2
    echo "  mv ${actual_txt} ${golden_txt} && git add ${golden_txt}" >&2
    echo >&2
    echo "In CI, download the 'vhs-drift' artifact to get ${actual_txt}." >&2
    return 1
  fi

  rm -f "$backup_txt" "$actual_txt"
  echo "wuphf visual regression: ${name} OK"
}

for tape in "${TAPES[@]}"; do
  check_one "$tape"
done
