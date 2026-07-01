#!/usr/bin/env bash
# dev-operator.sh — run THIS worktree's operator stack on DEDICATED ports so it
# never collides with another worktree's broker on the default 7890/7891.
#
#   broker API : 7892   (--broker-port)
#   web UI     : 7893   (--web-port)   <- the vite dev proxy forwards /api here
#   vite (FE)  : 5280
#
# The broker reads its OpenAI Realtime key + model from the runtime home below
# (paste the key in the operator Settings → Voice; it persists there).
#
# Usage:
#   bash scripts/dev-operator.sh            # build broker, start broker + vite
#   bash scripts/dev-operator.sh --no-build # skip the go build
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

BROKER_PORT="${WUPHF_BROKER_PORT:-7892}"
WEB_PORT="${WUPHF_WEB_PORT:-7893}"
VITE_PORT="${WUPHF_VITE_PORT:-5280}"
HOME_DIR="${WUPHF_RUNTIME_HOME:-/private/tmp/wuphf-operator-home}"
COMPOSIO_USER_ID="${WUPHF_COMPOSIO_USER_ID:?set WUPHF_COMPOSIO_USER_ID to your Composio user id}"
LOG_DIR="${TMPDIR:-/tmp}"

free_port() {
  for p in $(lsof -nP -tiTCP:"$1" -sTCP:LISTEN 2>/dev/null | sort -u); do
    echo "  freeing port $1 (pid $p)"
    kill "$p" 2>/dev/null || true
  done
}

echo "==> stopping anything on our ports ($BROKER_PORT/$WEB_PORT/$VITE_PORT)"
free_port "$BROKER_PORT"
free_port "$WEB_PORT"
free_port "$VITE_PORT"
sleep 1

if [[ "${1:-}" != "--no-build" ]]; then
  echo "==> building broker"
  go build -o wuphf-mvp ./cmd/wuphf
fi

echo "==> starting broker on broker:$BROKER_PORT web:$WEB_PORT (home: $HOME_DIR)"
# Never inherit a stray OpenAI/Composio key from the shell — the broker resolves
# them from the runtime home config (Settings).
env -u WUPHF_COMPOSIO_API_KEY -u OPENAI_API_KEY -u WUPHF_OPENAI_API_KEY \
  WUPHF_RUNTIME_HOME="$HOME_DIR" \
  WUPHF_COMPOSIO_USER_ID="$COMPOSIO_USER_ID" \
  ./wuphf-mvp --no-open --broker-port "$BROKER_PORT" --web-port "$WEB_PORT" \
  >"$LOG_DIR/wuphf-operator-broker.log" 2>&1 &
echo "  broker pid $! (log: $LOG_DIR/wuphf-operator-broker.log)"

echo "==> starting vite on $VITE_PORT (proxying /api -> 127.0.0.1:$WEB_PORT)"
(cd web && WUPHF_WEB_PROXY_PORT="$WEB_PORT" bunx vite --port "$VITE_PORT" --strictPort \
  >"$LOG_DIR/wuphf-operator-vite.log" 2>&1 &)
echo "  vite log: $LOG_DIR/wuphf-operator-vite.log"

sleep 5
echo "==> health check"
printf '  broker  /realtime/session : '
curl -s -o /dev/null -w "%{http_code}\n" -X POST "http://127.0.0.1:$BROKER_PORT/realtime/session"
printf '  proxy   /api/realtime/...  : '
curl -s -o /dev/null -w "%{http_code}\n" -X POST "http://127.0.0.1:$WEB_PORT/api/realtime/session"
echo "==> operator UI: http://localhost:$VITE_PORT/#/operator"
