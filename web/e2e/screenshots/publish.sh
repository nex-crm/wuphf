#!/usr/bin/env bash
# publish.sh — capture screenshots for a feature, push them to a one-off
# branch, and embed the raw URLs into a PR's description.
#
# Usage:
#   web/e2e/screenshots/publish.sh <feature> <pr-number>
#
# Example:
#   web/e2e/screenshots/publish.sh version-chip 738
#
# Modes:
#   --dry-run       capture only; print the markdown that would be
#                   appended; do NOT push the branch or edit the PR
#   --comment       post the screenshots as a NEW PR comment instead of
#                   replacing the PR body (use this when the PR already
#                   has a hand-written description you don't want to
#                   overwrite)
#
# Defaults: appends to the PR body. Pass --comment to leave the body alone.
#
# What it does:
#   1. Reads `web/e2e/screenshots/<feature>.mjs` — the spec.
#   2. Boots vite dev (if it's not already on $BASE_URL) and waits for
#      it to be ready.
#   3. Runs the spec with WUPHF_SCREENSHOTS_OUT=/tmp/wuphf-screenshots-<feature>.
#   4. Creates an orphan worktree at /tmp/wuphf-screenshots-branch-<pr>,
#      copies the PNGs in, commits, and pushes `screenshots/pr-<n>`.
#   5. Builds the markdown image block referencing
#      raw.githubusercontent.com/<repo>/screenshots/pr-<n>/<file>.png.
#   6. Edits the PR body or posts a comment via `gh`.
#
# Why a separate branch instead of GitHub user-attachments:
#   The drag-drop upload endpoint github.com uses (user-attachments
#   subdomain) is session-only and not exposed via the public REST API.
#   An orphan branch under nex-crm/wuphf is the simplest public-API
#   alternative — pushing PNGs there gives stable raw URLs without
#   needing a release, an external bucket, or a gist.

set -euo pipefail

usage() {
  cat <<'EOF' >&2
usage: publish.sh [--dry-run|--comment] <feature> <pr-number>

  feature      filename in web/e2e/screenshots/<feature>.mjs (without
               extension). E.g. `version-chip` for `version-chip.mjs`.
  pr-number    target PR (numeric). Branch will be `screenshots/pr-<n>`.

flags:
  --dry-run    capture and print markdown; no push, no PR edit
  --comment    post a new PR comment instead of replacing the PR body
EOF
  exit 2
}

mode="body"
[[ "${1-}" == "--dry-run" ]] && { mode="dry"; shift; }
[[ "${1-}" == "--comment" ]] && { mode="comment"; shift; }

feature="${1-}"
pr_number="${2-}"
[[ -z "$feature" || -z "$pr_number" ]] && usage
[[ "$pr_number" =~ ^[0-9]+$ ]] || { echo "pr-number must be numeric" >&2; exit 2; }

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
spec="$repo_root/web/e2e/screenshots/${feature}.mjs"
[[ -f "$spec" ]] || { echo "spec not found: $spec" >&2; exit 2; }

