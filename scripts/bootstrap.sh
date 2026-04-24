#!/usr/bin/env bash
# bootstrap.sh — one-shot setup for a fresh wuphf checkout.
#
# Idempotent: safe to re-run. Installs Bun dependencies at the repo root
# (secretlint, commitlint rules) and in web/ (frontend deps), registers
# lefthook git hooks, and prints install hints for optional tools.
#
# Required on PATH: bun, go, git, lefthook.
# Optional (warn if missing): vhs, golangci-lint.

set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

# ---- color helpers (cheap, no tput dep) ----
if [ -t 1 ]; then
  bold=$'\033[1m'; red=$'\033[31m'; yellow=$'\033[33m'
  green=$'\033[32m'; reset=$'\033[0m'
else
  bold=""; red=""; yellow=""; green=""; reset=""
fi

info()  { printf "%s==>%s %s\n"    "$bold"   "$reset" "$*"; }
warn()  { printf "%swarn:%s %s\n"  "$yellow" "$reset" "$*" >&2; }
fail()  { printf "%serror:%s %s\n" "$red"    "$reset" "$*" >&2; exit 1; }
ok()    { printf "%sok:%s %s\n"    "$green"  "$reset" "$*"; }

# ---- required tools ----
for tool in bun go git lefthook; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    case "$tool" in
      bun)      hint="curl -fsSL https://bun.sh/install | bash" ;;
      go)       hint="https://go.dev/doc/install (or: brew install go)" ;;
      git)      hint="brew install git  # or your OS package manager" ;;
      lefthook) hint="brew install lefthook  # or: go install github.com/evilmartians/lefthook@latest" ;;
    esac
    fail "$tool not found on PATH. Install hint: $hint"
  fi
done

# ---- bun install at repo root (secretlint, commitlint) ----
info "bun install (repo root)"
bun install

# ---- bun install in web/ (frontend deps) ----
info "bun install (web/)"
(cd web && bun install)

# ---- lefthook: install git hooks into .git/hooks ----
info "lefthook install"
lefthook install

# ---- optional tools: warn + hint, don't fail ----
if ! command -v vhs >/dev/null 2>&1; then
  warn "vhs not installed — pre-push visual regression will be skipped."
  warn "  install: brew install vhs  (or: go install github.com/charmbracelet/vhs@latest)"
else
  ok "vhs present"
fi

if ! command -v golangci-lint >/dev/null 2>&1; then
  warn "golangci-lint not installed — pre-commit Go lint will fail."
  warn "  install: brew install golangci-lint  (or: see https://golangci-lint.run/usage/install/)"
else
  ok "golangci-lint present"
fi

printf "\n%sYou're ready%s — try \`git commit\`.\n" "$bold" "$reset"
