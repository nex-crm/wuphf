# @wuphf/protocol

WUPHF v1 protocol package: branded types, receipt schema, IPC envelopes, audit events.

This package is the moat. It defines the type-system invariants that make tampering and substitution observable:

- **`FrozenArgs`** — args canonicalized (RFC 8785 JCS) and content-addressed by SHA-256 at freeze time. The only type the executor accepts.
- **`SanitizedString`** — NFKC-normalized, allowlist-filtered, recursively walked. The only string type the renderer accepts.
- **`SignedApprovalToken`** — verifiable, scoped, expiring. Replaces stringly-typed `ApprovedBy`.
- **`ReceiptSnapshot`** — append-only audit detail. Every approved tool call produces one.
- **Audit event** — hash-chained CBOR-line records. `prev_hash = sha256(prev_hash || event_bytes)`. Periodic Merkle roots signed by per-install non-exportable key.
- **IPC envelopes** — renderer ↔ broker over loopback HTTP/SSE/WebSocket. NOT Electron `contextBridge` for app data.

## No I/O

This package is pure types and pure-data classes. SQLite, filesystem, network, keychain — all live in other packages. That keeps the moat invariants verifiable in isolation.

## RFC anchor

Spec: `business-musings/wuphf-greenfield-rewrite-rfc-2026-05.md` §6 (Receipt schema), §7.3 (IPC discipline).

## Validation

```bash
bun run typecheck
bun run test
bun run check
```

`tests/` contains fast-check property tests. CI gates (`ci:moat:frozen-args`, `ci:moat:diff-card`, `ci:audit:verify`, `ci:xss`, `ci:receipt:fuzz`) wire to these workflows.
