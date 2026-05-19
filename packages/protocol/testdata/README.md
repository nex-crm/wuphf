# Protocol Testdata

## Broker URL conformance vectors

`broker-url-vectors.json` is the canonical fixture for the `BrokerUrl` bare
loopback-origin contract. The protocol package, the desktop renderer, and the
broker-internal classifier all validate this shape independently, so each
consumer should load the fixture and assert its local validator accepts every
`accepted` vector and rejects every `rejected` vector.

## Credential Handle Vectors

`credential-handle-vectors.json` pins the v1 credential handle wire shape:
`{ version: 1, id }`. The handle id is the capability; `agentId` and scope are
trusted broker-side context used to rehydrate a runtime `CredentialHandle`, not
serialized handle fields.

## Runner Vectors

`runner-vectors.json` pins the branch-9 runner control and event wire shapes.
It includes a canonical `RunnerSpawnRequest` plus every `RunnerEvent` variant
so Go/Rust runner ports can verify strict unknown-key rejection, schema-version
handling, and event parsing without depending on TypeScript tests. The fixture
includes both schema-versioned vectors and legacy unversioned vectors; parsers
must treat an absent `schemaVersion` as `1`, serialize `1`, and reject future
versions greater than they support.

## Agent Provider Routing Vectors

`agent-provider-routing-vectors.json` pins the branch-10
`AgentProviderRouting` wire shape. Accepted vectors must parse, normalize
`routes` by `RUNNER_KIND_VALUES` order, and serialize to the listed
`expected.canonicalSerialization` bytes. Rejected vectors must fail and include
the listed `expectedError` validation path, covering strict unknown-key
rejection, the 16-route cap, duplicate `kind` rejection, closed enum values,
and missing required fields.

Verify the fixture from this directory:

```bash
go run verifier-reference.go
```

## Signed Approval Token Vectors

`signed-approval-token-vectors.json` pins the branch-12
`SignedApprovalToken` WebAuthn wire shape. Accepted vectors must parse with
strict known-key rejection at every object boundary, enforce the role-bearing
approval scope, validate caller-supplied millisecond timestamps and WebAuthn
assertion budgets, and serialize to the listed canonical JSON bytes. Rejected
vectors cover unknown keys, missing scope role, claim/scope mismatch, lifetime
cap enforcement, malformed assertion bytes, and moat sanitization of Unicode
15.1 `Default_Ignorable_Code_Point` ranges.

## Audit Event Golden Vectors

`audit-event-vectors.json` is the cross-language fixture for WUPHF audit-chain
serialization and hashing. Implementers in Go, Rust, Python, or other runtimes
can load this file directly instead of scraping Vitest source.

The verifier contract is:

- `input.payload.bodyB64` is standard base64 for the raw opaque payload bytes.
- `expected.canonicalSerialization` is the UTF-8 JCS string for the record
  projection without `eventHash`.
- `expected.eventHash` is
  `sha256(asciiLowerHex(input.prevHash) || utf8(expected.canonicalSerialization))`.

To regenerate after an intentional wire-contract change, update the vector
values and run `bunx vitest run tests/audit-event.spec.ts` from
`packages/protocol/`. The test file at `../tests/audit-event.spec.ts` reads
this fixture and verifies the package serializer and hash function against it.

## Moat Disallowed Code-Point Table

`moat-disallowed-table.json` is the frozen Unicode classification the
`SanitizedString` `allowlist` ("moat") policy rejects: the union of `\p{C}`
(Cc, Cf, Cn, Co, Cs) and `\p{Default_Ignorable_Code_Point}`, captured once at a
pinned Unicode version.

The moat does **not** evaluate `\p{...}` at runtime — those property escapes
resolve against the host runtime's Unicode data, which differs across versions
(Node 24 ships Unicode 17; Bun 1.3 ships 15.1). A signer and a verifier on
different runtimes would disagree on the boundary. Freezing the table makes the
moat boundary a fixed cross-language contract.

The artifact has two parts:

- `disallowedRanges` — sorted, non-overlapping, non-adjacent inclusive
  `[start, end]` code-point ranges. Tab/LF/CR are `Cc` and therefore listed
  here; the sanitizer applies the tab/LF/CR carve-out on top of this raw table.
- `classificationVectors` — a curated corpus spanning every rejected class
  (Cc/Cf/Cn/Co/Cs and default-ignorables) plus range-edge probes. Every
  language port must classify each vector as listed.

Regenerate (only to deliberately bump the pinned Unicode version) with
`bun run scripts/generate-moat-table.ts` from `packages/protocol/`; it rewrites
both this file and the embedded `src/moat-disallowed-table.ts`. The TypeScript
test `tests/sanitized-string.spec.ts` pins the embedded table to this artifact.

## NFKC Normalization Table

`nfkc-table.json` is the frozen Unicode NFKC normalization data the
`SanitizedString` moat normalizes against — before and after the strip.

`String.prototype.normalize("NFKC")` resolves against the host runtime's
Unicode data, which differs across versions. The moat does **not** call it: a
signer on Bun 1.3 (Unicode 15.1) and a verifier on Node 24 (Unicode 17.0)
would otherwise produce different sanitized bytes (e.g. `U+A7F1` folds to `S`
under 17.0 but is unchanged under 15.1). Freezing the tables makes the
sanitized output a fixed cross-language contract — classification *and*
normalization.

The artifact has four parts:

- `decomposition` — `{ "cp", "to" }` rows mapping a code point to its
  fully-recursive NFKD decomposition. Hangul syllables are omitted; they are
  decomposed algorithmically.
- `composition` — `[starter, second, composite]` canonical composition pairs
  (non-Hangul).
- `combiningClass` — `[codePoint, class]` for every non-zero
  Canonical_Combining_Class.
- `normalizationVectors` — a curated `{ input, expected, name }` corpus. Every
  language port must reproduce `expected`. `tests/nfkc.spec.ts` and
  `verifier-reference.go` both check it.

The generator derives all three tables from vendored, authoritative Unicode
Character Database text files (`packages/protocol/scripts/ucd/` —
`UnicodeData-15.1.0.txt` and `CompositionExclusions-15.1.0.txt`), never from
the host runtime's `.normalize()`. It is therefore fully deterministic on any
host, regardless of the Unicode version the runtime ships (Bun reports a stale
`process.versions.unicode` and uses the platform ICU, so the runtime cannot be
trusted as the source). Regenerate (only to deliberately bump the pinned
Unicode version — replace the vendored UCD files first) with
`bun run scripts/generate-nfkc-table.ts` from `packages/protocol/`; it rewrites
both this file and the embedded `src/nfkc-table.generated.ts`. When run on a
host whose runtime genuinely ships the pinned version, it additionally
cross-checks `frozenNfkc` against `String.prototype.normalize("NFKC")` for
every code point as a bonus proof.

## Cross-language verification

`verifier-reference.go` is a stdlib-only Go reference implementation of the
audit-chain, runner, agent-provider-routing, signed-approval-token, moat-table,
and frozen-NFKC wire contracts. It loads `audit-event-vectors.json`,
`runner-vectors.json`, `agent-provider-routing-vectors.json`,
`signed-approval-token-vectors.json`, `moat-disallowed-table.json`, and
`nfkc-table.json`, recomputes each canonical serialization and eventHash,
binary-searches the moat ranges, re-normalizes with the frozen NFKC tables, and
verifies accept/reject, classification, and normalization behavior against the
bundled vectors. Run it from this directory:

```bash
cd packages/protocol/testdata
go run verifier-reference.go
```

If all vectors match, the wire contract is genuinely cross-language portable.
If any fail, the TypeScript writer and the Go reference disagree — coordinate
the wire-contract bump with downstream consumers before landing.

The Go file is intentionally minimal (no external deps) and only supports
the limited shapes used in the bundled vectors. For a production Go
implementation, swap the `canonicalize` helper for the
`github.com/cyberphone/json-canonicalization` library.
