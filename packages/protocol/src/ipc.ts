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
//      not 127.0.0.1, ::1, or localhost.

import type { Brand } from "./brand.ts";
import type { ReceiptId, SignedApprovalToken } from "./receipt.ts";
import type { Sha256Hex } from "./sha256.ts";

export type BrokerPort = Brand<number, "BrokerPort">;
export type ApiToken = Brand<string, "ApiToken">;
export type RequestId = Brand<string, "RequestId">;

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
   * broker dereferences it through the main-process keychain bridge.
   */
  getKeychainHandle(args: { agentSlug: string; service: string }): Promise<{
    handleId: string;
    expiresAt: string; // ISO 8601
  }>;
  setLoginItemSettings(args: { openAtLogin: boolean }): Promise<void>;
  requestSingleInstanceLock(): Promise<{ acquired: boolean }>;
  setAsDefaultProtocolClient(args: { scheme: "wuphf" }): Promise<{ ok: boolean }>;
  /**
   * Subscribe to deep-links. The handler receives URLs only.
   */
  onDeepLink(handler: (url: string) => void): () => void;
}

// ---------- Channel 2: Broker loopback ----------

/**
 * Bootstrap response shape. v0 endpoint preserved.
 *   GET http://127.0.0.1:<port>/api-token
 *   → { brokerBaseUrl, apiToken }
 */
export interface ApiBootstrap {
  readonly brokerBaseUrl: string; // http://127.0.0.1:<port>
  readonly apiToken: ApiToken;
  readonly issuedAt: string; // ISO 8601
  readonly expiresAt: string; // ISO 8601
}

export interface BrokerHttpRequest<TBody> {
  readonly requestId: RequestId;
  readonly token: ApiToken;
  readonly path: string; // e.g. "/v1/receipts"
  readonly method: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  readonly body?: TBody | undefined;
}

export type BrokerHttpResponse<T> =
  | { readonly ok: true; readonly status: 200 | 201 | 204; readonly body: T }
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
 * mutated payload to bypass the freeze would defeat the moat invariant.
 */
export interface ApprovalSubmitRequest {
  readonly receiptId: ReceiptId;
  readonly approvalToken: SignedApprovalToken;
  readonly clientFrozenArgsHash: Sha256Hex; // sanity-check only
}

export type ApprovalSubmitResponse =
  | {
      readonly accepted: true;
      readonly appliedAt: string;
      readonly executionResult: "applied" | "rollback";
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

export const ALLOWED_LOOPBACK_HOSTS: readonly string[] = ["127.0.0.1", "::1", "localhost"] as const;

export function isAllowedLoopbackHost(host: string): boolean {
  // Strip optional :port suffix.
  if (host.startsWith("[")) {
    const closeBracketIdx = host.indexOf("]");
    if (closeBracketIdx < 0) {
      return false;
    }
    const bareIpv6Host = host.slice(1, closeBracketIdx);
    const suffix = host.slice(closeBracketIdx + 1);
    if (suffix !== "" && !/^:\d+$/.test(suffix)) {
      return false;
    }
    return (ALLOWED_LOOPBACK_HOSTS as readonly string[]).includes(bareIpv6Host);
  }

  if (host.includes(":") && host.indexOf(":") !== host.lastIndexOf(":")) {
    return (ALLOWED_LOOPBACK_HOSTS as readonly string[]).includes(host);
  }

  const colonIdx = host.lastIndexOf(":");
  const bareHost = colonIdx >= 0 ? host.slice(0, colonIdx) : host;
  return (ALLOWED_LOOPBACK_HOSTS as readonly string[]).includes(bareHost);
}
