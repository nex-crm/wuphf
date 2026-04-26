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
  both|wizard|shell|local-llm|local-llm-dialects) ;;
  *)
    echo "usage: $0 [both|wizard|shell|local-llm|local-llm-dialects]" >&2
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

# Always rebuild on local-llm phase — we're iterating on the broker's
# tool-call parsing path and a stale binary is the most painful debug
# loop possible. Other phases reuse a cached binary for speed.
if [ "$phase" = "local-llm" ] || [ ! -x ./wuphf ]; then
  echo "[run-local] building wuphf binary"
  go build -o wuphf ./cmd/wuphf
fi

# Local-LLM phase additionally needs the stub server binary so the
# Playwright tests run against deterministic SSE rather than a real
# mlx_lm.server. Always rebuild — it's tiny and the fixture format
# changes mid-iteration. Stub binary is gitignored under web/e2e/bin/.
if [ "$phase" = "local-llm" ]; then
  mkdir -p web/e2e/bin
  echo "[run-local] building mlx-stub"
  go build -o web/e2e/bin/mlx-stub ./internal/testing/mlx-stub
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
  # Both the broker (`pid`) and the mlx-stub (`stub_pid`) must be
  # tracked at script scope so an abnormal exit (Ctrl-C, set -e
  # bailing on a phase) doesn't leak processes that are still bound
  # to the test ports. Phase functions assign these vars; cleanup
  # tolerates them being unset.
  kill_wuphf "${pid:-}"
  kill_wuphf "${stub_pid:-}"
  rm -rf "$runtime_home"
}
trap cleanup EXIT

start_wuphf() {
  local label="$1"
  local logfile="${runtime_home}/wuphf-${label}.log"
  echo "[run-local] starting wuphf (${label}); log: ${logfile}"
  WUPHF_RUNTIME_HOME="$runtime_home" \
    ./wuphf --no-open --broker-port "$broker_port" --web-port "$web_port" --no-nex \
    </dev/null > "$logfile" 2>&1 &
  pid=$!
  for _ in $(seq 1 30); do
    if curl -sf "http://localhost:${web_port}/onboarding/state" -o /dev/null; then
      echo "[run-local] wuphf ready (${label})"
      return 0
    fi
    sleep 1
  done
  echo "[run-local] wuphf failed to become ready (${label})" >&2
  cat "$logfile" >&2 || true
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
  # Wizard-phase specs: anything that needs the unseeded onboarding flow.
  echo "[run-local] running wizard-phase specs"
  (cd web/e2e && WUPHF_E2E_BASE_URL="http://localhost:${web_port}" bunx playwright test tests/wizard.spec.ts tests/local-llm-onboarding.spec.ts)
  stop_wuphf
}

run_shell_phase() {
  mkdir -p "${runtime_home}/.wuphf"
  cat > "${runtime_home}/.wuphf/onboarded.json" <<EOF
{"version":1,"completed_at":"2026-01-01T00:00:00Z","company_name":"e2e-local"}
EOF
  start_wuphf "shell"
  # Shell-phase specs: post-onboarding UI surfaces (smoke, settings).
  echo "[run-local] running shell-phase specs"
  (cd web/e2e && WUPHF_E2E_BASE_URL="http://localhost:${web_port}" bunx playwright test tests/smoke.spec.ts tests/local-llm-settings.spec.ts)
  stop_wuphf
}

