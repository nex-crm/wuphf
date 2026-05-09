// Renderer/main IPC envelopes.
//
// Two channels:
//
//   1. Electron `contextBridge` (renderer ↔ main): OS verbs ONLY.
//      No app data crosses this bridge. CI grep gate enforces this — any
//      `ipcMain.handle` channel name starting with `app:`, `data:`, `event:`,
//      `tool:`, `approval:`, `receipt:`, `cost:`, `agent:`, `wiki:`, or
//      `notebook:` fails the build.
//
//   2. Broker loopback HTTP/SSE/WebSocket (renderer ↔ broker, main ↔ broker):
//      All app data. Renderer is loaded from `http://127.0.0.1:<port>/` —
//      same-origin loopback. `GET /api-token` returns bearer + broker URL.
//      DNS-rebinding-guarded: broker rejects requests whose Host header is
//      not 127.0.0.1, ::1, or localhost AND whose RemoteAddr is not loopback.
//      Both checks must pass; the Host check alone is not the full gate.

import type { Brand } from "./brand.ts";
import type { ReceiptId, SignedApprovalToken, WriteResult } from "./receipt.ts";

export type BrokerPort = Brand<number, "BrokerPort">;
export type ApiToken = Brand<string, "ApiToken">;
export type RequestId = Brand<string, "RequestId">;
export type KeychainHandleId = Brand<string, "KeychainHandleId">;

// ---------- IPC brand constructors ----------
//
// The brand types are zero-overhead at runtime — they exist to prevent
// accidental string/number substitution at the API boundary (e.g. passing a
// plain number into a function that expects a BrokerPort, or feeding an
// arbitrary string into a credential-handle slot). Each constructor carries
// the same validation rules the broker enforces on the wire so that the only
// way to materialize a branded value is to pass through these checks.

const API_TOKEN_RE = /^[A-Za-z0-9._~+/-]{16,512}$/;
const REQUEST_ID_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;
const KEYCHAIN_HANDLE_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;

export function asBrokerPort(n: number): BrokerPort {
  if (!Number.isInteger(n) || n < 1 || n > 65535) {
    throw new Error(`asBrokerPort: expected integer in 1..65535, got ${n}`);
  }
  return n as BrokerPort;
}

export function isBrokerPort(value: unknown): value is BrokerPort {
  return typeof value === "number" && Number.isInteger(value) && value >= 1 && value <= 65535;
}

export function asApiToken(s: string): ApiToken {
  // Bearer token shape: URL-safe characters, length-bounded so log/header
  // truncation behaviour is predictable. The broker generates these with
  // crypto.randomBytes -> base64url, so the regex is conservative-permissive.
  if (!API_TOKEN_RE.test(s)) {
    throw new Error("asApiToken: not a valid API token shape");
  }
  return s as ApiToken;
}

export function isApiToken(value: unknown): value is ApiToken {
  return typeof value === "string" && API_TOKEN_RE.test(value);
}

export function asRequestId(s: string): RequestId {
  if (!REQUEST_ID_RE.test(s)) {
    throw new Error("asRequestId: not a valid RequestId shape");
  }
  return s as RequestId;
}

export function isRequestId(value: unknown): value is RequestId {
  return typeof value === "string" && REQUEST_ID_RE.test(value);
}

export function asKeychainHandleId(s: string): KeychainHandleId {
  if (!KEYCHAIN_HANDLE_RE.test(s)) {
    throw new Error("asKeychainHandleId: not a valid KeychainHandleId shape");
  }
  return s as KeychainHandleId;
}

export function isKeychainHandleId(value: unknown): value is KeychainHandleId {
  return typeof value === "string" && KEYCHAIN_HANDLE_RE.test(value);
}

// ---------- Channel 1: contextBridge OS verbs ----------

