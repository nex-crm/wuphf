#!/usr/bin/env bash
set -euo pipefail

release_mode="${WUPHF_RELEASE_MODE:-pr}"

case "${release_mode}" in
  pr | production) ;;
  *)
    echo "WUPHF_RELEASE_MODE must be 'pr' or 'production', got '${release_mode}'" >&2
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

echo "WUPHF release mode: ${release_mode}"
echo
echo "macOS signing/notarization:"
for name in "${mac_required[@]}"; do
  print_status "${name}"
done

echo
echo "Windows Azure Trusted Signing:"
for name in "${win_required[@]}"; do
  print_status "${name}"
done

if [[ "${release_mode}" == "production" && "${#missing[@]}" -gt 0 ]]; then
  echo
  printf "Missing production signing secret(s): %s\n" "${missing[*]}" >&2
  exit 1
fi

echo
echo "Signing secret check passed for ${release_mode} mode."
