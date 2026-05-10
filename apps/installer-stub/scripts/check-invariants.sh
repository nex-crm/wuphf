#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
package_root="$(cd -- "${script_dir}/.." && pwd)"
repo_root="$(cd -- "${package_root}/../.." && pwd)"
workflow="${repo_root}/.github/workflows/release-rewrite.yml"

if [[ ! -f "${workflow}" ]]; then
  echo "ERR: workflow file not found at ${workflow}; check-invariants must run from a wuphf checkout" >&2
  exit 2
fi

scan_targets=(
  "${package_root}/.gitignore"
  "${package_root}/package.json"
  "${package_root}/electron-builder.yml"
  "${package_root}/src"
  "${package_root}/build"
  "${package_root}/scripts"
  "${workflow}"
)

for scan_target in "${scan_targets[@]}"; do
  if [[ ! -e "${scan_target}" ]]; then
    echo "ERR: invariant scan target not found at ${scan_target}; check-invariants must run from a wuphf checkout" >&2
    exit 2
  fi
done

violations=()
forbidden_patterns=(
  # String splits keep this file from matching itself when ripgrep scans scripts/.
  "--skip-code""sign"
  "--skip-""sign"
  "skip-""sign"
  "skip-""notarize"
  "skip""Notarize"
  "notarize:"" false"
  "hardenedRuntime:"" false"
)

for pattern in "${forbidden_patterns[@]}"; do
  while IFS= read -r match; do
    violations+=("forbidden literal '${pattern}': ${match}")
  done < <(rg -n -F -- "${pattern}" "${scan_targets[@]}" || true)
done

cert_path_regex='(^|[[:space:]"'"'"'=(])([./~]|[A-Za-z]:\\)[^[:space:]"'"'"']+\.(p12|pfx)([[:space:]"'"'"')]|$)'
while IFS= read -r match; do
  violations+=("hardcoded certificate path: ${match}")
done < <(rg -n --pcre2 "${cert_path_regex}" "${scan_targets[@]}" || true)

