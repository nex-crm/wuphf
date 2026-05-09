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
import {
  MAX_APPROVAL_SIGNATURE_BYTES,
  MAX_WEBAUTHN_ASSERTION_BYTES,
  validateApprovalTokenLifetime,
} from "./budgets.ts";
import { canonicalJSON } from "./canonical-json.ts";
import { APPROVAL_CLAIMS_KEYS, SIGNED_APPROVAL_TOKEN_KEYS } from "./ipc-shared.ts";
import {
  type ApprovalClaims,
  type IdempotencyKey,
  isIdempotencyKey,
  isReceiptId,
  isWriteId,
  type ReceiptId,
  type SignedApprovalToken,
  type WriteFailureMetadata,
  type WriteResult,
} from "./receipt.ts";
import {
  APPROVAL_ROLE_VALUES,
  APPROVAL_TOKEN_ALGORITHM_VALUES,
  BASE64_RE,
  RISK_CLASS_VALUES,
} from "./receipt-literals.ts";
import { assertKnownKeys, hasOwn, isRecord, requireRecord } from "./receipt-utils.ts";
import { isSha256Hex } from "./sha256.ts";

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
const ISO_DATE_RE = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/;
const TEXT_ENCODER = new TextEncoder();

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
 * Bootstrap response shape. The TS interface uses camelCase so it conforms to
 * the package's lint-enforced naming convention (`useNamingConvention`); the
 * wire JSON keeps v0's snake_case keys (`{ token, broker_url }` from
 * `internal/team/broker_web_proxy.go` and `docs/architecture/broker-contract.md`).
 *
 *   Wire (JSON):  GET http://127.0.0.1:<port>/api-token → { token, broker_url }
 *   Runtime (TS): { token, brokerUrl }
 *
 * Use `apiBootstrapFromJson` / `apiBootstrapToJson` at the wire boundary;
 * never read snake_case keys off an `ApiBootstrap` value or hand-roll the
 * translation in callers.
 */
export interface ApiBootstrap {
  readonly token: ApiToken;
  readonly brokerUrl: string; // http://127.0.0.1:<port>
}

const API_BOOTSTRAP_WIRE_KEYS: ReadonlySet<string> = new Set(["token", "broker_url"]);

function requiredStringField(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): string {
  if (!hasOwn(record, key)) {
    throw new Error(`${path}: is required`);
  }
  const descriptor = Object.getOwnPropertyDescriptor(record, key);
  if (descriptor === undefined || !("value" in descriptor)) {
    throw new Error(`${path}: must be a data property`);
  }
  if (typeof descriptor.value !== "string") {
    throw new Error(`${path}: must be a string`);
  }
  return descriptor.value;
}

/** Parse the v0 wire JSON `{ token, broker_url }` into the camelCase TS shape. */
export function apiBootstrapFromJson(value: unknown): ApiBootstrap {
  const record = requireRecord(value, "apiBootstrap");
  assertKnownKeys(record, "apiBootstrap", API_BOOTSTRAP_WIRE_KEYS);
  const token = requiredStringField(record, "token", "apiBootstrap.token");
  const brokerUrl = requiredStringField(record, "broker_url", "apiBootstrap.broker_url");
  assertApiBootstrapBrokerUrl(brokerUrl);
  return { token: asApiToken(token), brokerUrl };
}

/**
 * Emit a v0-compatible wire JSON `{ token, broker_url }` from the TS shape.
 * Returns an unknown-typed record — callers who need the wire shape should
 * pass through `JSON.stringify` (or hand to a writer that emits the keys
 * verbatim). The wire keys are intentionally snake_case; the runtime TS
 * surface is camelCase by lint rule.
 */
export function apiBootstrapToJson(bootstrap: ApiBootstrap): Readonly<Record<string, string>> {
  return { token: bootstrap.token as string, broker_url: bootstrap.brokerUrl };
}

