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

echo "[run-local] installing e2e deps"
(cd web/e2e && bun install --frozen-lockfile >/dev/null)

echo "[run-local] ensuring playwright chromium is installed"
(cd web/e2e && bunx playwright install chromium >/dev/null)

# Sandbox: every wuphf state file lands under this throwaway dir, so a
# developer running this script never has their real onboarded.json or
# broker-state.json mutated.
runtime_home="$(mktemp -d -t wuphf-e2e-runtime-XXXXXX)"
echo "[run-local] sandboxed WUPHF_RUNTIME_HOME=${runtime_home}"

cleanup() {
  if [ -n "${pid:-}" ] && kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
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
  if [ -n "${pid:-}" ] && kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    pid=""
  fi
  # Wait for the port to free up so the next phase can rebind.
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
