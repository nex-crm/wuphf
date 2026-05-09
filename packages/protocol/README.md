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

`SignedApprovalToken` is an envelope: `{ claims, algorithm: "ed25519", signerKeyId, signature }`. `signature` is base64 for an Ed25519 detached signature over `canonicalJSON(claims)`. Broker verification output is not client-controlled token data; receipts carry it separately as `ApprovalEvent.tokenVerdict`.

External writes carry `writeId`. A token with `claims.writeId` authorizes only that write; a token without `claims.writeId` is receipt-scoped. Approval submissions also carry a client-minted `idempotencyKey`, and accepted responses echo it so clients can safely retry a lost `202 Accepted` with the same key.

## Validation

```bash
bun run typecheck
bun run test
bun run check
```

`tests/` contains fast-check property tests. CI gates (`ci:moat:frozen-args`, `ci:moat:diff-card`, `ci:audit:verify`, `ci:xss`, `ci:receipt:fuzz`) wire to these workflows.