function assertApiBootstrapBrokerUrl(brokerUrl: string): void {
  let parsed: URL;
  try {
    parsed = new URL(brokerUrl);
  } catch {
    throw new Error("apiBootstrap.broker_url: must be http://<loopback>:<port>");
  }
  if (
    parsed.protocol !== "http:" ||
    parsed.port === "" ||
    !isAllowedLoopbackHost(parsed.hostname) ||
    !isBrokerPort(Number(parsed.port))
  ) {
    throw new Error("apiBootstrap.broker_url: must be http://<loopback>:<port>");
  }
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
 * signed token binds frozenArgsHash and may bind writeId; the broker verifies
 * the token signature against the receipt it owns. Clients retrying after a
 * lost 202 MUST re-submit the same idempotencyKey; broker enforcement makes
 * the same key produce the same outcome.
 */
export interface ApprovalSubmitRequest {
  readonly receiptId: ReceiptId;
  readonly approvalToken: SignedApprovalToken;
  readonly idempotencyKey: IdempotencyKey;
}

const APPROVAL_SUBMIT_REQUEST_KEYS_TUPLE = [
  "receiptId",
  "approvalToken",
  "idempotencyKey",
] as const satisfies readonly (keyof ApprovalSubmitRequest)[];
const APPROVAL_SUBMIT_REQUEST_KEYS: ReadonlySet<string> = new Set(
  APPROVAL_SUBMIT_REQUEST_KEYS_TUPLE,
);
const APPROVAL_SUBMIT_REQUEST_WIRE_KEYS: ReadonlySet<string> = new Set([
  "receipt_id",
  "approval_token",
  "idempotency_key",
]);
const SIGNED_APPROVAL_TOKEN_WIRE_KEYS: ReadonlySet<string> = new Set([
  "claims",
  "algorithm",
  "signer_key_id",
  "signature",
]);
const APPROVAL_CLAIMS_WIRE_KEYS: ReadonlySet<string> = new Set([
  "signer_identity",
  "role",
  "receipt_id",
  "write_id",
  "frozen_args_hash",
  "risk_class",
  "issued_at",
  "expires_at",
  "webauthn_assertion",
]);

export function approvalClaimsToSigningBytes(claims: ApprovalClaims): Uint8Array {
  if (!isRecord(claims)) {
    throw new Error("approvalClaimsToSigningBytes: claims must be an object");
  }
  const shape = validateApprovalClaimsShape(claims);
  if (!shape.ok) {
    throw new Error(`approvalClaimsToSigningBytes: ${shape.reason}`);
  }
  return TEXT_ENCODER.encode(canonicalJSON(approvalClaimsToSigningProjection(claims)));
}

export function approvalSubmitRequestFromJson(value: unknown): ApprovalSubmitRequest {
  const record = requireRecord(value, "approvalSubmitRequest");
  const usesSnakeCase = hasOwn(record, "receipt_id") || hasOwn(record, "approval_token");
  assertKnownKeys(
    record,
    "approvalSubmitRequest",
    usesSnakeCase ? APPROVAL_SUBMIT_REQUEST_WIRE_KEYS : APPROVAL_SUBMIT_REQUEST_KEYS,
  );

  const receiptId = receiptIdFromJson(
    requiredStringJsonField(
      record,
      usesSnakeCase ? "receipt_id" : "receiptId",
      usesSnakeCase ? "approvalSubmitRequest.receipt_id" : "approvalSubmitRequest.receiptId",
    ),
    usesSnakeCase ? "approvalSubmitRequest.receipt_id" : "approvalSubmitRequest.receiptId",
  );
  const approvalToken = signedApprovalTokenFromJson(
    requiredJsonField(
      record,
      usesSnakeCase ? "approval_token" : "approvalToken",
      usesSnakeCase
        ? "approvalSubmitRequest.approval_token"
        : "approvalSubmitRequest.approvalToken",
    ),
    usesSnakeCase ? "approvalSubmitRequest.approval_token" : "approvalSubmitRequest.approvalToken",
  );
  const idempotencyKey = idempotencyKeyFromJson(
    requiredStringJsonField(
      record,
      usesSnakeCase ? "idempotency_key" : "idempotencyKey",
      usesSnakeCase
        ? "approvalSubmitRequest.idempotency_key"
        : "approvalSubmitRequest.idempotencyKey",
    ),
    usesSnakeCase
      ? "approvalSubmitRequest.idempotency_key"
      : "approvalSubmitRequest.idempotencyKey",
  );
  const request: ApprovalSubmitRequest = { receiptId, approvalToken, idempotencyKey };
  const validation = validateApprovalSubmitRequest(request);
  if (!validation.ok) {
    throw new Error(`approvalSubmitRequest: ${validation.reason}`);
  }
  return request;
}

/**
 * Validate the wire shape of an `ApprovalSubmitRequest`: rejects unknown
 * envelope keys, requires the three fields with the correct branded shapes
 * (ReceiptId, IdempotencyKey, SignedApprovalToken envelope), rejects unknown
 * token and claim keys, requires the token algorithm/signing fields, validates
 * claim field shapes, and enforces the cross-field binding
 * `req.receiptId === req.approvalToken.claims.receiptId`.
 *
 * What this DOES NOT check (broker-side responsibilities):
 *   - Token signature validity (Ed25519 verification against signerKeyId).
 *   - Token expiry (`claims.expiresAt > now`).
 *   - Signer trust (whether the signerKeyId is a recognized approver).
 *   - writeId / frozenArgsHash binding to a specific external write (those
 *     live on the receipt; the IPC validator can't see the receipt's writes).
 *   - Idempotency-key replay status.
 *   - Broker policy (rate limits, account scope, etc.).
 */
export function validateApprovalSubmitRequest(
  req: unknown,
): { ok: true } | { ok: false; reason: string } {
  if (!isRecord(req)) {
    return { ok: false, reason: "request must be an object" };
  }
  for (const key of Object.keys(req)) {
    if (!APPROVAL_SUBMIT_REQUEST_KEYS.has(key)) {
      return { ok: false, reason: `${key} is not allowed` };
    }
  }
  const receiptIdField = requiredField(req, "receiptId", "receiptId");
  if (!receiptIdField.ok) return receiptIdField;
  const receiptId = receiptIdField.value;
  const idempotencyKeyField = requiredField(req, "idempotencyKey", "idempotencyKey");
  if (!idempotencyKeyField.ok) return idempotencyKeyField;
  const idempotencyKey = idempotencyKeyField.value;
  const approvalTokenField = requiredField(req, "approvalToken", "approvalToken");
  if (!approvalTokenField.ok) return approvalTokenField;
  const approvalToken = approvalTokenField.value;
  if (!isReceiptId(receiptId)) {
    return { ok: false, reason: "receiptId must be an uppercase ULID ReceiptId" };
  }
  if (!isIdempotencyKey(idempotencyKey)) {
    return { ok: false, reason: "idempotencyKey must match /^[A-Za-z0-9_-]{1,128}$/" };
  }
  if (!isRecord(approvalToken)) {
    return { ok: false, reason: "approvalToken must be an object" };
  }
  const tokenShape = validateApprovalTokenShape(approvalToken);
  if (!tokenShape.ok) {
    return tokenShape;
  }
  const claimsField = requiredField(approvalToken, "claims", "approvalToken.claims");
  if (!claimsField.ok) return claimsField;
  const claims = claimsField.value;
  if (!isRecord(claims)) {
    return { ok: false, reason: "approvalToken.claims must be an object" };
  }
  const claimsReceiptId = requiredField(claims, "receiptId", "approvalToken.claims.receiptId");
  if (!claimsReceiptId.ok) return claimsReceiptId;
  if (receiptId !== claimsReceiptId.value) {
    return {
      ok: false,
      reason: "receiptId must match approvalToken.claims.receiptId",
    };
  }
  return { ok: true };
}

function validateApprovalTokenShape(
  token: Record<string, unknown>,
): { ok: true } | { ok: false; reason: string } {
  const tokenKeys = knownKeysResult(token, "approvalToken", SIGNED_APPROVAL_TOKEN_KEYS);
  if (!tokenKeys.ok) return tokenKeys;

  const claims = requiredField(token, "claims", "approvalToken.claims");
  if (!claims.ok) return claims;
  if (!isRecord(claims.value)) {
    return { ok: false, reason: "approvalToken.claims must be an object" };
  }
  const claimsShape = validateApprovalClaimsShape(claims.value);
  if (!claimsShape.ok) return claimsShape;

  const algorithm = requiredField(token, "algorithm", "approvalToken.algorithm");
  if (!algorithm.ok) return algorithm;
  if (!isLiteralValue(algorithm.value, APPROVAL_TOKEN_ALGORITHM_VALUES)) {
    return { ok: false, reason: "approvalToken.algorithm must be ed25519" };
  }

  const signerKeyId = requiredField(token, "signerKeyId", "approvalToken.signerKeyId");
  if (!signerKeyId.ok) return signerKeyId;
  if (typeof signerKeyId.value !== "string") {
    return { ok: false, reason: "approvalToken.signerKeyId must be a string" };
  }

  const signature = requiredField(token, "signature", "approvalToken.signature");
  if (!signature.ok) return signature;
  if (typeof signature.value !== "string") {
    return { ok: false, reason: "approvalToken.signature must be a non-empty base64 string" };
  }
  if (signature.value.length > MAX_APPROVAL_SIGNATURE_BYTES) {
    return {
      ok: false,
      reason: "approvalToken.signature exceeds MAX_APPROVAL_SIGNATURE_BYTES",
    };
  }
  if (signature.value.length === 0 || !BASE64_RE.test(signature.value)) {
    return { ok: false, reason: "approvalToken.signature must be a non-empty base64 string" };
  }

  return { ok: true };
}

function validateApprovalClaimsShape(
  claims: Record<string, unknown>,
): { ok: true } | { ok: false; reason: string } {
  const claimKeys = knownKeysResult(claims, "approvalToken/claims", APPROVAL_CLAIMS_KEYS);
  if (!claimKeys.ok) return claimKeys;

  const signerIdentity = requiredField(
    claims,
    "signerIdentity",
    "approvalToken.claims.signerIdentity",
  );
  if (!signerIdentity.ok) return signerIdentity;
  const signerIdentityValue = signerIdentity.value;
  if (typeof signerIdentityValue !== "string") {
    return { ok: false, reason: "approvalToken.claims.signerIdentity must be a string" };
  }

  const role = requiredField(claims, "role", "approvalToken.claims.role");
  if (!role.ok) return role;
  const roleValue = role.value;
  if (!isLiteralValue(roleValue, APPROVAL_ROLE_VALUES)) {
    return { ok: false, reason: "approvalToken.claims.role must be a valid approval role" };
  }

  const receiptId = requiredField(claims, "receiptId", "approvalToken.claims.receiptId");
  if (!receiptId.ok) return receiptId;
  const receiptIdValue = receiptId.value;
  if (!isReceiptId(receiptIdValue)) {
    return {
      ok: false,
      reason: "approvalToken.claims.receiptId must be an uppercase ULID ReceiptId",
    };
  }

  const writeId = optionalField(claims, "writeId", "approvalToken.claims.writeId");
  if (!writeId.ok) return writeId;
  const writeIdValue = writeId.value;
  if (writeIdValue !== undefined && !isWriteId(writeIdValue)) {
    return { ok: false, reason: "approvalToken.claims.writeId must be a valid WriteId" };
  }

  const frozenArgsHash = requiredField(
    claims,
    "frozenArgsHash",
    "approvalToken.claims.frozenArgsHash",
  );
  if (!frozenArgsHash.ok) return frozenArgsHash;
  const frozenArgsHashValue = frozenArgsHash.value;
  if (!isSha256Hex(frozenArgsHashValue)) {
    return { ok: false, reason: "approvalToken.claims.frozenArgsHash must be a sha256 hex digest" };
  }

  const riskClass = requiredField(claims, "riskClass", "approvalToken.claims.riskClass");
  if (!riskClass.ok) return riskClass;
  const riskClassValue = riskClass.value;
  if (!isLiteralValue(riskClassValue, RISK_CLASS_VALUES)) {
    return { ok: false, reason: "approvalToken.claims.riskClass must be a valid risk class" };
  }

  const issuedAtField = requiredField(claims, "issuedAt", "approvalToken.claims.issuedAt");
  if (!issuedAtField.ok) return issuedAtField;
  const issuedAt = issuedAtField.value;
  if (!isValidDate(issuedAt)) {
    return { ok: false, reason: "approvalToken.claims.issuedAt must be a valid Date" };
  }

  const expiresAtField = requiredField(claims, "expiresAt", "approvalToken.claims.expiresAt");
  if (!expiresAtField.ok) return expiresAtField;
  const expiresAt = expiresAtField.value;
  if (!isValidDate(expiresAt)) {
    return { ok: false, reason: "approvalToken.claims.expiresAt must be a valid Date" };
  }

  const webauthnAssertion = optionalField(
    claims,
    "webauthnAssertion",
    "approvalToken.claims.webauthnAssertion",
  );
  if (!webauthnAssertion.ok) return webauthnAssertion;
  const webauthnAssertionValue = webauthnAssertion.value;
  if (webauthnAssertionValue !== undefined && typeof webauthnAssertionValue !== "string") {
    return {
      ok: false,
      reason: "approvalToken.claims.webauthnAssertion must be a string",
    };
  }
  if (
    typeof webauthnAssertionValue === "string" &&
    utf8ByteLengthUpTo(webauthnAssertionValue, MAX_WEBAUTHN_ASSERTION_BYTES) >
      MAX_WEBAUTHN_ASSERTION_BYTES
  ) {
    return {
      ok: false,
      reason: "approvalToken.claims.webauthnAssertion exceeds MAX_WEBAUTHN_ASSERTION_BYTES",
    };
  }
  if (
    (riskClassValue === "high" || riskClassValue === "critical") &&
    (typeof webauthnAssertionValue !== "string" || webauthnAssertionValue.length === 0)
  ) {
    return {
      ok: false,
      reason:
        "approvalToken.claims.webauthnAssertion must be a non-empty string for high/critical risk",
    };
  }

  if (issuedAt.getTime() >= expiresAt.getTime()) {
    return {
      ok: false,
      reason: "approvalToken.claims.expiresAt must be strictly after issuedAt",
    };
  }

  const approvalClaims: ApprovalClaims = {
    signerIdentity: signerIdentityValue,
    role: roleValue,
    receiptId: receiptIdValue,
    ...(writeIdValue === undefined ? {} : { writeId: writeIdValue }),
    frozenArgsHash: frozenArgsHashValue,
    riskClass: riskClassValue,
    issuedAt,
    expiresAt,
    ...(webauthnAssertionValue === undefined ? {} : { webauthnAssertion: webauthnAssertionValue }),
  };
  const lifetime = validateApprovalTokenLifetime(approvalClaims);
  if (!lifetime.ok) {
    return {
      ok: false,
      reason: `approvalToken.claims exceeds MAX_APPROVAL_TOKEN_LIFETIME_MS: ${lifetime.reason}`,
    };
  }

  return { ok: true };
}

function approvalClaimsToSigningProjection(claims: ApprovalClaims): Record<string, string> {
  return {
    signerIdentity: claims.signerIdentity,
    role: claims.role,
    receiptId: claims.receiptId,
    ...(claims.writeId === undefined ? {} : { writeId: claims.writeId }),
    frozenArgsHash: claims.frozenArgsHash,
    riskClass: claims.riskClass,
    issuedAt: claims.issuedAt.toISOString(),
    expiresAt: claims.expiresAt.toISOString(),
    ...(claims.webauthnAssertion === undefined
      ? {}
      : { webauthnAssertion: claims.webauthnAssertion }),
  };
}

function signedApprovalTokenFromJson(value: unknown, path: string): SignedApprovalToken {
  const record = requireRecord(value, path);
  const usesSnakeCase = hasOwn(record, "signer_key_id");
  assertKnownKeys(
    record,
    path,
    usesSnakeCase ? SIGNED_APPROVAL_TOKEN_WIRE_KEYS : SIGNED_APPROVAL_TOKEN_KEYS,
  );
  return {
    claims: approvalClaimsFromJson(requiredJsonField(record, "claims", `${path}.claims`), [
      path,
      "claims",
    ]),
    algorithm: requiredLiteralJsonField(
      record,
      "algorithm",
      `${path}.algorithm`,
      APPROVAL_TOKEN_ALGORITHM_VALUES,
    ),
    signerKeyId: requiredStringJsonField(
      record,
      usesSnakeCase ? "signer_key_id" : "signerKeyId",
      usesSnakeCase ? `${path}.signer_key_id` : `${path}.signerKeyId`,
    ),
    signature: requiredStringJsonField(record, "signature", `${path}.signature`),
  };
}

function approvalClaimsFromJson(value: unknown, path: readonly string[]): ApprovalClaims {
  const pathString = path.join(".");
  const record = requireRecord(value, pathString);
  const usesSnakeCase =
    hasOwn(record, "signer_identity") ||
    hasOwn(record, "receipt_id") ||
    hasOwn(record, "issued_at") ||
    hasOwn(record, "expires_at");
  assertKnownKeys(
    record,
    pathString,
    usesSnakeCase ? APPROVAL_CLAIMS_WIRE_KEYS : APPROVAL_CLAIMS_KEYS,
  );
  const writeId = optionalStringJsonField(
    record,
    usesSnakeCase ? "write_id" : "writeId",
    usesSnakeCase ? `${pathString}.write_id` : `${pathString}.writeId`,
  );
  const webauthnAssertion = optionalStringJsonField(
    record,
    usesSnakeCase ? "webauthn_assertion" : "webauthnAssertion",
    usesSnakeCase ? `${pathString}.webauthn_assertion` : `${pathString}.webauthnAssertion`,
  );
  return {
    signerIdentity: requiredStringJsonField(
      record,
      usesSnakeCase ? "signer_identity" : "signerIdentity",
      usesSnakeCase ? `${pathString}.signer_identity` : `${pathString}.signerIdentity`,
    ),
    role: requiredLiteralJsonField(record, "role", `${pathString}.role`, APPROVAL_ROLE_VALUES),
    receiptId: receiptIdFromJson(
      requiredStringJsonField(
        record,
        usesSnakeCase ? "receipt_id" : "receiptId",
        usesSnakeCase ? `${pathString}.receipt_id` : `${pathString}.receiptId`,
      ),
      usesSnakeCase ? `${pathString}.receipt_id` : `${pathString}.receiptId`,
    ),
    ...(writeId === undefined
      ? {}
      : {
          writeId: writeIdFromJson(
            writeId,
            usesSnakeCase ? `${pathString}.write_id` : `${pathString}.writeId`,
          ),
        }),
    frozenArgsHash: sha256HexFromJson(
      requiredStringJsonField(
        record,
        usesSnakeCase ? "frozen_args_hash" : "frozenArgsHash",
        usesSnakeCase ? `${pathString}.frozen_args_hash` : `${pathString}.frozenArgsHash`,
      ),
      usesSnakeCase ? `${pathString}.frozen_args_hash` : `${pathString}.frozenArgsHash`,
    ),
    riskClass: requiredLiteralJsonField(
      record,
      usesSnakeCase ? "risk_class" : "riskClass",
      usesSnakeCase ? `${pathString}.risk_class` : `${pathString}.riskClass`,
      RISK_CLASS_VALUES,
    ),
    issuedAt: requiredIsoDateJsonField(
      record,
      usesSnakeCase ? "issued_at" : "issuedAt",
      usesSnakeCase ? `${pathString}.issued_at` : `${pathString}.issuedAt`,
    ),
    expiresAt: requiredIsoDateJsonField(
      record,
      usesSnakeCase ? "expires_at" : "expiresAt",
      usesSnakeCase ? `${pathString}.expires_at` : `${pathString}.expiresAt`,
    ),
    ...(webauthnAssertion === undefined ? {} : { webauthnAssertion }),
  };
}

function requiredJsonField(record: Record<string, unknown>, key: string, path: string): unknown {
  const result = requiredField(record, key, path);
  if (!result.ok) {
    throw new Error(result.reason);
  }
  return result.value;
}

function requiredStringJsonField(
  record: Record<string, unknown>,
  key: string,
  path: string,
): string {
  const value = requiredJsonField(record, key, path);
  if (typeof value !== "string") {
    throw new Error(`${path}: must be a string`);
  }
  return value;
}

function optionalStringJsonField(
  record: Record<string, unknown>,
  key: string,
  path: string,
): string | undefined {
  const result = optionalField(record, key, path);
  if (!result.ok) {
    throw new Error(result.reason);
  }
  if (result.value === undefined) return undefined;
  if (typeof result.value !== "string") {
    throw new Error(`${path}: must be a string`);
  }
  return result.value;
}

function requiredLiteralJsonField<const T extends string>(
  record: Record<string, unknown>,
  key: string,
  path: string,
  allowed: readonly T[],
): T {
  const value = requiredStringJsonField(record, key, path);
  if (!isLiteralValue(value, allowed)) {
    throw new Error(`${path}: must be one of ${allowed.join(", ")}`);
  }
  return value;
}

function requiredIsoDateJsonField(
  record: Record<string, unknown>,
  key: string,
  path: string,
): Date {
  const value = requiredStringJsonField(record, key, path);
  if (!ISO_DATE_RE.test(value)) {
    throw new Error(`${path}: must be an ISO 8601 string`);
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime()) || date.toISOString() !== value) {
    throw new Error(`${path}: must be a valid ISO 8601 instant`);
  }
  return date;
}