export interface OsVerbsApi {
  setTrayIcon(args: { state: "idle" | "active" | "error"; tooltip: string }): Promise<void>;
  notify(args: { title: string; body: string; urgent?: boolean }): Promise<void>;
  setBadge(args: { count: number }): Promise<void>;
  openFileDialog(args: {
    title: string;
    filters: ReadonlyArray<{ name: string; extensions: readonly string[] }>;
    multi?: boolean;
  }): Promise<{ canceled: boolean; paths: readonly string[] }>;
  saveFileDialog(args: {
    title: string;
    defaultPath?: string;
  }): Promise<{ canceled: boolean; path: string | null }>;
  /**
   * Open an opaque OS-keychain handle for a per-agent credential. The handle is
   * a process-scoped pointer; the actual secret never crosses the bridge — the
   * broker dereferences it through the main-process keychain bridge. The brand
   * exists to make accidental string substitution into a credential field a
   * compile error.
   */
  getKeychainHandle(args: { agentSlug: string; service: string }): Promise<{
    handleId: KeychainHandleId;
    expiresAt: string; // ISO 8601
  }>;
  setLoginItemSettings(args: { openAtLogin: boolean }): Promise<void>;
  requestSingleInstanceLock(): Promise<{ acquired: boolean }>;
  setAsDefaultProtocolClient(args: { scheme: "wuphf" }): Promise<{ ok: boolean }>;
  /** Subscribe to deep-links. The handler receives URLs only. */
  onDeepLink(handler: (url: string) => void): () => void;
}

// ---------- Channel 2: Broker loopback ----------

/**
 * Bootstrap response shape. Wire-stable with v0:
 *   GET http://127.0.0.1:<port>/api-token
 *   → { token, broker_url }
 *
 * Snake-keyed because that's what v0 emits (`internal/team/broker_web_proxy.go`)
 * and what `docs/architecture/broker-contract.md` documents. Renderer code that
 * wants camelCase should normalize after parsing the wire response.
 */
export interface ApiBootstrap {
  readonly token: ApiToken;
  readonly broker_url: string; // http://127.0.0.1:<port>
}

export interface BrokerHttpRequest<TBody> {
  readonly requestId: RequestId;
  readonly token: ApiToken;
  readonly path: string; // e.g. "/v1/receipts"
  readonly method: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  readonly body?: TBody | undefined;
}

/**
 * `204` carries no body — typed as `body?: undefined` so callers don't need to
 * fabricate a placeholder. `202 Accepted` is included because v0 already uses
 * it for queued/preview confirmations (`internal/team/broker_scan.go`).
 */
export type BrokerHttpResponse<T> =
  | { readonly ok: true; readonly status: 200 | 201 | 202; readonly body: T }
  | { readonly ok: true; readonly status: 204; readonly body?: undefined }
  | { readonly ok: false; readonly status: number; readonly error: BrokerError };

export interface BrokerError {
  readonly code: string;
  readonly message: string;
  readonly retryable: boolean;
  readonly retryAfterMs?: number | undefined;
  readonly receiptId?: ReceiptId | undefined;
}

/**
 * Approval submission. Carries only an opaque receipt_id and a verifiable
 * signed token. NEVER carries the mutable proposed payload — re-submitting a
 * mutated payload to bypass the freeze would defeat the moat invariant. The
 * signed token already binds frozenArgsHash; the broker verifies the token
 * signature against the receipt it owns.
 */
export interface ApprovalSubmitRequest {
  readonly receiptId: ReceiptId;
  readonly approvalToken: SignedApprovalToken;
}

/**
 * Approval submission response. `executed` carries the same `WriteResult` as
 * the receipt's external write so callers can switch on a single shape across
 * receipt projections and approval responses. `queued` covers the broker
 * accepting the approval but deferring execution (matches `ReceiptStatus`
 * `"approval_pending"` semantics).
 */
export type ApprovalSubmitResponse =
  | {
      readonly accepted: true;
      readonly state: "executed";
      readonly appliedAt: string; // ISO 8601
      readonly executionResult: WriteResult;
    }
  | {
      readonly accepted: true;
      readonly state: "queued";
      readonly acceptedAt: string; // ISO 8601
      readonly receiptId: ReceiptId;
    }
  | {
      readonly accepted: false;
      readonly reason: "tampered" | "expired" | "wrong_hash" | "policy_denied";
    };

// ---------- SSE ----------

export type StreamEventKind =
  | "receipt.created"
  | "receipt.updated"
  | "receipt.finalized"
  | "approval.requested"
  | "approval.decided"
  | "cost.exceeded"
  | "agent.online"
  | "agent.offline"
  | "agent.message"
  | "tool.call.started"
  | "tool.call.completed"
  | "backpressure";

export interface StreamEvent<TPayload = unknown> {
  readonly id: string;
  readonly kind: StreamEventKind;
  readonly emittedAt: string; // ISO 8601
  readonly receiptId?: ReceiptId | undefined;
  readonly payload: TPayload;
}

/**
 * Backpressure signal. Renderer/agent caller must pause for `retryAfterMs` and
 * re-query state. Drop-newest semantics: the broker's bounded mailbox dropped a
 * frame; events are a projection — re-query is cheap and correct.
 */
