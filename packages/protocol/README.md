# @wuphf/protocol

WUPHF v1 protocol package: branded types, receipt schema, IPC envelopes, audit events.

This package is the moat. It defines the type-system invariants that make tampering and substitution observable:

- **`FrozenArgs`** — args canonicalized (RFC 8785 JCS) and content-addressed by SHA-256 at freeze time. The only type the executor accepts.
- **`SanitizedString`** — NFKC-normalized, allowlist-filtered, recursively walked. The only string type the renderer accepts.
- **`SignedApprovalToken`** — Ed25519 signed envelope over `ApprovalClaims`, scoped by receipt and optionally by write. Replaces stringly-typed `ApprovedBy`.
- **`ReceiptSnapshot`** — append-only audit detail. Every approved tool call produces one.
- **Audit event** — hash-chained CBOR-line records on disk; the chain hash is computed over a JCS projection (see `serializeAuditEventRecordForHash`) using the formula `eventHash = sha256(asciiLowerHex(prevHash) || jcsBytes(record))`. The ASCII-hex form of `prevHash` is intentional — it keeps debug dumps readable but is non-standard, so cross-language verifiers MUST mix the 64-byte ASCII string, not the 32 raw bytes. `GENESIS_PREV_HASH = sha256("wuphf:audit:genesis:v1")`. Test vectors live in `tests/audit-event.spec.ts` (golden serialization + golden eventHash). Periodic Merkle roots signed by per-install non-exportable key.
- **IPC envelopes** — renderer ↔ broker over loopback HTTP/SSE/WebSocket. NOT Electron `contextBridge` for app data.

## No I/O

This package is pure types and pure-data classes. SQLite, filesystem, network, keychain — all live in other packages. That keeps the moat invariants verifiable in isolation.

## RFC anchor

Spec: `business-musings/wuphf-greenfield-rewrite-rfc-2026-05.md` §6 (Receipt schema), §7.3 (IPC discipline).

## Approval token wire shape

`SignedApprovalToken` is an envelope: `{ claims, algorithm: "ed25519", signerKeyId, signature }`. `signature` is base64 for an Ed25519 detached signature over `approvalClaimsToSigningBytes(claims)`, which canonicalizes the claim projection with `issuedAt` and `expiresAt` encoded as ISO 8601 strings. Broker verification output is not client-controlled token data; receipts carry it separately as `ApprovalEvent.tokenVerdict`.

External writes carry `writeId`. A token with `claims.writeId` authorizes only that write; a token without `claims.writeId` is receipt-scoped. Approval submissions use `ApprovalSubmitRequest`: `{ receiptId, approvalToken, idempotencyKey: IdempotencyKey }`. Clients mint the idempotency key and accepted responses echo it so clients can safely retry a lost `202 Accepted` with the same key. Idempotency keys are branded `IdempotencyKey` values and must match `/^[A-Za-z0-9_-]{1,128}$/`. Use `approvalSubmitRequestFromJson` for JSON input with ISO-string claim dates, then `validateApprovalSubmitRequest` at the IPC boundary to reject unknown request, token, and claim keys; validate branded `receiptId`, `idempotencyKey`, optional `claims.writeId`, and `claims.frozenArgsHash`; require `algorithm: "ed25519"`, `signerKeyId`, and bounded non-empty base64 `signature`; validate claim role, risk class, and Date shapes; and enforce `receiptId === approvalToken.claims.receiptId`. The broker still owns signature verification, expiry against current time, signer trust, write/diff binding, replay, and policy decisions.

## Resource budgets

Protocol-level resource caps live in `src/budgets.ts` and are exported from `src/index.ts` so downstream consumers can enforce the same contract. The receipt cap is 10 MiB serialized; per-blob caps are 1 MiB for `FrozenArgs` canonical JSON, `SanitizedString` UTF-8 text, and each audit event body before base64/JCS serialization; `EventLsn` strings are capped at 256 bytes before format parsing. Receipt arrays are bounded (`toolCalls` 1,024; `filesChanged`, `sourceReads`, `notebookWrites`, and `wikiWrites` 10,000; `commits` 1,024; `writes` 256; `approvals` 64). Approval tokens are capped at a 30-minute lifetime, approval signatures at 4 KiB, and WebAuthn assertions at 16 KiB. These numbers keep normal large tasks viable while preventing runaway receipts, blobs, event bodies, approval submissions, and stale capabilities from exhausting verifier memory.

`receiptFromJson` rejects oversized serialized input before parsing, then checks collection budgets before decoding fields. `receiptToJson` runs the typed budget validator before semantic validation and canonicalization.

## Merkle root checkpoint wire shape

`MerkleRootRecord` checkpoints use the JSON projection `{ seqNo, rootHash, signedAt, ephemeralKeyId, signature, certChainPem }`. `seqNo` is an `EventLsn` string such as `"v1:42"`, `rootHash` is a lowercase SHA-256 hex digest, `signedAt` is an ISO 8601 instant with milliseconds, `signature` is non-empty base64, and `certChainPem` is non-empty PEM text. Golden vectors for both audit events and Merkle root checkpoints live in `testdata/audit-event-vectors.json`.

## Streaming verifier

Long audit chains can be verified incrementally with `verifyChainIncremental(state, batch)`, starting from `INITIAL_VERIFIER_STATE`. Each batch is capped at `MAX_AUDIT_CHAIN_BATCH_SIZE` (10,000 records), so callers can stream large logs without holding the full chain in verifier state. `verifyChain(records)` remains the convenience wrapper for already-materialized arrays and uses the same incremental verifier internally.

## Validation

```bash
bun run typecheck
bun run test
bun run check
```

`tests/` contains fast-check property tests. CI's `protocol` job in `.github/workflows/ci.yml` installs workspace dependencies, then runs typecheck, Biome check, Vitest through `bash scripts/test-protocol.sh`, the public API demo smoke test (`bun run packages/protocol/scripts/demo.ts`), and the Go reference verifier (`go run verifier-reference.go` from `packages/protocol/testdata`).