function receiptIdFromJson(value: string, path: string): ReceiptId {
  if (!isReceiptId(value)) {
    throw new Error(`${path}: must be an uppercase ULID ReceiptId`);
  }
  return value;
}

function idempotencyKeyFromJson(value: string, path: string): IdempotencyKey {
  if (!isIdempotencyKey(value)) {
    throw new Error(`${path}: must match /^[A-Za-z0-9_-]{1,128}$/`);
  }
  return value;
}

function writeIdFromJson(value: string, path: string): NonNullable<ApprovalClaims["writeId"]> {
  if (!isWriteId(value)) {
    throw new Error(`${path}: must be a valid WriteId`);
  }
  return value;
}

function sha256HexFromJson(value: string, path: string): ApprovalClaims["frozenArgsHash"] {
  if (!isSha256Hex(value)) {
    throw new Error(`${path}: must be a sha256 hex digest`);
  }
  return value;
}

function knownKeysResult(
  record: Record<string, unknown>,
  path: string,
  allowed: ReadonlySet<string>,
): { ok: true } | { ok: false; reason: string } {
  try {
    assertKnownKeys(record, path, allowed);
    return { ok: true };
  } catch (err) {
    return { ok: false, reason: err instanceof Error ? err.message : String(err) };
  }
}

function requiredField(
  record: Record<string, unknown>,
  key: string,
  path: string,
): { ok: true; value: unknown } | { ok: false; reason: string } {
  if (!hasOwn(record, key)) {
    return { ok: false, reason: `${path} is required` };
  }
  const descriptor = Object.getOwnPropertyDescriptor(record, key);
  if (descriptor === undefined || !("value" in descriptor)) {
    return { ok: false, reason: `${path} must be a data property` };
  }
  if (descriptor.value === undefined) {
    return { ok: false, reason: `${path} is required` };
  }
  return { ok: true, value: descriptor.value };
}