base_url="${BASE_URL:-http://localhost:5273}"
out_dir="/tmp/wuphf-screenshots-${feature}"
mkdir -p "$out_dir"
rm -f "$out_dir"/*.png "$out_dir/README.md"

# 1. Start vite if it isn't already listening on $base_url. Only the
# vite dev server can serve `/src/stores/app.ts` as an ESM module,
# which `flipStore()` depends on. The check uses curl with --silent and
# a bounded timeout so a misconfigured base URL fails fast.
vite_pid=""
cleanup() {
  [[ -n "$vite_pid" ]] && kill "$vite_pid" 2>/dev/null || true
}
trap cleanup EXIT

if ! curl -sf -m 2 "$base_url/" >/dev/null 2>&1; then
  echo "[publish] starting vite dev (existing server not detected at $base_url)" >&2
  vite_log="$(mktemp)"
  bun --cwd "$repo_root/web" run dev > "$vite_log" 2>&1 &
  vite_pid="$!"
  for _ in $(seq 1 30); do
    if curl -sf -m 2 "$base_url/" >/dev/null 2>&1; then
      break
    fi
    sleep 0.5
  done
  if ! curl -sf -m 2 "$base_url/" >/dev/null 2>&1; then
    echo "[publish] vite did not become ready; tail of dev log:" >&2
    tail -40 "$vite_log" >&2
    exit 1
  fi
fi

# 2. Run the spec. node resolves @playwright/test from web/e2e/node_modules
# (installed by `bun install` over there).
echo "[publish] capturing screenshots for $feature → $out_dir" >&2
(
  cd "$repo_root/web/e2e"
  WUPHF_SCREENSHOTS_OUT="$out_dir" BASE_URL="$base_url" \
    node "$spec"
)

shopt -s nullglob
pngs=("$out_dir"/*.png)
shopt -u nullglob
if [[ ${#pngs[@]} -eq 0 ]]; then
  echo "[publish] no PNGs were captured — spec ran without writing files" >&2
  exit 1
fi
echo "[publish] captured ${#pngs[@]} screenshots" >&2

# 3. Build the markdown block. raw.githubusercontent.com works with
# `<owner>/<repo>/<branch>/<path>` so the URLs survive a future repo
# rename only if the branch survives — which is fine for a one-off
# PR-bound screenshots branch.
repo_slug="$(gh repo view --json nameWithOwner -q .nameWithOwner)"
branch="screenshots/pr-${pr_number}"
md_file="$(mktemp)"
{
  echo
  echo "## Screenshots"
  echo
  for png in "${pngs[@]}"; do
    file="$(basename "$png")"
    alt="${file%.png}"
    echo "![${alt}](https://github.com/${repo_slug}/raw/${branch}/${file})"
    echo
  done
} > "$md_file"

require_git_worktree_orphan() {
  local version major minor
  version="$(git version | awk '{print $3}')"
  IFS=. read -r major minor _ <<<"$version"
  if [[ -z "${major:-}" || -z "${minor:-}" || ! "$major" =~ ^[0-9]+$ || ! "$minor" =~ ^[0-9]+$ ]]; then
    echo "[publish] unable to parse git version: ${version:-unknown}" >&2
    exit 1
  fi
  if (( major < 2 || (major == 2 && minor < 42) )); then
    echo "[publish] git worktree add --orphan requires Git >= 2.42; found $version" >&2
    exit 1
  fi
}

strip_existing_screenshots_section() {
  awk '
    /^## Screenshots[[:space:]]*$/ { skipping=1; next }
    skipping && /^## [^#]/ { skipping=0 }
    !skipping { print }
  ' "$1"
}

if [[ "$mode" == "dry" ]]; then
  echo "[publish] dry-run; markdown that would be posted:" >&2
  echo "------" >&2
  cat "$md_file"
  echo "------" >&2
  exit 0
fi

# 4. Push the orphan branch with the PNGs.
worktree_dir="/tmp/wuphf-screenshots-branch-${pr_number}"
if [[ -d "$worktree_dir" ]]; then
  git -C "$repo_root" worktree remove --force "$worktree_dir" 2>/dev/null || rm -rf "$worktree_dir"
fi

# Delete any existing local branch with the same name. `--orphan` rejects
# the worktree create if the ref already exists.
git -C "$repo_root" branch -D "$branch" 2>/dev/null || true

# `git worktree add --orphan` landed in Git 2.42. Fail with a clear message
# instead of falling through to a cryptic "unknown option" on older clients.
require_git_worktree_orphan
git -C "$repo_root" worktree add --orphan -b "$branch" "$worktree_dir"
cp "$out_dir"/*.png "$worktree_dir/"

# Lightweight README so a future maintainer landing on the branch knows
# what they're looking at.
cat > "$worktree_dir/README.md" <<EOF
# Screenshots for PR #${pr_number}

Captured by \`web/e2e/screenshots/publish.sh ${feature} ${pr_number}\` from
\`${feature}.mjs\` against vite dev with mocked broker responses.
Safe to delete after the PR merges.
EOF

git -C "$worktree_dir" add .
git -C "$worktree_dir" commit -m "docs(screenshots): PR #${pr_number} (${feature})" >/dev/null
# Each run rebuilds an orphan history, so a fresh push is non-fast-forward
# against any previous run for the same PR. --force-with-lease lets re-runs
# overwrite the remote screenshots branch without clobbering an unexpected
# update from a third party.
git -C "$worktree_dir" push --force-with-lease -u origin "$branch"

git -C "$repo_root" worktree remove "$worktree_dir"

# 5. Edit the PR body or post a comment.
if [[ "$mode" == "comment" ]]; then
  echo "[publish] posting comment on PR #${pr_number}" >&2
  gh pr comment "$pr_number" --body-file "$md_file"
else
  echo "[publish] updating screenshots in PR #${pr_number} body" >&2
  current_body="$(gh pr view "$pr_number" --json body -q .body)"
  current_body_file="$(mktemp)"
  combined="$(mktemp)"
  printf '%s\n' "$current_body" > "$current_body_file"
  strip_existing_screenshots_section "$current_body_file" > "$combined"
  if [[ -s "$combined" ]]; then
    printf '\n' >> "$combined"
  fi
  cat "$md_file" >> "$combined"
  gh pr edit "$pr_number" --body-file "$combined"
fi

echo "[publish] done — https://github.com/${repo_slug}/pull/${pr_number}" >&2