# run_local_llm_phase: deterministic chat-flow tests against a stubbed
# mlx-lm. Boots web/e2e/bin/mlx-stub on a free port, points wuphf at
# it via WUPHF_MLX_LM_BASE_URL, seeds onboarded.json + an
# llm_provider=mlx-lm config so the wizard is skipped. The Playwright
# spec drives a DM and asserts the agent's reply is rendered prose,
# not a raw JSON code block.
run_local_llm_phase() {
  local stub_port="$((web_port + 1000))"
  local stub_log="${runtime_home}/mlx-stub.log"
  # MLX_STUB_FIXTURE lets a Playwright spec swap the response script
  # without touching this driver — useful for the structured-
  # tool-call vs JSON-in-content vs text-only dialect sweep.
  local fixture="${MLX_STUB_FIXTURE:-web/e2e/fixtures/qwen-markdown-tool.txt}"

  echo "[run-local] starting mlx-stub on :${stub_port}; log: ${stub_log}"
  ./web/e2e/bin/mlx-stub --port "$stub_port" --fixture "$fixture" \
    >"$stub_log" 2>&1 &
  # Note: stub_pid is script-global (no `local`) so the EXIT trap's
  # cleanup() can reach it if we abort before the explicit kill below.
  stub_pid=$!

  local ready=0
  for _ in $(seq 1 20); do
    if curl -sf "http://127.0.0.1:${stub_port}/v1/models" -o /dev/null; then
      ready=1
      break
    fi
    sleep 0.2
  done
  if [ "$ready" -ne 1 ]; then
    echo "[run-local] mlx-stub failed to become ready on :${stub_port}" >&2
    cat "$stub_log" >&2 || true
    exit 1
  fi

  mkdir -p "${runtime_home}/.wuphf"
  cat > "${runtime_home}/.wuphf/onboarded.json" <<EOF
{"version":1,"completed_at":"2026-01-01T00:00:00Z","company_name":"e2e-local-llm"}
EOF
  # Seed config so wuphf launches with mlx-lm as the active provider
  # and points at the stub. WUPHF_MLX_LM_BASE_URL also overrides at
  # runtime; we set both so the Settings UI can read either.
  cat > "${runtime_home}/.wuphf/config.json" <<EOF
{
  "llm_provider": "mlx-lm",
  "memory_backend": "markdown",
  "provider_endpoints": {
    "mlx-lm": {
      "base_url": "http://127.0.0.1:${stub_port}/v1",
      "model": "stub-model-v1"
    }
  }
}
EOF

  echo "[run-local] starting wuphf (local-llm)"
  WUPHF_RUNTIME_HOME="$runtime_home" \
    WUPHF_MLX_LM_BASE_URL="http://127.0.0.1:${stub_port}/v1" \
    WUPHF_MLX_LM_MODEL="stub-model-v1" \
    ./wuphf --no-open --broker-port "$broker_port" --web-port "$web_port" --no-nex \
    --provider mlx-lm \
    </dev/null > "${runtime_home}/wuphf-local-llm.log" 2>&1 &
  pid=$!
  local ready=0
  for _ in $(seq 1 30); do
    if curl -sf "http://localhost:${web_port}/onboarding/state" -o /dev/null; then
      echo "[run-local] wuphf ready (local-llm)"
      ready=1
      break
    fi
    sleep 1
  done
  if [ "$ready" -ne 1 ]; then
    echo "[run-local] wuphf failed to become ready (local-llm)" >&2
    cat "${runtime_home}/wuphf-local-llm.log" >&2 || true
    exit 1
  fi

  echo "[run-local] running local-llm chat specs"
  # set +e around playwright so we capture the exit code without
  # tripping the outer `set -e` and losing log capture below.
  set +e
  (cd web/e2e && WUPHF_E2E_BASE_URL="http://localhost:${web_port}" \
     bunx playwright test tests/local-llm-chat.spec.ts)
  local rc=$?
  set -e
  # Persist logs to a stable location so post-run debugging works
  # even though the EXIT trap wipes runtime_home. Iteration on the
  # parser / runner relies on having the most recent broker log.
  rm -rf /tmp/wuphf-local-llm-logs
  mkdir -p /tmp/wuphf-local-llm-logs
  cp -p "${runtime_home}/wuphf-local-llm.log" \
        "${runtime_home}/mlx-stub.log" \
        /tmp/wuphf-local-llm-logs/ 2>/dev/null || true
  cp -rp "${runtime_home}/.wuphf/logs" /tmp/wuphf-local-llm-logs/agent-logs 2>/dev/null || true
  echo "[run-local] logs preserved at /tmp/wuphf-local-llm-logs/"
  kill_wuphf "${pid:-}"
  pid=""
  kill_wuphf "${stub_pid:-}"
  stub_pid=""
  return $rc
}

