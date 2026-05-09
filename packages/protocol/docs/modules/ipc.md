# Module: IPC

> Path: `packages/protocol/src/ipc.ts`, `packages/protocol/src/ipc-shared.ts` - Owner: protocol - Stability: stable

## 1. Purpose

This module defines the pure-data IPC contract for Wuphf's broker boundary: renderer/main OS verbs, loopback HTTP envelopes, approval submission, SSE projections, WebSocket frames, and DNS-rebinding loopback checks. It belongs in `@wuphf/protocol` because downstream packages need the same branded types and validators without importing I/O, Electron, HTTP servers, or keychain code. Removing it would leave bearer-token bootstrap, approval submission, and loopback security checks stringly typed across processes.

## 2. Public API surface

Types in `ipc.ts`: `BrokerPort` line 41, `ApiToken` 42, `RequestId` 43, `KeychainHandleId` 44, `OsVerbsApi` 108, `ApiBootstrap` 154, `BrokerHttpRequest<TBody>` 199, `BrokerHttpResponse<T>` 212, `BrokerError` 217, `ApprovalSubmitRequest` 234, `ApprovalSubmitResponse` 540, `StreamEventKind` 563, `StreamEvent<TPayload>` 577, `BackpressureFrame` 590, `WsFrame` 598, `AllowedLoopbackHost` 611.

Functions/constants in `ipc.ts`: `asBrokerPort` 59, `isBrokerPort` 66, `asApiToken` 70, `isApiToken` 80, `asRequestId` 84, `isRequestId` 91, `asKeychainHandleId` 95, `isKeychainHandleId` 102, `apiBootstrapFromJson` 180, `apiBootstrapToJson` 195, `validateApprovalSubmitRequest` 266, `ALLOWED_LOOPBACK_HOSTS` 610, `isAllowedLoopbackHost` 636, `isLoopbackRemoteAddress` 671.

Shared constants in `ipc-shared.ts`: `APPROVAL_CLAIMS_KEYS` line 14 and `SIGNED_APPROVAL_TOKEN_KEYS` 24. They stay outside `ipc.ts` so IPC, receipt codecs, and receipt validators share one key allowlist without import cycles or duplicated literal tuples.

## 3. Behavior contract

1. The runtime TypeScript surface MUST stay camelCase. The v0 bootstrap wire shape is `{ token, broker_url }`, and `apiBootstrapFromJson` / `apiBootstrapToJson` MUST be the only translation site between snake_case wire JSON and `ApiBootstrap.brokerUrl`.
2. Brand constructors MUST be the only supported way to materialize IPC brands: broker ports are integers 1..65535; API tokens are URL/header-safe strings of 16..512 chars; request IDs and keychain handle IDs are 1..128 chars starting with alphanumeric and continuing with alphanumeric, dot, underscore, or hyphen.
3. Broker HTTP responses MUST model success statuses 200, 201, 202 with a body, success 204 without a body, and non-success with `BrokerError`. `202 Accepted` is sufficient for queued or preview confirmations; runtime callers own the actual routing.
4. Approval submissions MUST carry only `receiptId`, `approvalToken`, and `idempotencyKey`. They MUST NOT carry mutable proposed payloads; the broker compares token signature and receipt-owned data.
5. `validateApprovalSubmitRequest` MUST reject non-objects, unknown request keys, accessor fields, missing fields, invalid `ReceiptId`, invalid `IdempotencyKey`, non-object tokens, unknown token keys, missing or non-`ed25519` algorithm, non-string signer key IDs, and empty or invalid-base64 signatures.
6. Approval token claims MUST reject unknown keys; require valid signer identity, role, receipt ID, frozen-args hash, risk class, `issuedAt`, and `expiresAt`; validate optional `writeId`; require non-empty WebAuthn assertion for high/critical risk; require `expiresAt` strictly after `issuedAt`; enforce `MAX_APPROVAL_TOKEN_LIFETIME_MS`; and require request `receiptId === approvalToken.claims.receiptId`.
7. The IPC validator MUST NOT verify Ed25519 signatures, current-time expiry, signer trust, write/diff binding, idempotency replay, or broker policy. Those require broker state.
8. `ApprovalSubmitResponse` MUST be one of: accepted/executed with `executionResult`, accepted/queued with `receiptId`, or `accepted: false` with `tampered`, `expired`, `wrong_hash`, or `policy_denied`.
9. SSE `StreamEventKind` and `WsFrame.t` values MUST remain closed wire unions. Adding an event or frame kind requires updating this module, tests, and any runtime validators/codecs.
10. DNS-rebinding defense MUST compose both gates: `isAllowedLoopbackHost(Host)` and `isLoopbackRemoteAddress(peerIp)`. Host validation accepts only `127.0.0.1`, `localhost` case-insensitively, exact unbracketed `::1`, bracketed `[::1]`, and those accepted hostname forms with valid numeric ports where documented. It rejects rebound suffixes, expanded IPv6, bracketed non-IPv6 hosts, malformed ports, spaces, comma lists, and trailing junk.
11. `isLoopbackRemoteAddress` MUST receive a peer IP with no port. It accepts `::1`, IPv4-mapped `::ffff:127.0.0.0/8`, and `127.0.0.0/8`; it rejects wildcard, private non-loopback, link-local, empty, malformed, and port-suffixed inputs. It does not parse `Forwarded` / `X-Forwarded-For`, validate Origin/CORS, resolve DNS, or prove the listener bind address.

