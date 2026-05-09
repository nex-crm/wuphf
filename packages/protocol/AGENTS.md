# @wuphf/protocol — Agent Guidelines

This package is the moat. Everything here is hand-rolled (no schema lib, no
crypto-library wrappers) so the wire-shape and security invariants stay
auditable end-to-end. Any agent — Claude, Codex, Cursor, human reviewer —
working in this directory MUST read this file before making changes.

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

### 10. Public API only changes through index.ts

Anything a consumer can import from `@wuphf/protocol` is in
`packages/protocol/src/index.ts`. Add new exports there explicitly; don't
rely on transitive re-exports from sub-modules. The visible public surface
is what we promise to keep stable.

---

## Working rules for AI agents

### Before editing

1. Read this file. Read the file you're about to edit. Read the test file
   covering it.
2. Run `bun run typecheck && bun run check && bun run test` from
   `packages/protocol/` to confirm the baseline is green. If anything is
   already broken, surface that — don't fix unrelated things by stealth.
3. Identify which hard constraints (above) your change interacts with.

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
