# Audit Event Golden Vectors

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
