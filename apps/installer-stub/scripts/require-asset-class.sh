# shellcheck shell=bash
# Sourced helper for the rewrite release workflow.
#
# Used by `.github/workflows/release-rewrite.yml` to assert that a glob
# expansion produced at least one matching artifact path. With
# `shopt -s nullglob` set in the caller, an empty match expands to zero
# arguments — so the function fails the build instead of silently uploading
# an incomplete release.
#
# Usage:
#   shopt -s nullglob
#   source apps/installer-stub/scripts/require-asset-class.sh
#   mac_dmgs=(release-assets/*.dmg)
#   require_asset_class "dmg" "${mac_dmgs[@]}"

require_asset_class() {
  local class="$1"
  shift

  if [[ "$#" -eq 0 ]]; then
    echo "Missing required release asset class: ${class}" >&2
    exit 1
  fi
}