function optionalField(
  record: Record<string, unknown>,
  key: string,
  path: string,
): { ok: true; value: unknown | undefined } | { ok: false; reason: string } {
  if (!hasOwn(record, key)) return { ok: true, value: undefined };
  const descriptor = Object.getOwnPropertyDescriptor(record, key);
  if (descriptor === undefined || !("value" in descriptor)) {
    return { ok: false, reason: `${path} must be a data property` };
  }
  return { ok: true, value: descriptor.value };
}

function isLiteralValue<const T extends string>(value: unknown, allowed: readonly T[]): value is T {
  return typeof value === "string" && allowed.includes(value as T);
}

function isValidDate(value: unknown): value is Date {
  return value instanceof Date && !Number.isNaN(value.getTime());
}

function utf8ByteLengthUpTo(value: string, budget: number): number {
  if (value.length > budget) return budget + 1;

  let bytes = 0;
  for (let i = 0; i < value.length; i++) {
    const code = value.charCodeAt(i);
    if (code <= 0x7f) {
      bytes += 1;
    } else if (code <= 0x7ff) {
      bytes += 2;
    } else if (code >= 0xd800 && code <= 0xdbff && i + 1 < value.length) {
      const next = value.charCodeAt(i + 1);
      if (next >= 0xdc00 && next <= 0xdfff) {
        bytes += 4;
        i += 1;
      } else {
        bytes += 3;
      }
    } else {
      bytes += 3;
    }

    if (bytes > budget) return budget + 1;
  }

  return bytes;
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
      readonly idempotencyKey: IdempotencyKey;
      readonly failureMetadata?: WriteFailureMetadata | undefined;
    }
  | {
      readonly accepted: true;
      readonly state: "queued";
      readonly acceptedAt: string; // ISO 8601
      readonly receiptId: ReceiptId;
      readonly idempotencyKey: IdempotencyKey;
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

export const STREAM_EVENT_KIND_VALUES = [
  "receipt.created",
  "receipt.updated",
  "receipt.finalized",
  "approval.requested",
  "approval.decided",
  "cost.exceeded",
  "agent.online",
  "agent.offline",
  "agent.message",
  "tool.call.started",
  "tool.call.completed",
  "backpressure",
] as const satisfies readonly StreamEventKind[];

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

export const WS_FRAME_TYPE_VALUES = [
  "stdout",
  "stderr",
  "stdin",
  "resize",
  "exit",
  "ping",
  "pong",
] as const satisfies readonly WsFrame["t"][];

export function isStreamEventKind(value: unknown): value is StreamEventKind {
  return isLiteralValue(value, STREAM_EVENT_KIND_VALUES);
}

export function isWsFrameType(value: unknown): value is WsFrame["t"] {
  return isLiteralValue(value, WS_FRAME_TYPE_VALUES);
}

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