# electron-builder.yml sets `npmRebuild: false` to avoid the bun npm_execpath
# leak in CI. That's safe while the stub has no NATIVE-MODULE production
# dependencies (electron-builder's npmRebuild step rebuilds native bindings
# under bun, which crashes when bun is the running JS host).
#
# Pure-JS production dependencies are allowed because npmRebuild does not
# touch them. The allowlist is enforced as a closed set: any production dep
# whose name does not appear in `wuphfRuntimeDependenciesAllowlist` (in
# package.json) fails the gate. peerDependencies + optionalDependencies are
# still forbidden outright — they would expand the supply-chain surface
# without electron-builder bundling them deterministically.
#
# Why not allow arbitrary deps? Issue #771 surfaced that
# `electron-updater` was wired into src/main.js but only declared in
# devDependencies, so the packaged app crashed on launch (devDeps are
# pruned out of the asar). The fix moves it to `dependencies` and locks
# the allowlist to that single audited name; widening requires editing
# BOTH package.json's allowlist field AND this invariant in the same
# PR, which is intentional friction.
dependency_check_output="$(
  cd "${repo_root}" &&
    bun -e '
      const pkg = require("./apps/installer-stub/package.json");

      // The single source of truth for which runtime dependencies are
      // approved. Hardcoded HERE — not read from package.json — so a future
      // PR cannot widen the surface by editing only package.json. Adding a
      // name requires editing this script (separate review point) AND the
      // package.json allowlist field (consistency check below).
      //
      // electron-updater rationale: required at runtime by src/main.js for
      // the auto-update flow that the signing pipeline ships end-to-end.
      // Pure JS, no native bindings, so safe under npmRebuild=false.
      const APPROVED_RUNTIME_DEPS = new Set(["electron-updater"]);

      const declaredAllowlist = Array.isArray(pkg.wuphfRuntimeDependenciesAllowlist)
        ? pkg.wuphfRuntimeDependenciesAllowlist
        : [];

      const forbiddenBlocks = ["peerDependencies", "optionalDependencies"];
      let failed = false;

      for (const blockName of forbiddenBlocks) {
        const block = pkg[blockName];
        if (block && typeof block === "object" && Object.keys(block).length > 0) {
          console.error("forbidden dependency block: " + blockName);
          failed = true;
        }
      }

      // Reject any package.json allowlist entry that is not in the
      // hardcoded approved set. This is the second gate the PR comments
      // promised: package.json declares INTENT, this script enforces POLICY.
      for (const declared of declaredAllowlist) {
        if (!APPROVED_RUNTIME_DEPS.has(declared)) {
          console.error(
            "wuphfRuntimeDependenciesAllowlist contains \"" + declared +
              "\" but it is not in APPROVED_RUNTIME_DEPS in check-invariants.sh; " +
              "widening the runtime-dep surface requires editing BOTH files in the same PR",
          );
          failed = true;
        }
      }

      const declaredAllowlistSet = new Set(declaredAllowlist);

      const deps = pkg.dependencies;
      if (deps && typeof deps === "object") {
        for (const name of Object.keys(deps)) {
          if (!APPROVED_RUNTIME_DEPS.has(name)) {
            console.error(
              "dependencies." + name +
                " is not in APPROVED_RUNTIME_DEPS in check-invariants.sh; " +
                "widening the runtime-dep surface requires editing BOTH files in the same PR",
            );
            failed = true;
            continue;
          }
          // Approved by the script-side policy, but the package.json must
          // ALSO declare the intent in its allowlist field. This keeps the
          // public-facing notes/rationale in sync with the deps block —
          // otherwise a contributor approved by the hardcoded set could
          // skip updating the wuphfRuntimeDependenciesAllowlistNotes
          // documentation that downstream readers rely on.
          if (!declaredAllowlistSet.has(name)) {
            console.error(
              "dependencies." + name +
                " is approved by APPROVED_RUNTIME_DEPS but not declared in " +
                "package.json wuphfRuntimeDependenciesAllowlist; add the name to the allowlist",
            );
            failed = true;
          }
        }
      }

      // Prevent stale entries: every approved name that is on the package
      // allowlist must actually be declared in dependencies. A missing
      // declaration means the dep was removed from main.js but the
      // allowlist field was forgotten — surface that immediately so the
      // surface only ever shrinks, never accumulates dead names.
      const declaredDepNames = new Set(
        deps && typeof deps === "object" ? Object.keys(deps) : [],
      );
      for (const allowed of declaredAllowlist) {
        if (!declaredDepNames.has(allowed)) {
          console.error(
            "wuphfRuntimeDependenciesAllowlist contains \"" + allowed +
              "\" but it is not declared in dependencies; remove the stale entry",
          );
          failed = true;
        }
      }

      if (failed) {
        process.exit(1);
      }
    ' 2>&1
)" || {
  while IFS= read -r line; do
    violations+=("${line}")
  done <<< "${dependency_check_output}"
}

while IFS= read -r line; do
  action_ref="$(sed -E 's/^([^:]+:)?[0-9]+:.*uses:[[:space:]]*([^[:space:]#]+).*/\2/' <<<"${line}")"

  if [[ "${action_ref}" == ./* || "${action_ref}" == docker://* ]]; then
    continue
  fi

  if [[ "${action_ref}" != *@* ]]; then
    violations+=("GitHub Action is missing an explicit ref: ${line}")
    continue
  fi

  action_sha="${action_ref##*@}"
  if [[ ! "${action_sha}" =~ ^[0-9a-f]{40}$ ]]; then
    violations+=("GitHub Action is not pinned to a full SHA: ${line}")
  fi
done < <(rg -n 'uses:[[:space:]]*[^[:space:]#]+' "${workflow}" || true)

if [[ "${#violations[@]}" -gt 0 ]]; then
  printf "Installer invariant violations:\n" >&2
  printf -- "- %s\n" "${violations[@]}" >&2
  exit 1
fi

echo "Installer invariants passed."
