#!/usr/bin/env bash
# Verify the desktop main+preload build doesn't re-introduce workspace
# packages as runtime externals.
#
# Why this check exists:
#
# `electron-vite`'s default `externalizeDepsPlugin()` marks every entry
# in `package.json#dependencies` as a runtime external. For ordinary npm
# packages with compiled `.js` entry points that's correct. For our
# workspace packages (`@wuphf/broker`, `@wuphf/protocol`) it is
# **catastrophic**: their `package.json#exports` map to raw `./src/*.ts`,
# and Node 22's strip-only TypeScript support can't transpile TS-only
# syntax constructs like parameter properties (used in
# `SqliteReceiptStore`'s private constructor). At packaged runtime the
# Electron utility process would crash on the first `import` of a
# workspace package — before the parent-port handshake — and no JS-side
# test catches this because Vitest evaluates TS through Vite's
# transformer.
#
# This check inspects the post-build artifacts and fails CI if:
#   - any `out/{main,preload}/**.js` imports a `@wuphf/*` workspace
#     package by name (means the workspace dep wasn't bundled), OR
#   - any output file imports a raw `*.ts` path (means an external's
#     `exports` field still points to source).
#
# It also asserts that `better-sqlite3` IS still an external — its
# native-binding loader (`bindings()`) walks up the filesystem to find
# its `.node` sibling, which requires the JS wrapper to live at its
# npm location rather than getting flattened into our bundle.

set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out_dir="${root_dir}/out"

if [ ! -d "${out_dir}/main" ]; then
  echo "FAIL: ${out_dir}/main does not exist. Run \`bun run build\` first." >&2
  exit 1
fi

fail=0
report() {
  echo "FAIL: $1" >&2
  fail=1
}

# Collect every emitted .js under main + preload (chunks live under
# out/main/chunks/). Skip renderer — it's a browser bundle and its
# externalization rules are different. `mapfile` is bash 4+; use a
# portable read loop so this works on macOS's stock bash 3.2.
output_files=()
while IFS= read -r f; do
  output_files+=("$f")
done < <(
  find "${out_dir}/main" "${out_dir}/preload" \
    -type f -name '*.js' 2>/dev/null | sort
)

if [ "${#output_files[@]}" -eq 0 ]; then
  echo "FAIL: no JS output found under ${out_dir}/{main,preload}." >&2
  exit 1
fi

# --- Forbidden: workspace packages must be bundled, not externalized ---
# Both static (`from "@wuphf/..."`) and dynamic (`import("@wuphf/...")`)
# forms are checked. After a correct build, vite rewrites dynamic
# imports of bundled packages to internal chunk paths like
# `./chunks/sqlite-receipt-store-XYZ.js`.
workspace_static_hits="$(grep -nE 'from "@wuphf/[^"]+"' "${output_files[@]}" 2>/dev/null || true)"
if [ -n "${workspace_static_hits}" ]; then
  report "workspace package imports leaked into build (workspace deps must be inlined):"
  echo "${workspace_static_hits}" >&2
fi

workspace_dynamic_hits="$(grep -nE 'import\("@wuphf/[^"]+"\)' "${output_files[@]}" 2>/dev/null || true)"
if [ -n "${workspace_dynamic_hits}" ]; then
  report "workspace package dynamic imports leaked into build:"
  echo "${workspace_dynamic_hits}" >&2
fi

# --- Forbidden: raw .ts imports at runtime ---
# If a workspace `package.json#exports` field still points at `./src/*.ts`
# and the package is externalized, the build output ends up with
# `from "..../foo.ts"` literally in a .js file. Node 22 strip-only TS
# rejects parameter properties, so this would crash at runtime.
raw_ts_hits="$(grep -nE 'from "[^"]+\.ts"' "${output_files[@]}" 2>/dev/null || true)"
if [ -n "${raw_ts_hits}" ]; then
  report "raw .ts imports in build output (the runtime would attempt to evaluate TypeScript):"
  echo "${raw_ts_hits}" >&2
fi

# --- Required: better-sqlite3 stays external ---
# Its `bindings()` loader resolves `.node` via the npm filesystem layout.
# If the JS wrapper got bundled, the native binding fails to load at
# runtime even though the build succeeds.
if ! grep -rqE 'from "better-sqlite3"' "${out_dir}/main"; then
  report "better-sqlite3 import missing from out/main/ — the wrapper got inlined, and bindings() will fail to locate the .node sibling at runtime."
fi

if [ "${fail}" -ne 0 ]; then
  echo >&2
  echo "Build-output externalization check failed. See" >&2
  echo "  apps/desktop/electron.vite.config.ts — externalizeDepsPlugin({ exclude: ... })" >&2
  echo "  packages/broker/docs/event-log-projections-design.md § 'Package surface'" >&2
  exit 1
fi

echo "PASS: build-output externalization invariants intact (${#output_files[@]} files inspected)."