# run_local_llm_dialects_phase: parser-dialect parity sweep. Restarts
# the stub once per fixture (markdown-fenced JSON / structured
# tool_calls / text-only) so the single dialects spec runs against
# each parser branch end-to-end. The fixture in play is named via
# DIALECT_NAME so the spec body knows which assertions to skip vs
# enforce.
run_local_llm_dialects_phase() {
  local stub_port="$((web_port + 1000))"
  local rc=0
  declare -a fixtures=(
    "markdown:web/e2e/fixtures/qwen-markdown-tool.txt"
    "structured:web/e2e/fixtures/structured-tool-call.txt"
    "text-only:web/e2e/fixtures/text-only-reply.txt"
  )

  mkdir -p "${runtime_home}/.wuphf"
  cat > "${runtime_home}/.wuphf/onboarded.json" <<EOF
{"version":1,"completed_at":"2026-01-01T00:00:00Z","company_name":"e2e-local-llm-dialects"}
EOF
  cat > "${runtime_home}/.wuphf/config.json" <<EOF
{
  "llm_provider": "mlx-lm",
  "memory_backend": "markdown",
  "provider_endpoints": {
    "mlx-lm": {
      "base_url": "http://127.0.0.1:${stub_port}/v1",
      "model": "stub-model-v1"
    }
  }
}
EOF

  for entry in "${fixtures[@]}"; do
    local name="${entry%%:*}"
    local fixture="${entry#*:}"
    local stub_log="${runtime_home}/mlx-stub-${name}.log"
    local wuphf_log="${runtime_home}/wuphf-${name}.log"
    echo "[run-local] dialect-sweep: ${name} (${fixture})"

    ./web/e2e/bin/mlx-stub --port "$stub_port" --fixture "$fixture" \
      >"$stub_log" 2>&1 &
    # Script-global stub_pid so cleanup() can reach it if we exit early.
    stub_pid=$!
    local stub_ready=0
    for _ in $(seq 1 20); do
      if curl -sf "http://127.0.0.1:${stub_port}/v1/models" -o /dev/null; then
        stub_ready=1
        break
      fi
      sleep 0.2
    done
    if [ "$stub_ready" -ne 1 ]; then
      echo "[run-local] dialect-sweep ${name}: mlx-stub failed to become ready" >&2
      cat "$stub_log" >&2 || true
      exit 1
    fi

    WUPHF_RUNTIME_HOME="$runtime_home" \
      WUPHF_MLX_LM_BASE_URL="http://127.0.0.1:${stub_port}/v1" \
      WUPHF_MLX_LM_MODEL="stub-model-v1" \
      ./wuphf --no-open --broker-port "$broker_port" --web-port "$web_port" --no-nex \
      --provider mlx-lm \
      </dev/null > "$wuphf_log" 2>&1 &
    pid=$!
    local wuphf_ready=0
    for _ in $(seq 1 30); do
      if curl -sf "http://localhost:${web_port}/onboarding/state" -o /dev/null; then
        wuphf_ready=1
        break
      fi
      sleep 1
    done
    if [ "$wuphf_ready" -ne 1 ]; then
      echo "[run-local] dialect-sweep ${name}: wuphf failed to become ready" >&2
      cat "$wuphf_log" >&2 || true
      exit 1
    fi

    set +e
    (cd web/e2e && DIALECT_NAME="$name" \
       WUPHF_E2E_BASE_URL="http://localhost:${web_port}" \
       bunx playwright test tests/local-llm-dialects.spec.ts)
    local sub_rc=$?
    set -e
    [ "$sub_rc" -ne 0 ] && rc=$sub_rc

    kill_wuphf "${pid:-}"
    pid=""
    kill_wuphf "${stub_pid:-}"
    stub_pid=""
    # Wait for ports to free.
    for _ in $(seq 1 10); do
      if ! (exec 3<>/dev/tcp/127.0.0.1/"$web_port") 2>/dev/null; then break; fi
      sleep 1
    done
  done

  rm -rf /tmp/wuphf-local-llm-logs
  mkdir -p /tmp/wuphf-local-llm-logs
  cp -p "${runtime_home}"/*.log /tmp/wuphf-local-llm-logs/ 2>/dev/null || true
  echo "[run-local] logs preserved at /tmp/wuphf-local-llm-logs/"
  return $rc
}

case "$phase" in
  wizard) run_wizard_phase ;;
  shell)  run_shell_phase ;;
  local-llm) run_local_llm_phase ;;
  local-llm-dialects) run_local_llm_dialects_phase ;;
  both)
    run_wizard_phase
    run_shell_phase
    ;;
esac

echo "[run-local] done"
