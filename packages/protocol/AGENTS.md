# @wuphf/protocol — Agent Guidelines

This package is the moat. Everything here is hand-rolled (no schema lib, no
crypto-library wrappers) so the wire-shape and security invariants stay
auditable end-to-end. Any agent — Claude, Codex, Cursor, human reviewer —
working in this directory MUST read this file before making changes.

Start with `docs/OVERVIEW.md` for the package map, cross-module invariants,
wire surfaces, and rolled-up Phase 3 punch list.

If a rule below clashes with what your prompt asked you to do, stop and
surface the conflict. The user will tell you which side wins; don't silently
choose.

---

## Runtime target

This package targets **Node.js (and Electron's main process)**. The tsconfig
declares `"types": ["node"]`; the audit chain uses `node:crypto.createHash` and
`Buffer` for byte/base64 encoding. The protocol shapes themselves are
runtime-agnostic, but the verifier helpers (`computeEventHash`,
`serializeAuditEventRecordForHash`) will not run in a browser or Electron
renderer with `nodeIntegration: false`. If renderer-side verification is ever
needed, port `sha256.ts` and the base64 encoder in `audit-event.ts` to
`crypto.subtle` + `btoa` together — they're a matched pair.

## What this package does

- Branded TypeScript types for the wire surface: `ReceiptId`, `ProviderKind`,
  `BrokerPort`, etc.
- The **two moat primitives** — `FrozenArgs` (canonical-JSON + content-hash)
  and `SanitizedString` (NFKC + Unicode-bypass + prototype-pollution-safe
  recursive walk).
- The **receipt schema + codec + validator** with cross-field invariants.
- The **audit chain types** (hash-chained `AuditEventRecord` with golden
  vectors locking the cross-language wire contract).
- IPC envelopes (broker ↔ cli ↔ renderer) including the DNS-rebinding
  loopback guard.

This package contains **no I/O**. No filesystem, no network, no SQLite, no
keychain calls. Anything that touches the OS lives in another package and
imports types from here. If your change requires I/O, it belongs elsewhere.

---

## Hard constraints (do not violate)

### 1. Strict-unknown rejection at every object boundary

Every codec function that builds a typed object from JSON MUST call
`assertKnownKeys(record, path, KEYS_SET)`. Every per-type `KEYS_SET` MUST be
defined as `as const satisfies readonly (keyof T)[]` so a typo fails
typecheck.

The one place this leaked historically was `FrozenArgsFromJson` — it read
two fields and silently dropped any sibling. Bug, not a pattern.

### 2. Wire shape changes need golden vectors

`packages/protocol/tests/audit-event.spec.ts` pins both the canonical
serialization bytes AND the resulting `eventHash`. If you change:

- `serializeAuditEventRecordForHash`,
- `computeEventHash`,
- the prevHash mixing (currently 64-byte ASCII lower-hex, NOT raw bytes),
- `GENESIS_PREV_HASH`,
- the JCS canonicalization in `canonical-json.ts`,
- the EventLsn wire format,

you have changed the wire contract. Cross-language verifiers (Go / Rust /
Python) WILL break. Either:

(a) Update the golden vectors in the same commit and write a clear migration
    note in the commit body explaining how implementers should bump, or
(b) Don't make the change.

### 3. The README must match the code

The README states the on-wire hash formula. If you touch the hash chain, fix
the README in the same commit. Reviewer R2/R6/R7 caught this drifting once;
don't let it happen again.

### 4. Validators re-derive, don't trust `instanceof`

`validateFrozenArgs` and `validateSanitizedString` MUST recompute the
canonical JSON / sanitized projection from input fields and compare bytes.
The `instanceof` check alone is forgeable via `Object.create(...prototype)`.

### 5. SignedApprovalToken cross-field binding

When validating an approval token inside a receipt, the validator MUST:

- Confirm `token.receiptId === enclosing receipt.id`
- Confirm `token.frozenArgsHash === enclosing proposedDiff.hash`

`validateExternalWrite` enforces both bindings because external writes carry
the `proposedDiff`. `validateApprovalEvent` enforces only the receiptId binding
because an approval event does not carry the diff needed to re-check
`frozenArgsHash`. If you add a new sibling-with-`FrozenArgs` place a token can
appear, add the hash binding there too.

### 6. ProviderKind is a closed enum

`ProviderKind = Brand<(typeof PROVIDER_KIND_VALUES)[number], "ProviderKind">`.
Adding a value extends the type AND forces every `switch (providerKind)` to
cover it. Don't widen the brand to `Brand<string, ...>`; that loses
exhaustiveness.

### 7. EventLsn safe-integer bound is intentional

`lsnFromV1Number` requires `Number.isSafeInteger`. `parseLsn` rejects
unsafe integers. The two MUST stay in sync — otherwise the appender mints
tokens the verifier rejects. If you ever need values > MAX_SAFE_INTEGER,
that's a v2 wire format with bigint storage, not a relaxed guard.

### 8. ExternalWrite is a discriminated union

Each `result` value has its own variant with the precise nullability for
that state. The validator and codec mirror the per-state field
requirements. If you add a new `WriteResult` value, add the variant, the
validator branch, and the codec branch in the same commit.

### 9. No `any`, no biome ignores, no `// @ts-ignore`

Use `unknown` with narrowing. Use `Record<string, unknown>` for parsed
JSON. Use `as` only when narrowing through a runtime check that just
proved the type. Lint suppressions are not a fix — fix the code.

### 10. Sustainability discipline (bounded operations)

Protocol work is not complete until the operational shape is bounded. New
verifiers, wire readers, chain walkers, or recovery surfaces MUST account for
bounded budgets, streaming verification where full materialization would be
unsafe, and recovery primitives for interrupted or corrupt chains. Don't ship
an unbounded all-in-memory path and call it protocol-grade.

Specifically:

- **Bounded resource budgets are NOT optional.** Anything that can grow with
  input size has a named budget in `budgets.ts` and a validator that fails
  fast before deeper validation or canonicalization.
- **Streaming/incremental APIs** for any verifier or codec that processes a
  sequence. `verifyChain` has `verifyChainIncremental`; add the same pattern
  when introducing similar surfaces.
- **Cleanup primitives**: when adding a new resource type that needs eventual
  cleanup (persistent token, expiring claim, temporary file), define the
  lifecycle at the protocol layer — TTL constant, expiry timestamp, validator
  that checks the lifetime.
- **No-runaway guards are part of the wire contract.** Receipt cost fields
  are mirror-only (broker-enforced); retryable `WriteFailureMetadata` carries
  a per-failure retry hint, but total attempt counting and retry budgets are
  broker-side. Any new cost-bearing surface needs the same.
- **Resource budget changes are wire-contract changes.** Coordinate the bump
  with downstream consumers and update the README in the same PR.

### 11. Public API only changes through index.ts

Anything a consumer can import from `@wuphf/protocol` is in
`packages/protocol/src/index.ts`. Add new exports there explicitly; don't
rely on transitive re-exports from sub-modules. The visible public surface
is what we promise to keep stable.

### 12. Runtime TS surface is camelCase, lint-enforced

Every TS interface property, type member, and function name in this package
MUST be camelCase. The Biome rule `style/useNamingConvention` enforces this
at every save, every commit, and every CI run — so a snake_case interface
field fails lint and never reaches review. The rule is configured with
`strictCase: false` (allows PascalCase generic parameters like
`BrokerHttpRequest<TBody>`) and an `objectLiteralProperty` exemption (so
wire-format string-keyed constants like `PAYLOAD_KIND_METADATA` keep working
because their keys are typed against the audit-kind enum).

When the wire format is snake_case (e.g. `{ token, broker_url }` from v0
`/api-token`), keep the TS interface camelCase and add a codec at the
boundary that translates: `apiBootstrapFromJson` / `apiBootstrapToJson` are
the canonical example. The codec is the ONLY place the two shapes meet —
never read snake_case keys off a runtime value or hand-roll the
translation in callers. Same pattern receipt codecs use.

### 13. Date APIs are for marking time only

`Date`, `new Date(...)`, `.getTime()`, `.toISOString()`, and the rest of
the wall-clock surface MUST NOT provide uniqueness, ordering, deduplication,
hash-key uniqueness, or monotonic-counter behavior. Millisecond precision
collides under rapid events: two events fired in the same ms get the same
timestamp, and any invariant built on top silently breaks.

Use `EventLsn` for audit-chain ordering, ULID brands for IDs (the random
suffix tolerates same-ms collisions), and explicit sequence/counter fields
for monotonic state. Date values may record when something happened,
serialize that mark to the wire, or enforce a per-record validity window
(e.g. `issuedAt < expiresAt` for an approval token). They must NEVER decide
cross-record order.

`Date.now()`, `Date.parse()`, and `performance.now()` have no legitimate
marking-time use in this package and are forbidden outright by
`scripts/check-invariants.sh`. The check fires on commit and CI. If you
think you need `Date.now()` for something, it's almost certainly an
ordering/uniqueness use — surface it before adding it.

### 14. Coverage is a ratchet, never a floor

`vitest.config.ts` declares numeric coverage thresholds. Every PR MUST
keep coverage at or above the current thresholds. NEVER lower the
thresholds to make a PR green — write tests instead, or surface the
regression to the reviewer.

The aspirational target is 98 lines / 98 statements / 98 functions / 98
branches. When measured coverage stably exceeds the gate by ≥1 point,
the next docs commit raises the gate (one-way ratchet). The gate runs
in CI; not pre-commit, because vitest --coverage adds substantial
overhead and local iteration must stay fast.

If you genuinely cannot test a branch (e.g. defensive code that cannot
fire by construction), use a `/* c8 ignore next */` annotation with a
one-line comment explaining why — but the bar is high. Untested defensive
code is usually broken defensive code.

### 15. Sub-agent dispatch contract

When Claude, Codex, or a human delegates package work to a sub-agent
(`codex exec`, Claude `Agent`, etc.), the prompt MUST carry the same rules
the delegator is following. There is no looser path because the work was
delegated. The prompt MUST:

1. Reference `packages/protocol/AGENTS.md` by path.
2. Quote the relevant hard rules verbatim; sub-agents don't always read links.
3. Specify the exact verification commands to run before commit.
4. Specify per-finding disposition format:
   `FIXED` / `SKIPPED` + reason / `DEFERRED` + issue.
5. Specify scope boundary: which files to touch, which to leave alone,
   especially for parallel batches.
6. Specify failure-mode behavior: if you can't safely fix something, leave a
   TODO with rationale rather than commit a half-fix.
7. Pick a profile sandbox that matches the task: `-p auto -s workspace-write`
   for editing tasks; `-p auto -s read-only` for review-only work.

---

## Working rules for AI agents

### Before editing

1. Read this file. Read the file you're about to edit. Read the test file
   covering it.
2. Run `bun run typecheck && bun run check && bun run test` from
   `packages/protocol/` to confirm the baseline is green. If anything is
   already broken, surface that — don't fix unrelated things by stealth.
3. Identify which hard constraints (above) your change interacts with.

### Demo-driven iteration

For any non-trivial public API change (new export, validator, brand, codec
function, or wire-shape field):

1. Run the demo first: `bun run packages/protocol/scripts/demo.ts`. ~100ms.
   Establishes a "before" baseline of what passes.
2. Make your changes.
3. Run the demo again; it catches `index.ts` drift that per-module tests miss.
   To catch that drift, the demo MUST import through `src/index.ts`, not
   `src/<submodule>.ts` paths.
4. If you add public API surface, extend `scripts/demo.ts` with a scenario:
   copy an existing block, replace inputs/expectations, and watch the new PASS
   line print. The demo is a living artifact reviewers read before the diff
   (per `feedback_atomic_demo_slices.md`).
5. If you touch the audit chain or canonical JSON, run
   `cd packages/protocol/testdata && go run verifier-reference.go`. ~2s on
   warm cache. If you changed the wire shape, extend
   `audit-event-vectors.json` AND the Go verifier in the same commit —
   otherwise cross-language drift is undetectable until a Go/Rust/Python
   implementer writes their own and complains.
6. If the change is not reflected in `scripts/demo.ts`, the commit body must
   explain why the demo did not need an extension.

### While editing

1. Prefer surgical edits. The receipt module was just split out from a
   1500-LOC file; don't bloat it back. The file-size budget is enforced.
2. Add a test for every behavior change. Property tests
   (`fast-check`) for invariants; spec tests for fixed cases.
3. If you add a new type, add the `as*` constructor + `is*` guard +
   `KEYS_TUPLE` (if it's a wire-shape interface) + index.ts export.
4. If you touch a hash, signature, or canonical serialization: assume
   you're changing the wire contract until you've proven otherwise with
   the golden vector.

### After editing

1. Run `bun run typecheck && bun run check && bun run test`.
2. If golden vectors fail, decide: did you intend a wire-contract change
   (then update vectors + README) or is this a bug in your edit?
3. Verify the file-size budget hasn't been busted: the pre-push hook
   runs `scripts/check-file-size.sh`.
4. Commit with a Conventional Commit message that explains the WHY,
   not the what.

### When you delegate

When delegating to a sub-agent (Codex, Claude `Agent`, etc.):

1. Plan the file-overlap matrix first; if multiple agents will touch the same
   file, sequence them or scope each to a different concern.
2. Use git worktrees for parallel agents:
   `git worktree add /path -b branch base-ref`.
3. Make each agent prompt self-contained with the hard rules, verification
   commands, and disposition format; sub-agents do not share your history.
4. Cherry-pick in dependency order: least overlap first, then resolve
   integration conflicts.
5. Don't ask agents to re-do conflicting work; resolve manually after their
   isolated batch is complete.
6. Clean up worktrees after integration:
   `git worktree remove /path && git branch -D branch-name`.

### Things that are NOT allowed without explicit human approval

- Adding a runtime dependency to `package.json`. Each new dep is a supply
  chain target. The current dep list is `canonicalize` (RFC 8785) and
  nothing else.
- Changing the JCS canonicalization (e.g. switching to a different lib).
  The wire vectors depend on the exact byte output.
- Removing or weakening a `assertKnownKeys` call.
- Removing or weakening a cross-field invariant in the validator.
- Marking the package as having I/O capabilities.
- Bumping the schema version (currently 1) without a migration plan in
  the same PR.

---

## Where things live

```text
packages/protocol/
├── AGENTS.md                          (this file — read first)
├── README.md                          (consumer-facing)
├── package.json
├── tsconfig.json
├── biome.json
├── vitest.config.ts
└── src/
    ├── index.ts                       (public API surface)
    ├── brand.ts                       (Brand<T, "Tag"> primitive)
    ├── sha256.ts                      (Sha256Hex brand + sha256Hex helper)
    ├── canonical-json.ts              (RFC 8785 JCS + assertJcsValue)
    ├── frozen-args.ts                 (canonical-JSON + content-hash class)
    ├── sanitized-string.ts            (NFKC + Unicode bypass + safe walk)
    ├── event-lsn.ts                   (opaque "v1:<n>" LSN tokens)
    ├── audit-event.ts                 (hash-chained record types + verifier)
    ├── ipc.ts                         (broker IPC envelopes + loopback guard)
    ├── receipt-types.ts               (receipt interface + brand constructors)
    ├── receipt-utils.ts               (shared codec/validator helpers)
    ├── receipt-validator.ts           (validateReceipt + per-type KEYS sets)
    └── receipt.ts                     (codec: receiptToJson / receiptFromJson)
└── tests/
    ├── ipc.spec.ts                    (loopback guard + IPC brand constructors)
    ├── frozen-args.spec.ts            (FrozenArgs property tests)
    ├── sanitized-string.spec.ts       (NFKC corpus + prototype pollution)
    ├── event-lsn.spec.ts              (LSN format + safe-integer guards)
    ├── audit-event.spec.ts            (chain verification + golden vectors)
    └── receipt.spec.ts                (codec + validator + cross-field binding)
```

If you're adding a new file, put it in the directory whose pattern matches
its responsibility. Don't add new top-level dirs without surfacing why.
