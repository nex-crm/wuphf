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
cap enforcement, and malformed assertion bytes.

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

## Cross-language verification

`verifier-reference.go` is a stdlib-only Go reference implementation of the
audit-chain, runner, agent-provider-routing, and signed-approval-token wire
contracts. It loads `audit-event-vectors.json`, `runner-vectors.json`,
`agent-provider-routing-vectors.json`, and `signed-approval-token-vectors.json`,
recomputes each canonical serialization and eventHash, and verifies
accept/reject behavior against the bundled vectors. Run it from this directory:

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