## 4. Diagrams

### 4.1 Approval Submission - Sequence

```mermaid
sequenceDiagram
  participant Client
  participant IPC as validateApprovalSubmitRequest
  participant Broker
  Client->>IPC: ApprovalSubmitRequest
  IPC-->>Client: reject shape, brands, lifetime, receiptId mismatch
  IPC->>Broker: ok typed request
  Broker-->>Client: BrokerError for malformed/replay transport failure
  Broker->>Broker: verify signature, signer trust, receipt write/hash, policy
  Broker-->>Client: ApprovalSubmitResponse executed, queued, or accepted false
```

### 4.2 Loopback Host Decision - Flowchart

```mermaid
flowchart TD
  A["Host string"] --> B{"starts with ["}
  B -->|yes| C{"contains ] and inside is ::1"}
  C -->|no| R["reject"]
  C -->|yes| D{"suffix empty or :port"}
  D -->|empty| OK["allow"]
  D -->|:port| E{"port numeric 0..65535"}
  E -->|yes| OK
  E -->|no| R
  B -->|no| F{"multiple colons"}
  F -->|yes| G{"exact ::1"}
  G -->|yes| OK
  G -->|no| R
  F -->|no| H["split optional last :port"]
  H --> I{"port absent or valid"}
  I -->|no| R
  I -->|yes| J{"bare host is 127.0.0.1 or lowercase localhost"}
  J -->|yes| OK
  J -->|no| R
```

### 4.3 Approval Validator Pipeline - Flowchart

```mermaid
flowchart TD
  A["unknown request"] --> B{"object"}
  B -->|no| X["fail: request must be object"]
  B -->|yes| C["reject unknown request keys"]
  C --> D["require data properties: receiptId, idempotencyKey, approvalToken"]
  D --> E["validate ReceiptId and IdempotencyKey brands"]
  E --> F{"approvalToken object"}
  F -->|no| X
  F -->|yes| G["reject unknown token keys"]
  G --> H["validate claims shape first"]
  H --> I["require algorithm, signerKeyId, signature"]
  I --> J["reject unknown claim keys"]
  J --> K["validate claim fields and high risk WebAuthn"]
  K --> L{"expiresAt strictly after issuedAt"}
  L -->|no| X
  L -->|yes| M{"lifetime within cap"}
  M -->|no| X
  M -->|yes| N{"request receiptId equals claims receiptId"}
  N -->|yes| OK["ok"]
  N -->|no| X
```

### 4.4 ApprovalSubmitResponse States - State

```mermaid
stateDiagram-v2
  [*] --> Executed: accepted true / state executed
  [*] --> Queued: accepted true / state queued
  [*] --> Rejected: accepted false
  Executed --> [*]: appliedAt + executionResult
  Queued --> [*]: acceptedAt + receiptId
  Rejected --> [*]: tampered, expired, wrong_hash, policy_denied
```

## 5. Failure modes

