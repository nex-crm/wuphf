# Protocol Testdata

## Broker URL conformance vectors

`broker-url-vectors.json` is the canonical fixture for the `BrokerUrl` bare
loopback-origin contract. The protocol package, the desktop renderer, and the
broker-internal classifier all validate this shape independently, so each
consumer should load the fixture and assert its local validator accepts every
`accepted` vector and rejects every `rejected` vector.

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
audit-chain wire contract. It loads `audit-event-vectors.json`, recomputes
each canonical serialization and eventHash, and compares against the bundled
expected values. Run it from this directory:

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
