#!/usr/bin/env bash
# run-local.sh — drive web/e2e against a sandboxed wuphf without touching
# the developer's real ~/.wuphf state.
#
# Usage:
#   web/e2e/run-local.sh             # both phases (wizard then shell)
#   web/e2e/run-local.sh wizard      # fresh-install path only (no seed)
#   web/e2e/run-local.sh shell       # post-onboarding shell path only (seeded)
#   PORT=27891 web/e2e/run-local.sh  # override the web port (default 27891)
#
# What it sets up that the README explains:
#   - Builds web/dist (vite) and the wuphf binary if missing.
#   - Pins WUPHF_RUNTIME_HOME to a per-run tempdir so onboarded.json /
#     broker-state.json never clobber your real ~/.wuphf.
#   - For the shell phase, seeds <RUNTIME_HOME>/.wuphf/onboarded.json
#     before launching wuphf (the same JSON the CI workflow writes — see
#     .github/workflows/ci.yml :: seed onboarding state).
#   - Launches wuphf on $PORT and $((PORT - 1)) so it doesn't collide
#     with a developer's normally-running 7891 wuphf.

set -euo pipefail

phase="${1:-both}"
case "$phase" in
  both|wizard|shell) ;;
  *)
    echo "usage: $0 [both|wizard|shell]" >&2
    exit 2
    ;;
esac

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

web_port="${PORT:-27891}"
broker_port="$((web_port - 1))"

echo "[run-local] using ports broker=${broker_port} web=${web_port}"

if [ ! -f web/dist/index.html ]; then
  echo "[run-local] building web/dist"
  (cd web && bun install --frozen-lockfile && bun run build)
fi

if [ ! -x ./wuphf ]; then
  echo "[run-local] building wuphf binary"
  go build -o wuphf ./cmd/wuphf
fi

if [ ! -d web/e2e/node_modules ]; then
  echo "[run-local] installing e2e deps"
  (cd web/e2e && bun install --frozen-lockfile >/dev/null)
fi

# Playwright caches chromium in ~/.cache/ms-playwright (Linux) or
# ~/Library/Caches/ms-playwright (macOS). If either has any chromium-*
# directory, skip the install — `bunx playwright install` is itself a
# multi-second no-op when the browser is cached, but we'd rather not pay
# even that on every run. compgen -G returns 0 iff the glob matches
# anything (bash builtin, no subprocess; shellcheck-clean).
if ! compgen -G "$HOME/.cache/ms-playwright/chromium-*" >/dev/null \
   && ! compgen -G "$HOME/Library/Caches/ms-playwright/chromium-*" >/dev/null; then
  echo "[run-local] installing playwright chromium"
  (cd web/e2e && bunx playwright install chromium >/dev/null)
fi

# Sandbox: every wuphf state file lands under this throwaway dir, so a
# developer running this script never has their real onboarded.json or
# broker-state.json mutated.
runtime_home="$(mktemp -d -t wuphf-e2e-runtime-XXXXXX)"
echo "[run-local] sandboxed WUPHF_RUNTIME_HOME=${runtime_home}"

# kill_wuphf sends SIGTERM, polls up to 5s for the process to exit, then
# SIGKILLs if it's still around. Bare `wait` is unbounded — if wuphf hangs
# on shutdown the trap blocks the script forever and leaks $runtime_home.
kill_wuphf() {
  local p="$1"
  [ -n "$p" ] || return 0
  kill -0 "$p" 2>/dev/null || return 0
  kill -TERM "$p" 2>/dev/null || true
  for _ in $(seq 1 50); do
    kill -0 "$p" 2>/dev/null || return 0
    sleep 0.1
  done
  kill -KILL "$p" 2>/dev/null || true
  wait "$p" 2>/dev/null || true
}

cleanup() {
  kill_wuphf "${pid:-}"
  rm -rf "$runtime_home"
}
trap cleanup EXIT

start_wuphf() {
  local label="$1"
  echo "[run-local] starting wuphf (${label})"
  WUPHF_RUNTIME_HOME="$runtime_home" \
    ./wuphf --no-open --broker-port "$broker_port" --web-port "$web_port" --no-nex \
    </dev/null > "/tmp/wuphf-e2e-${label}.log" 2>&1 &
  pid=$!
  for _ in $(seq 1 30); do
    if curl -sf "http://localhost:${web_port}/onboarding/state" -o /dev/null; then
      echo "[run-local] wuphf ready (${label})"
      return 0
    fi
    sleep 1
  done
  echo "[run-local] wuphf failed to become ready (${label})" >&2
  cat "/tmp/wuphf-e2e-${label}.log" >&2 || true
  exit 1
}

stop_wuphf() {
  kill_wuphf "${pid:-}"
  pid=""
  # Wait for the port to free up so the next phase can rebind. The
  # /dev/tcp/<host>/<port> trick is bash-only (not POSIX sh) — relies on
  # the shebang being bash.
  for _ in $(seq 1 10); do
    if ! (exec 3<>/dev/tcp/127.0.0.1/"$web_port") 2>/dev/null; then break; fi
    sleep 1
  done
}

run_wizard_phase() {
  rm -f "${runtime_home}/.wuphf/onboarded.json"
  start_wuphf "wizard"
  echo "[run-local] running wizard.spec.ts"
  (cd web/e2e && WUPHF_E2E_BASE_URL="http://localhost:${web_port}" bunx playwright test tests/wizard.spec.ts)
  stop_wuphf
}

run_shell_phase() {
  mkdir -p "${runtime_home}/.wuphf"
  cat > "${runtime_home}/.wuphf/onboarded.json" <<EOF
{"version":1,"completed_at":"2026-01-01T00:00:00Z","company_name":"e2e-local"}
EOF
  start_wuphf "shell"
  echo "[run-local] running smoke.spec.ts"
  (cd web/e2e && WUPHF_E2E_BASE_URL="http://localhost:${web_port}" bunx playwright test tests/smoke.spec.ts)
  stop_wuphf
}

case "$phase" in
  wizard) run_wizard_phase ;;
  shell)  run_shell_phase ;;
  both)
    run_wizard_phase
    run_shell_phase
    ;;
esac

echo "[run-local] done"
