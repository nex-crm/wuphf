#!/usr/bin/env bash
set -euo pipefail

release_mode="${WUPHF_RELEASE_MODE:-pr}"
signing_scope="${WUPHF_SIGNING_SCOPE:-all}"

case "${release_mode}" in
  pr | production) ;;
  *)
    echo "WUPHF_RELEASE_MODE must be 'pr' or 'production', got '${release_mode}'" >&2
    exit 1
    ;;
esac

case "${signing_scope}" in
  all | mac | win) ;;
  *)
    echo "WUPHF_SIGNING_SCOPE must be 'all', 'mac', or 'win', got '${signing_scope}'" >&2
    exit 1
    ;;
esac

mac_required=(
  "APPLE_ID"
  "APPLE_TEAM_ID"
  "APPLE_APP_SPECIFIC_PASSWORD"
  "APPLE_CERT_P12_BASE64"
  "APPLE_CERT_PASSWORD"
)

win_required=(
  "AZURE_TENANT_ID"
  "AZURE_CLIENT_ID"
  "AZURE_CLIENT_SECRET"
  "AZURE_SIGNING_ACCOUNT_NAME"
  "AZURE_CERT_PROFILE_NAME"
  "AZURE_ENDPOINT"
  "AZURE_EXPECTED_PUBLISHER_NAME"
)

missing=()

print_status() {
  local name="$1"
  local value="${!name:-}"

  if [[ -n "${value}" ]]; then
    printf "✓ %s set\n" "${name}"
    return
  fi

  missing+=("${name}")
  if [[ "${release_mode}" == "production" ]]; then
    printf "✗ %s missing - required for production release\n" "${name}"
  else
    printf "✗ %s missing - will build unsigned\n" "${name}"
  fi
}

base64_decode_ok() {
  local value="$1"

  if command -v base64 >/dev/null 2>&1; then
    if printf "%s" "${value}" | base64 --decode >/dev/null 2>&1; then
      return 0
    fi

    if printf "%s" "${value}" | base64 -D >/dev/null 2>&1; then
      return 0
    fi
  fi

  if command -v python3 >/dev/null 2>&1; then
    VALUE="${value}" python3 - <<'PY'
import base64
import os

base64.b64decode(os.environ["VALUE"], validate=True)
PY
    return $?
  fi

  return 1
}

echo "WUPHF release mode: ${release_mode}"
echo "WUPHF signing scope: ${signing_scope}"
echo
if [[ "${signing_scope}" == "all" || "${signing_scope}" == "mac" ]]; then
  echo "macOS signing/notarization:"
  for name in "${mac_required[@]}"; do
    print_status "${name}"
  done

  if [[ -n "${APPLE_CERT_P12_BASE64:-}" ]] && ! base64_decode_ok "${APPLE_CERT_P12_BASE64}"; then
    echo "APPLE_CERT_P12_BASE64 is not valid base64; check for trailing whitespace" >&2
    exit 1
  fi
fi

echo
if [[ "${signing_scope}" == "all" || "${signing_scope}" == "win" ]]; then
  echo "Windows Azure Trusted Signing:"
  for name in "${win_required[@]}"; do
    print_status "${name}"
  done
fi

if [[ "${release_mode}" == "production" && "${#missing[@]}" -gt 0 ]]; then
  echo
  printf "Missing production signing secret(s): %s\n" "${missing[*]}" >&2
  exit 1
fi

echo
echo "Signing secret check passed for ${release_mode} mode."