export interface BackpressureFrame {
  readonly retryAfterMs: number;
  readonly droppedSince: string; // ISO 8601
  readonly droppedCount: number;
}

// ---------- WebSocket (terminal/agent stdio) ----------

export type WsFrame =
  | { readonly t: "stdout"; readonly d: string }
  | { readonly t: "stderr"; readonly d: string }
  | { readonly t: "stdin"; readonly d: string }
  | { readonly t: "resize"; readonly cols: number; readonly rows: number }
  | { readonly t: "exit"; readonly code: number | null; readonly signal: string | null }
  | { readonly t: "ping" }
  | { readonly t: "pong" };

// ---------- DNS-rebinding guard (declared as a contract, broker enforces) ----------

// Tuple type — preserves literal narrowing for downstream consumers.
export const ALLOWED_LOOPBACK_HOSTS = ["127.0.0.1", "::1", "localhost"] as const;
export type AllowedLoopbackHost = (typeof ALLOWED_LOOPBACK_HOSTS)[number];

const PORT_RE = /^\d+$/;

function isValidPort(port: string): boolean {
  if (!PORT_RE.test(port)) return false;
  const parsed = Number(port);
  return Number.isInteger(parsed) && parsed >= 0 && parsed <= 65535;
}

/**
 * Validate the HTTP `Host` header against the loopback allowlist. This is the
 * Host-header half of the DNS-rebinding gate; the broker MUST also confirm the
 * remote address is loopback (see `isLoopbackRemoteAddress`). Either check
 * alone is insufficient.
 *
 * Accepted forms:
 *   "127.0.0.1", "127.0.0.1:8080"
 *   "localhost", "localhost:8080"   (case-insensitive on the hostname)
 *   "::1", "[::1]", "[::1]:8080"
 *
 * Rejected forms include `127.0.0.1.evil.com`, `Localhost.evil`, expanded IPv6
 * (`0:0:0:0:0:0:0:1`), unbracketed IPv6+port (`::1:8080`), `[localhost]`,
 * `[127.0.0.1]:8080`, malformed ports (`localhost:abc`), and trailing junk.
 */
export function isAllowedLoopbackHost(host: string): boolean {
  if (host.startsWith("[")) {
    const closeBracketIdx = host.indexOf("]");
    if (closeBracketIdx < 0) return false;
    const bareIpv6Host = host.slice(1, closeBracketIdx);
    if (bareIpv6Host !== "::1") return false;
    const suffix = host.slice(closeBracketIdx + 1);
    if (suffix === "") return true;
    if (!suffix.startsWith(":")) return false;
    return isValidPort(suffix.slice(1));
  }

  // Unbracketed multi-colon means raw IPv6 (or junk). Only allow exact "::1".
  if (host.includes(":") && host.indexOf(":") !== host.lastIndexOf(":")) {
    return host === "::1";
  }

  const colonIdx = host.lastIndexOf(":");
  const bareHost = colonIdx >= 0 ? host.slice(0, colonIdx) : host;
  if (colonIdx >= 0 && !isValidPort(host.slice(colonIdx + 1))) {
    return false;
  }
  if (bareHost === "127.0.0.1") return true;
  return bareHost.toLowerCase() === "localhost";
}

/**
 * Validate the request's RemoteAddr (peer IP, no port) against the loopback
 * range. The broker MUST gate on both this AND `isAllowedLoopbackHost(Host)`.
 * Without the RemoteAddr check, an attacker that gets a non-loopback bind by
 * mistake can spoof `Host: 127.0.0.1` and pass the Host gate.
 *
 * Accepts any address in 127.0.0.0/8, plus `::1`. Rejects 0.0.0.0 and
 * non-loopback IPs.
 */
export function isLoopbackRemoteAddress(remoteAddr: string): boolean {
  if (remoteAddr === "::1") return true;
  if (remoteAddr === "::ffff:127.0.0.1" || remoteAddr.startsWith("::ffff:127.")) {
    // IPv4-mapped IPv6 form of 127.0.0.0/8
    const v4 = remoteAddr.slice(7);
    return isLoopbackIpv4(v4);
  }
  return isLoopbackIpv4(remoteAddr);
}

function isLoopbackIpv4(addr: string): boolean {
  const parts = addr.split(".");
  if (parts.length !== 4) return false;
  for (const part of parts) {
    if (!PORT_RE.test(part)) return false;
    const n = Number(part);
    if (!Number.isInteger(n) || n < 0 || n > 255) return false;
  }
  return parts[0] === "127";
}
