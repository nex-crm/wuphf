# Expert D — Coverage + ratchet + test taxonomy

## 1. Coverage gate

- **Provider**: use Vitest `v8`. This is a Node-only TS package, so native V8
  coverage avoids Istanbul pre-instrumentation. First add the missing provider:

  ```json
  { "devDependencies": { "@vitest/coverage-v8": "2.1.9" } }
  ```

- **Thresholds**: start at the existing enforceable bar: lines `90`,
  statements `90`, functions `90`, branches `85`. Ratchet to `98/98/98/98`
  after measured green runs, in `+2` point steps.
- **How to ratchet**:

  ```ts
  coverage: {
    provider: "v8",
    all: true,
    include: ["src/**/*.ts"],
    exclude: [
      "testdata/**",
      "scripts/**",
      "tests/**",
      "**/*.type-test.ts",
      "**/*.d.ts",
    ],
    reporter: process.env.CI ? ["text", "json-summary"] : ["text"],
    reportsDirectory: "coverage/protocol",
    thresholds: {
      // ratchet: only raise these after a green coverage run.
      lines: 90,
      statements: 90,
      functions: 90,
      branches: 85,
    },
  }
  ```

- **Pre-commit vs CI**: CI only for coverage; pre-push keeps demo, Go verifier,
  and invariant ratchets. Coverage reruns 183 declared tests and should not tax
  every local commit.
- **Expected current coverage**: `bunx vitest run --coverage` currently fails
  because `@vitest/coverage-v8` is absent. Estimated `<98` files:
  `receipt.ts`, `receipt-validator.ts`, `audit-event.ts`, `ipc.ts`,
  `budgets.ts`; likely `>=98`: `brand.ts`, `sha256.ts`, `canonical-json.ts`,
  `event-lsn.ts`, `frozen-args.ts`, `sanitized-string.ts`.

## 2. Test taxonomy

- **Current state**: `tests/*.spec.ts` is flat: 8 spec files plus
  `receipt-literals.type-test.ts`; `rg` finds 183 `it(...)` declarations, with
  parameterized cases matching the prompt's ~205 tests.
- **Proposed split**:
  - `tests/spec/`: `budgets`, `canonical-json`, `event-lsn`, `ipc`, `receipt`.
  - `tests/property/`: `audit-event`, `frozen-args`, `sanitized-string`.
  - `tests/integration/`: reserve for multi-module consumer flows; none today.
- **Markers vs directories**: use directories. Vitest has `describe.skip`,
  `it.concurrent`, sharding, and sequence controls, but no pytest-style marker
  taxonomy worth adding here.
- **Migration cost**:

  ```ts
  test: {
    include: ["tests/{spec,property,integration}/**/*.spec.ts"],
    environment: "node",
  }
  ```

  `scripts/test-protocol.sh` still works for full and focused paths; update its
  examples to `tests/property/frozen-args.spec.ts`. AGENTS.md only needs its
  tree and taxonomy text updated.

## 3. Quality ratchet (baseline pattern)

Apply this to `createHash` call sites. `process.env` and demo submodule imports
should stay hard-zero.

Baseline file, `packages/protocol/scripts/hash-entrypoints-baseline.txt`:

```text
# Existing approved direct hash entry points. Format: file:symbol:api
src/sha256.ts:sha256Hex:createHash
src/audit-event.ts:computeEventHash:createHash
```

Check script, `packages/protocol/scripts/check-hash-entrypoints.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
pkg_root="$(cd "$script_dir/.." && pwd)"
baseline="$pkg_root/scripts/hash-entrypoints-baseline.txt"
current="$(mktemp)"; expected="$(mktemp)"
trap 'rm -f "$current" "$expected"' EXIT

while IFS=: read -r rel line _; do
  symbol=$(awk -v n="$line" '
    NR <= n && /^(export )?function [A-Za-z0-9_]+\(/ {
      s=$0; sub(/^export /,"",s); sub(/^function /,"",s); sub(/\(.*/,"",s); name=s
    } END { print name ? name : "<module>" }
  ' "$pkg_root/$rel")
  printf '%s:%s:createHash\n' "$rel" "$symbol"
done < <(cd "$pkg_root" && grep -rnE --include='*.ts' '\bcreateHash\(' src/) \
  | sort > "$current"

sed -E '/^[[:space:]]*(#|$)/d; s/[[:space:]]+$//' "$baseline" | sort > "$expected"
new=$(comm -23 "$current" "$expected" || true)
stale=$(comm -13 "$current" "$expected" || true)
if [ -n "$new" ] || [ -n "$stale" ]; then
  [ -z "$new" ] || { echo "New direct createHash call sites:" >&2; echo "$new" >&2; }
  [ -z "$stale" ] || { echo "Stale hash baseline entries; remove:" >&2; echo "$stale" >&2; }
  echo "Hashing must stay behind sha256.ts except documented audit-chain entries." >&2
  exit 1
fi
echo "hash entrypoint ratchet passed"
```

## 4. Vitest add-ons

- **Random order**: Vitest v2 has sequence shuffle and seed. Add a reproducible
  CI-only smoke:

  ```json
  { "scripts": { "test:shuffle": "vitest run --sequence.shuffle.files --sequence.shuffle.tests --sequence.seed=743" } }
  ```

- **xdist equivalent**: Vitest workers are already default. Keep config empty:

  ```ts
  test: { poolOptions: {} }
  ```

- **interrogate equivalent**: defer TypeDoc/JSDoc coverage; TS types plus
  README/demo are lower-noise for this small public surface.

  ```json
  { "devDependencies": {} }
  ```

- **hypothesis equivalent**: keep `fast-check`; it is already used in 6 files.

## 5. CI wiring

Protocol job addition:

```yaml
- name: Vitest coverage gate
  working-directory: packages/protocol
  run: bunx vitest run --coverage.enabled --coverage.reporter=text --coverage.reporter=json-summary
```

Pre-push ratchet addition:

```yaml
protocol-hash-entrypoints:
  files: bash scripts/changed-files-since-base.sh
  glob: "packages/protocol/{src/**,scripts/check-hash-entrypoints.sh,scripts/hash-entrypoints-baseline.txt}"
  run: bash packages/protocol/scripts/check-hash-entrypoints.sh
```

Coverage artifact upload: **no**. `text` plus `json-summary` in CI logs is
enough for a small library; add artifacts only if reviewers need HTML diffs.

## 6. Verdict

Add now: coverage provider + CI gate (`1` config change), hash-entrypoint
baseline (`1` script + `1` baseline), directory taxonomy (`8` moves + config).
Defer: TypeDoc/JSDoc coverage, per-file `98` gates, randomized order on every
PR. Start at `90/90/90/85`, then ratchet to `92/92/92/87` after the first
measured green coverage run and continue until `98/98/98/98`.

The intelligence pattern translates well for thresholds and shrink-only
baselines. It does not translate cleanly for pytest markers or docstring
coverage: Vitest has weaker marker semantics, and TS exported types already
carry API structure. Use directories, fast-check, demo smoke, and targeted
custom ratchets instead.
