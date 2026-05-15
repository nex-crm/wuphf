# @wuphf/protocol

WUPHF v1 protocol package: branded types, receipt schema, IPC envelopes, audit events.

This package is the moat. It defines the type-system invariants that make tampering and substitution observable:

- **`FrozenArgs`** — args canonicalized (RFC 8785 JCS) and content-addressed by SHA-256 at freeze time. The only type the executor accepts.
- **`SanitizedString`** — NFKC-normalized, allowlist-filtered, recursively walked. The only string type the renderer accepts.
- **`SignedApprovalToken`** — WebAuthn-backed approval proof over a discriminated `ApprovalClaim` and role-bearing `ApprovalScope`. Replaces stringly-typed `ApprovedBy`.
- **`ReceiptSnapshot`** — append-only audit detail. Every approved tool call produces one.
- **Audit event** — hash-chained CBOR-line records on disk; the chain hash is computed over a JCS projection (see `serializeAuditEventRecordForHash`) using the formula `eventHash = sha256(asciiLowerHex(prevHash) || jcsBytes(record))`. The ASCII-hex form of `prevHash` is intentional — it keeps debug dumps readable but is non-standard, so cross-language verifiers MUST mix the 64-byte ASCII string, not the 32 raw bytes. `GENESIS_PREV_HASH = sha256("wuphf:audit:genesis:v1")`. Test vectors live in `tests/audit-event.spec.ts` (golden serialization + golden eventHash). Periodic Merkle roots signed by per-install non-exportable key.
- **IPC envelopes** — renderer ↔ broker over loopback HTTP/SSE/WebSocket. NOT Electron `contextBridge` for app data.
- **Cost ledger types** — integer `MicroUsd` brand (no float drift), `BudgetId` ULID, closed `BudgetScope`, and three audit payloads (`cost_event`, `budget_set`, `budget_threshold_crossed`) that drive the AI-gateway spend chokepoint. See `docs/modules/cost.md`.

## No I/O

This package is pure types and pure-data classes. SQLite, filesystem, network, keychain — all live in other packages. That keeps the moat invariants verifiable in isolation.

## RFC anchor

Spec: `business-musings/wuphf-greenfield-rewrite-rfc-2026-05.md` §6 (Receipt schema), §7.3 (IPC discipline).

## Credential handle wire shape

`CredentialHandle` is a runtime capability wrapper with private `id`, `agentId`,
and `scope` slots. Its JSON wire shape is only `{ version: 1, id }`; the handle
id is the capability, while `agentId` and scope are trusted broker context used
by `credentialHandleFromJson`. Object spread, `Object.assign`, and
`Object.keys` expose no handle fields. `structuredClone(handle)` produces an
empty plain object, not a `CredentialHandle`, so clones cannot be used to
dereference secrets.

## Approval token wire shape

`SignedApprovalToken` is an envelope: `{ schemaVersion, tokenId, claim, scope, notBefore, expiresAt, issuedTo, signature }`. `claim` is a discriminated approval intent, `scope` carries the single-use capability bounds and approver `role`, timestamps are caller-supplied integer epoch milliseconds, and `signature` is a structured WebAuthn assertion. Broker verification output is not client-controlled token data; receipts carry it separately as `ApprovalEvent.tokenVerdict`.

External writes carry `writeId`. A `receipt_co_sign` token with `claim.writeId` authorizes only that write; a token without `claim.writeId` is receipt-scoped. Approval submissions use `ApprovalSubmitRequest`: `{ receiptId, approvalToken, idempotencyKey: IdempotencyKey }`. Clients mint the idempotency key and accepted responses echo it so clients can safely retry a lost `202 Accepted` with the same key. Idempotency keys are branded `IdempotencyKey` values and must match `/^[A-Za-z0-9_-]{1,128}$/`. Use `approvalSubmitRequestFromJson` for JSON input, then `validateApprovalSubmitRequest` at the IPC boundary to reject unknown request, token, claim, scope, and assertion keys; validate branded `receiptId`, `idempotencyKey`, optional `claim.writeId`, and `claim.frozenArgsHash`; require a `receipt_co_sign` claim with matching scope; enforce token lifetime and `receiptId === approvalToken.claim.receiptId`. The broker still owns WebAuthn verification, expiry against current time, credential trust, role threshold policy, write/diff binding, replay, and policy decisions.

## Resource budgets

Protocol-level resource caps live in `src/budgets.ts` and are exported from `src/index.ts` so downstream consumers can enforce the same contract. The receipt cap is 10 MiB serialized; per-blob caps are 1 MiB for `FrozenArgs` canonical JSON, `SanitizedString` UTF-8 text, and each audit event body before base64/JCS serialization; `EventLsn` strings are capped at 256 bytes before format parsing. Receipt arrays are bounded (`toolCalls` 1,024; `filesChanged`, `sourceReads`, `notebookWrites`, and `wikiWrites` 10,000; `commits` 1,024; `writes` 256; `approvals` 64). Approval tokens are capped at a 30-minute lifetime; approval claim canonical JSON is capped at 64 KiB, scope canonical JSON at 8 KiB, and WebAuthn assertions at 16 KiB total with 16 KiB per assertion field. These numbers keep normal large tasks viable while preventing runaway receipts, blobs, event bodies, approval submissions, and stale capabilities from exhausting verifier memory.

### Cost-event + budget caps (wire-contract)

The cost-ledger surface adds five caps that are part of the wire contract — wire payloads that violate these are rejected by `costAuditPayloadToBytes` / `receiptFromJson` BEFORE any storage or signing path runs. Downstream verifiers (TS, Go, Rust) must enforce the same numbers:

| Constant | Value | Meaning |
|---|---|---|
| `MAX_COST_EVENT_AMOUNT_MICRO_USD` | `100_000_000` ($100) | Per-event spend cap; the gateway validates estimator output against this before `appendCostEvent`. |
| `MAX_BUDGET_LIMIT_MICRO_USD` | `1_000_000_000_000` ($1M) | Per-budget ceiling; covers the upper bound for office/team/agent budgets. |
| `MAX_BUDGET_THRESHOLD_BPS` | `10_000` | Threshold basis-points are in `(0, 10_000]`; 10_000 bps = 100% of the budget limit. |
| `MAX_BUDGET_THRESHOLDS` | `8` | At most 8 threshold-crossed events per budget (e.g. 50/75/90/100%). |
| `MAX_COST_MODEL_BYTES` | `128` | `cost_event.model` string length cap (covers dated snapshots like `claude-haiku-4-5-20251001`). |

These are first-class wire constants: producers must validate before emit, and consumers must reject before deserialize. Crossing the per-event cap is the primary defense against a runaway estimator billing the office budget in one call.

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