| Input | Expected error message | Why this matters |
|---|---|---|
| `{ receiptId, approvalToken, idempotencyKey, extra: 1 }` | `extra is not allowed` | Strict unknown rejection prevents hidden protocol fields. |
| Accessor `approvalToken` property | `approvalToken must be a data property` | Avoids invoking hostile getters during validation. |
| Token `algorithm: "rsa"` | `approvalToken.algorithm must be ed25519` | Keeps the signed-token algorithm closed. |
| `expiresAt === issuedAt` | `expiresAt must be strictly after issuedAt` | Equal timestamps must not mint zero-lifetime capabilities. |
| Lifetime over cap | `exceeds MAX_APPROVAL_TOKEN_LIFETIME_MS` | Prevents stale bearer material from lasting beyond the cleanup window. |
| Host `127.0.0.1.evil.com` | `false` | DNS rebinding must fail on Host, not DNS resolution. |
| Remote `127.0.0.1:1234` | `false` | Callers must strip the peer port before remote-address validation. |

## 6. Invariants the module assumes from callers

Callers provide parsed runtime objects, not raw bytes. Approval claim dates must already be `Date` instances; raw JSON date strings require a codec before `validateApprovalSubmitRequest`. Broker code must strip ports from remote peer addresses, run both loopback checks, perform signature verification and policy decisions, and treat SSE/WebSocket payloads as untrusted until a runtime codec or validator checks them.

## 7. Audit findings (current code vs this spec)

| # | Spec section | File:line | Discrepancy | Severity | Fix needed |
|---|---|---|---|---|---|
| 1 | Sec 3.1 | `web/src/api/client.ts:18` | A caller still hand-rolls `{ broker_url } -> brokerUrl` instead of using `apiBootstrapFromJson`, so the codec is not the only translation site repo-wide. | MEDIUM | Route `/api-token` parsing through the protocol codec or document this as an intentional non-package boundary. |
| 2 | Sec 3.1, Sec 3.10 | `packages/protocol/src/ipc.ts:184` | `apiBootstrapFromJson` validates `broker_url` only as a string even though `ApiBootstrap.brokerUrl` is documented as a loopback broker URL. | MEDIUM | Parse the URL and require an allowed loopback host plus a usable broker port, or relax the documented contract. |
| 3 | Sec 3.9 | `packages/protocol/src/ipc.ts:563`, `packages/protocol/src/ipc.ts:598` | `StreamEventKind` and `WsFrame` are closed wire unions but have no exported runtime validators/codecs. | MEDIUM | Add reader/writer validators for SSE and WebSocket envelopes, or explicitly mark them type-only internal surfaces. |

## 8. Test coverage gaps (against this spec, not against current code)

| # | Spec section | What's untested | Why it matters | Suggested test |
|---|---|---|---|---|
| 1 | Sec 3.1 | `apiBootstrapFromJson` rejecting non-loopback or malformed `broker_url` | Prevents token bootstrap from pointing callers at attacker-controlled origins if the codec owns URL validation. | Add `broker_url: "https://evil.test"` and malformed URL cases. |
| 2 | Sec 3.5 | Top-level unknown request keys, missing request fields, non-object `approvalToken`, invalid `receiptId` | Confirms every early failure exit in the public validator. | Table-test the first half of the pipeline. |
| 3 | Sec 3.5-3.6 | Invalid signer identity, role, claims receipt ID, `writeId`, hash, risk class, invalid dates, non-base64 signature | Current tests sample key paths but not every claim validator. | One table with field mutation and expected reason regex. |
| 4 | Sec 3.6 | High/critical risk without non-empty WebAuthn assertion | This is a policy-relevant shape check. | Test high and critical reject empty/missing assertion and low accepts missing assertion. |
| 5 | Sec 3.6 | Lifetime exactly at `MAX_APPROVAL_TOKEN_LIFETIME_MS` through `validateApprovalSubmitRequest` | Budget tests cover the helper; IPC should prove it wires the helper correctly. | Build an approval request at the exact cap and expect `{ ok: true }`. |
| 6 | Sec 3.8 | `executed` and `accepted: false` response variants | Only `queued` is compile-smoked today. | Add type-level examples or runtime fixtures for all union arms. |
| 7 | Sec 3.9 | SSE and WebSocket unknown `kind` / `t` rejection | Wire unions need runtime rejection once validators exist. | Add validator tests when `streamEventFromJson` / `wsFrameFromJson` are introduced. |
| 8 | Sec 3.10-3.11 | Loopback edge forms: trailing dot, uppercase IPv4-mapped IPv6, port-suffixed remote, decimal/octal IPv4 lookalikes | These are common DNS-rebinding bypass probes. | Extend the loopback table with expected fail-closed cases. |
