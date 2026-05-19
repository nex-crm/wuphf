// HTTP routes for explicit approval requests.
//
// Mounted under `/api/v1/approvals` when the host supplies an approvals
// config block. The listener owns loopback and bearer checks before this
// dispatcher runs.

import { createHash } from "node:crypto";
import type { IncomingMessage, ServerResponse } from "node:http";

import {
  type AgentId,
  APPROVAL_REQUEST_STATUS_VALUES,
  type ApiToken,
  type ApprovalDecidedAuditPayload,
  type ApprovalDecisionRequest,
  type ApprovalRequest,
  type ApprovalRequestCreateRequest,
  type ApprovalRequestedAuditPayload,
  type ApprovalRequestId,
  type ApprovalRequestStatus,
  type ApprovalStreamEvent,
  approvalDecisionRequestFromJson,
  approvalDecisionRequestToJsonValue,
  approvalDecisionResponseToJsonValue,
  approvalGetResponseToJsonValue,
  approvalListResponseToJsonValue,
  approvalRequestCreateRequestFromJson,
  approvalRequestCreateRequestToJsonValue,
  approvalRequestCreateResponseToJsonValue,
  asApprovalRequestId,
  asSignerIdentity,
  asTaskId,
  asThreadId,
  canonicalJSON,
  type EventLsn,
  type IdempotencyKey,
  isApprovalRequestId,
  MAX_ROUTE_APPROVAL_LIST_ITEMS,
  parseLsn,
  routeErrorToJsonValue,
  type SignerIdentity,
  type ThreadId,
  validateApprovalStreamEvent,
} from "@wuphf/protocol";
import BetterSqlite3 from "better-sqlite3";

import { agentIdForBearer } from "../auth.ts";
import type { BrokerLogger } from "../types.ts";
import {
  type ApprovalAppender,
  ApprovalDecisionInvalidError,
  ApprovalIdempotencyConflictError,
  ApprovalPendingLimitExceededError,
  ApprovalRequestAlreadyDecidedError,
  ApprovalRequestAlreadyExistsError,
  ApprovalRequestNotFoundError,
  ApprovalThreadNotFoundError,
  ApprovalTokenAlreadyUsedError,
  ApprovalTokenIssuedToMismatchError,
} from "./appender.ts";
import type { ApprovalCommand, ParsedApprovalIdempotencyKey } from "./idempotency.ts";
import type { ApprovalListFilter, ApprovalProjection } from "./projections.ts";
import { approvalViewFromApproval } from "./view.ts";

const MAX_APPROVAL_BODY_BYTES = 256 * 1024;
const APPROVAL_STATUS_SET: ReadonlySet<string> = new Set<string>(APPROVAL_REQUEST_STATUS_VALUES);
const APPROVAL_ROUTE_ACTOR = asSignerIdentity("broker");
const APPROVAL_REQUEST_ID_ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";
const DEFAULT_APPROVAL_LIST_LIMIT = MAX_ROUTE_APPROVAL_LIST_ITEMS;

export interface ApprovalRouteDeps {
  readonly appender: ApprovalAppender;
  readonly projection: ApprovalProjection;
  readonly tokenAgentIds: ReadonlyMap<ApiToken, AgentId> | null;
  readonly defaultThreadId: ThreadId | null;
  readonly logger: BrokerLogger;
  readonly nowMs: () => number;
  readonly emit: (event: ApprovalStreamEvent) => void;
  readonly emitThreadEvent: (event: ApprovalThreadStreamEvent) => void;
}

export interface ApprovalThreadStreamEvent {
  readonly kind: "thread.pinned_approvals.changed";
  readonly threadId: ThreadId;
  readonly headLsn: EventLsn;
}

export async function handleApprovalRoute(
  req: IncomingMessage,
  res: ServerResponse,
  pathname: string,
  deps: ApprovalRouteDeps,
): Promise<boolean> {
  if (pathname === "/api/v1/approvals") {
    if (req.method === "POST") {
      await handleApprovalRequestPost(req, res, deps);
      return true;
    }
    if (req.method === "GET" || req.method === "HEAD") {
      handleApprovalList(req, res, deps);
      return true;
    }
    methodNotAllowed(res, "GET, HEAD, POST");
    return true;
  }

  const decisionId = approvalDecisionIdFromPathname(pathname);
  if (decisionId !== null) {
    if (req.method !== "POST") {
      methodNotAllowed(res, "POST");
      return true;
    }
    await handleApprovalDecisionPost(req, res, deps, decisionId);
    return true;
  }

  if (pathname.startsWith("/api/v1/approvals/")) {
    const id = approvalIdFromPathname(pathname);
    if (id === null) {
      notFound(res);
      return true;
    }
    if (req.method !== "GET" && req.method !== "HEAD") {
      methodNotAllowed(res, "GET, HEAD");
      return true;
    }
    handleApprovalGet(res, deps, id);
    return true;
  }

  return false;
}

async function handleApprovalRequestPost(
  req: IncomingMessage,
  res: ServerResponse,
  deps: ApprovalRouteDeps,
): Promise<void> {
  if (!ensureJsonContentType(req, res)) return;

  let body: string;
  try {
    body = await readBody(req, res, MAX_APPROVAL_BODY_BYTES);
  } catch {
    return;
  }

  let request: ApprovalRequestCreateRequest;
  const parsedJson = parseJsonBody(body);
  if (!parsedJson.ok) {
    malformedJson(res, deps, "approval_request", parsedJson.err);
    return;
  }
  try {
    request = approvalRequestCreateRequestFromJson(parsedJson.value);
  } catch (err) {
    invalidPayload(res, deps, "approval_request", err);
    return;
  }

  const nowMs = deps.nowMs();
  const payload = approvalRequestedPayloadFromRouteRequest(request, nowMs, deps.defaultThreadId);
  try {
    const result = deps.appender.requestApprovalIdempotent({
      payload,
      idempotency: parsedRouteIdempotencyKey(request.idempotencyKey, "approval.requested"),
      requestFingerprint: approvalRouteRequestFingerprint(
        "approval.requested",
        payload.requestId,
        approvalRequestCreateRequestToJsonValue(request),
      ),
      nowMs,
      render: (applied) => ({
        statusCode: 201,
        payload: jsonBuffer(
          approvalRequestCreateResponseToJsonValue({
            approvalRequest: applied.approval,
            headLsn: applied.lsn,
          }),
        ),
      }),
    });
    writeJsonRaw(res, result.statusCode, result.payload, result.replayed);
    if (!result.replayed && result.approval !== null && result.lsn !== null) {
      emitApprovalInvalidation("approval.requested", result.approval, result.lsn, deps);
      emitThreadPinnedApprovalsInvalidation(result.approval, result.lsn, deps);
    }
  } catch (err) {
    if (err instanceof ApprovalRequestAlreadyExistsError) {
      writeRouteError(res, 409, { error: "approval_request_exists" });
      return;
    }
    if (err instanceof ApprovalPendingLimitExceededError) {
      writeRouteError(res, 409, { error: "pending_approval_limit_exceeded" });
      return;
    }
    if (err instanceof ApprovalThreadNotFoundError) {
      writeRouteError(res, 400, { error: "thread_not_found" });
      return;
    }
    if (err instanceof ApprovalIdempotencyConflictError) {
      writeRouteError(res, 409, { error: "idempotency_key_conflict" });
      return;
    }
    if (writeSqliteErrorResponse(res, err, deps.logger, "approval_request_rejected")) return;
    throw err;
  }
}

async function handleApprovalDecisionPost(
  req: IncomingMessage,
  res: ServerResponse,
  deps: ApprovalRouteDeps,
  pathId: ApprovalRequestId,
): Promise<void> {
  if (!ensureJsonContentType(req, res)) return;

  let body: string;
  try {
    body = await readBody(req, res, MAX_APPROVAL_BODY_BYTES);
  } catch {
    return;
  }

  let request: ApprovalDecisionRequest;
  const parsedJson = parseJsonBody(body);
  if (!parsedJson.ok) {
    malformedJson(res, deps, "approval_decision", parsedJson.err);
    return;
  }
  try {
    request = approvalDecisionRequestFromJson(parsedJson.value);
  } catch (err) {
    invalidPayload(res, deps, "approval_decision", err);
    return;
  }

  const nowMs = deps.nowMs();
  const decidedBy = approvalDecisionActor(req, deps);
  if (decidedBy === null) {
    writeRouteError(res, 403, { error: "approval_actor_unresolved" });
    return;
  }
  const payload = approvalDecidedPayloadFromRouteRequest(pathId, request, nowMs, decidedBy);
  try {
    const result = deps.appender.decideApprovalIdempotent({
      payload,
      idempotency: parsedRouteIdempotencyKey(request.idempotencyKey, "approval.decided"),
      requestFingerprint: approvalRouteRequestFingerprint(
        "approval.decided",
        pathId,
        approvalDecisionRequestToJsonValue(request),
      ),
      nowMs,
      render: (applied) => ({
        statusCode: 201,
        payload: jsonBuffer(
          approvalDecisionResponseToJsonValue({
            approvalRequest: applied.approval,
            headLsn: applied.lsn,
          }),
        ),
      }),
    });
    writeJsonRaw(res, result.statusCode, result.payload, result.replayed);
    if (!result.replayed && result.approval !== null && result.lsn !== null) {
      emitApprovalInvalidation("approval.decided", result.approval, result.lsn, deps);
      emitThreadPinnedApprovalsInvalidation(result.approval, result.lsn, deps);
    }
  } catch (err) {
    if (err instanceof ApprovalDecisionInvalidError) {
      invalidPayload(res, deps, "approval_decision", err);
      return;
    }
    if (err instanceof ApprovalRequestNotFoundError) {
      notFound(res);
      return;
    }
    if (err instanceof ApprovalRequestAlreadyDecidedError) {
      writeRouteError(res, 409, { error: "approval_not_pending" });
      return;
    }
    if (err instanceof ApprovalTokenAlreadyUsedError) {
      writeRouteError(res, 409, { error: "approval_token_reused" });
      return;
    }
    if (err instanceof ApprovalTokenIssuedToMismatchError) {
      writeRouteError(res, 403, { error: "approval_token_actor_mismatch" });
      return;
    }
    if (err instanceof ApprovalIdempotencyConflictError) {
      writeRouteError(res, 409, { error: "idempotency_key_conflict" });
      return;
    }
    if (writeSqliteErrorResponse(res, err, deps.logger, "approval_decision_rejected")) return;
    throw err;
  }
}

function handleApprovalList(
  req: IncomingMessage,
  res: ServerResponse,
  deps: ApprovalRouteDeps,
): void {
  const parsed = parseListQuery(req);
  if (!parsed.ok) {
    writeRouteError(res, 400, { error: parsed.error });
    return;
  }
  try {
    const page = deps.projection.listPage(parsed.filter, {
      limit: parsed.limit,
      ...(parsed.afterHeadLsn === undefined ? {} : { afterHeadLsn: parsed.afterHeadLsn }),
    });
    writeJson(
      res,
      200,
      approvalListResponseToJsonValue({
        approvals: page.rows.map((row) => approvalViewFromApproval(row.approval)),
        nextCursor: page.nextCursor,
      }),
    );
  } catch (err) {
    if (writeSqliteErrorResponse(res, err, deps.logger, "approval_list_rejected")) return;
    throw err;
  }
}

function handleApprovalGet(
  res: ServerResponse,
  deps: ApprovalRouteDeps,
  id: ApprovalRequestId,
): void {
  try {
    const row = deps.projection.getById(id);
    if (row === null) {
      notFound(res);
      return;
    }
    writeJson(
      res,
      200,
      approvalGetResponseToJsonValue({ approval: approvalViewFromApproval(row.approval) }),
    );
  } catch (err) {
    if (writeSqliteErrorResponse(res, err, deps.logger, "approval_get_rejected")) return;
    throw err;
  }
}

function approvalRequestedPayloadFromRouteRequest(
  request: ApprovalRequestCreateRequest,
  nowMs: number,
  defaultThreadId: ThreadId | null,
): ApprovalRequestedAuditPayload {
  const threadId = request.threadId ?? defaultThreadId ?? undefined;
  return {
    requestId: approvalRequestIdFromIdempotencyKey(request.idempotencyKey),
    claim: request.claim,
    scope: request.scope,
    riskClass: request.riskClass,
    ...(threadId === undefined ? {} : { threadId }),
    ...(request.taskId === undefined ? {} : { taskId: request.taskId }),
    ...(request.receiptId === undefined ? {} : { receiptId: request.receiptId }),
    requestedBy: APPROVAL_ROUTE_ACTOR,
    requestedAt: new Date(nowMs),
  };
}

function approvalDecidedPayloadFromRouteRequest(
  requestId: ApprovalRequestId,
  request: ApprovalDecisionRequest,
  nowMs: number,
  decidedBy: SignerIdentity,
): ApprovalDecidedAuditPayload {
  const suppliedApprovalToken = request.decision === "approve" ? request.token : undefined;
  return {
    requestId,
    decision: request.decision,
    decidedBy,
    decidedAt: new Date(nowMs),
    ...(suppliedApprovalToken === undefined ? {} : { token: suppliedApprovalToken }),
  };
}

function approvalDecisionActor(
  req: IncomingMessage,
  deps: Pick<ApprovalRouteDeps, "tokenAgentIds">,
): SignerIdentity | null {
  if (deps.tokenAgentIds === null) return null;
  const agentId = agentIdForBearer(req, deps.tokenAgentIds);
  if (agentId === null) return null;
  // TODO(security): enforce reject/abstain decision capability once approval
  // roles are wired; this hook only attributes the bearer-bound agent.
  return asSignerIdentity(agentId);
}

function parsedRouteIdempotencyKey(
  idempotencyKey: IdempotencyKey,
  command: ApprovalCommand,
): ParsedApprovalIdempotencyKey {
  const raw = `${command}:${idempotencyKey}`;
  return { raw, command, ulid: "" };
}

function approvalRouteRequestFingerprint(
  command: ApprovalCommand,
  approvalId: ApprovalRequestId,
  body: Readonly<Record<string, unknown>>,
): string {
  return canonicalJSON({
    command,
    approvalId,
    body,
  });
}

function approvalRequestIdFromIdempotencyKey(idempotencyKey: IdempotencyKey): ApprovalRequestId {
  if (isApprovalRequestId(idempotencyKey)) return asApprovalRequestId(idempotencyKey);
  const digest = createHash("sha256").update(idempotencyKey).digest();
  let output = "";
  let buffer = 0;
  let bits = 0;
  for (const byte of digest) {
    buffer = (buffer << 8) | byte;
    bits += 8;
    while (bits >= 5 && output.length < 26) {
      bits -= 5;
      output += APPROVAL_REQUEST_ID_ALPHABET[(buffer >> bits) & 31];
    }
    buffer &= (1 << bits) - 1;
    if (output.length === 26) break;
  }
  return asApprovalRequestId(output);
}

function parseListQuery(req: IncomingMessage):
  | {
      readonly ok: true;
      readonly filter: ApprovalListFilter;
      readonly limit: number;
      readonly afterHeadLsn?: number;
    }
  | { readonly ok: false; readonly error: string } {
  const url = new URL(req.url ?? "", "http://127.0.0.1");
  const filter: {
    status?: ApprovalRequestStatus;
    threadId?: ReturnType<typeof asThreadId>;
    taskId?: ReturnType<typeof asTaskId>;
  } = {};

  const status = singleQueryParam(url.searchParams, "status");
  if (status === "multiple") return { ok: false, error: "invalid_status" };
  if (status !== undefined) {
    if (!APPROVAL_STATUS_SET.has(status)) return { ok: false, error: "invalid_status" };
    filter.status = status as ApprovalRequestStatus;
  }

  const threadId = singleQueryParam(url.searchParams, "threadId");
  if (threadId === "multiple") return { ok: false, error: "invalid_thread_id" };
  if (threadId !== undefined) {
    try {
      filter.threadId = asThreadId(threadId);
    } catch {
      return { ok: false, error: "invalid_thread_id" };
    }
  }

  const taskId = singleQueryParam(url.searchParams, "taskId");
  if (taskId === "multiple") return { ok: false, error: "invalid_task_id" };
  if (taskId !== undefined) {
    try {
      filter.taskId = asTaskId(taskId);
    } catch {
      return { ok: false, error: "invalid_task_id" };
    }
  }

  const limitParam = singleQueryParam(url.searchParams, "limit");
  if (limitParam === "multiple") return { ok: false, error: "invalid_limit" };
  let limit = DEFAULT_APPROVAL_LIST_LIMIT;
  if (limitParam !== undefined) {
    if (!/^\d+$/.test(limitParam)) return { ok: false, error: "invalid_limit" };
    const parsedLimit = Number.parseInt(limitParam, 10);
    if (!Number.isSafeInteger(parsedLimit) || parsedLimit < 1) {
      return { ok: false, error: "invalid_limit" };
    }
    limit = Math.min(parsedLimit, MAX_ROUTE_APPROVAL_LIST_ITEMS);
  }

  const cursor = singleQueryParam(url.searchParams, "cursor");
  if (cursor === "multiple") return { ok: false, error: "invalid_cursor" };
  if (cursor !== undefined) {
    try {
      const afterHeadLsn = parseLsn(cursor as EventLsn).localLsn;
      return { ok: true, filter, limit, afterHeadLsn };
    } catch {
      return { ok: false, error: "invalid_cursor" };
    }
  }

  return { ok: true, filter, limit };
}

function singleQueryParam(params: URLSearchParams, key: string): string | "multiple" | undefined {
  const values = params.getAll(key);
  if (values.length === 0) return undefined;
  if (values.length > 1) return "multiple";
  return values[0] ?? "";
}

function approvalIdFromPathname(pathname: string): ApprovalRequestId | null {
  const prefix = "/api/v1/approvals/";
  const encoded = pathname.slice(prefix.length);
  if (encoded.length === 0 || encoded.includes("/")) return null;
  try {
    return asApprovalRequestId(decodeURIComponent(encoded));
  } catch {
    return null;
  }
}

function approvalDecisionIdFromPathname(pathname: string): ApprovalRequestId | null {
  const prefix = "/api/v1/approvals/";
  const suffix = "/decision";
  if (!pathname.startsWith(prefix) || !pathname.endsWith(suffix)) return null;
  const encoded = pathname.slice(prefix.length, pathname.length - suffix.length);
  if (encoded.length === 0 || encoded.includes("/")) return null;
  try {
    return asApprovalRequestId(decodeURIComponent(encoded));
  } catch {
    return null;
  }
}

function emitApprovalInvalidation(
  kind: "approval.requested" | "approval.decided",
  approval: ApprovalRequest,
  headLsn: EventLsn,
  deps: ApprovalRouteDeps,
): void {
  const event: ApprovalStreamEvent = {
    id: headLsn,
    kind,
    emittedAt: new Date(deps.nowMs()).toISOString(),
    payload: {
      requestId: approval.id,
      ...(approval.threadId === undefined ? {} : { threadId: approval.threadId }),
      headLsn,
    },
  };
  const validation = validateApprovalStreamEvent(event);
  if (!validation.ok) {
    throw new Error(`invalid approval stream event: ${JSON.stringify(validation.errors)}`);
  }
  deps.emit(event);
}

function emitThreadPinnedApprovalsInvalidation(
  approval: ApprovalRequest,
  headLsn: EventLsn,
  deps: Pick<ApprovalRouteDeps, "emitThreadEvent">,
): void {
  if (approval.threadId === undefined) return;
  deps.emitThreadEvent({
    kind: "thread.pinned_approvals.changed",
    threadId: approval.threadId,
    headLsn,
  });
}

function invalidPayload(
  res: ServerResponse,
  deps: Pick<ApprovalRouteDeps, "logger">,
  routeName: string,
  err: unknown,
): void {
  const reason = err instanceof Error ? err.message : "validation_failed";
  deps.logger.warn(`${routeName}_rejected`, { reason: "invalid_payload" });
  writeRouteError(res, 422, { error: "invalid_payload", message: reason });
}

function malformedJson(
  res: ServerResponse,
  deps: Pick<ApprovalRouteDeps, "logger">,
  routeName: string,
  err: unknown,
): void {
  const reason = err instanceof Error ? err.message : "invalid_json";
  deps.logger.warn(`${routeName}_rejected`, { reason: "invalid_json" });
  writeRouteError(res, 400, { error: "invalid_json", message: reason });
}

function parseJsonBody(
  body: string,
): { readonly ok: true; readonly value: unknown } | { readonly ok: false; readonly err: unknown } {
  try {
    return { ok: true, value: JSON.parse(body) as unknown };
  } catch (err) {
    return { ok: false, err };
  }
}

function ensureJsonContentType(req: IncomingMessage, res: ServerResponse): boolean {
  const contentType = req.headers["content-type"];
  if (typeof contentType !== "string" || !isJsonMediaType(contentType)) {
    writeRouteError(res, 415, { error: "unsupported_media_type" });
    return false;
  }
  return true;
}

function isJsonMediaType(value: string): boolean {
  const semi = value.indexOf(";");
  const head = (semi === -1 ? value : value.slice(0, semi)).trim().toLowerCase();
  return head === "application/json";
}

function readBody(req: IncomingMessage, res: ServerResponse, maxBytes: number): Promise<string> {
  return new Promise((resolveFn, rejectFn) => {
    let receivedBytes = 0;
    const chunks: Buffer[] = [];
    let settled = false;
    const finish = (run: () => void): void => {
      if (settled) return;
      settled = true;
      run();
    };
    req.on("data", (chunk: Buffer) => {
      receivedBytes += chunk.length;
      if (receivedBytes > maxBytes) {
        req.pause();
        if (!res.writableEnded) {
          writeRouteError(res, 413, { error: "body_too_large" }, { Connection: "close" });
        }
        finish(() => rejectFn(new Error("body_too_large")));
        return;
      }
      chunks.push(chunk);
    });
    req.on("end", () => finish(() => resolveFn(Buffer.concat(chunks).toString("utf8"))));
    req.on("error", (err) => finish(() => rejectFn(err)));
    req.on("close", () => finish(() => rejectFn(new Error("body_read_aborted"))));
  });
}

function writeSqliteErrorResponse(
  res: ServerResponse,
  err: unknown,
  logger: BrokerLogger,
  rejectedEvent: string,
): boolean {
  if (!isSqliteError(err)) return false;
  if (isSqliteBusyError(err)) {
    logger.warn(rejectedEvent, { reason: "store_busy" });
    writeRouteError(res, 503, { error: "store_busy", retryAfterMs: 1000 }, { "Retry-After": "1" });
    return true;
  }
  if (isSqliteFullError(err)) {
    logger.warn(rejectedEvent, { reason: "store_full" });
    writeRouteError(res, 507, { error: "store_full" });
    return true;
  }
  if (isSqliteUnavailableError(err)) {
    logger.error(rejectedEvent, { reason: "storage_error" });
    writeRouteError(res, 503, { error: "storage_error" });
    return true;
  }
  return false;
}

interface SqliteErrorWithCode extends Error {
  readonly code: string;
}

function isSqliteError(err: unknown): err is SqliteErrorWithCode {
  return err instanceof BetterSqlite3.SqliteError;
}

function isSqliteFullError(err: SqliteErrorWithCode): boolean {
  return err.code === "SQLITE_FULL";
}

function isSqliteBusyError(err: SqliteErrorWithCode): boolean {
  return (
    err.code === "SQLITE_BUSY" ||
    err.code === "SQLITE_LOCKED" ||
    err.code.startsWith("SQLITE_BUSY_") ||
    err.code.startsWith("SQLITE_LOCKED_")
  );
}

function isSqliteUnavailableError(err: SqliteErrorWithCode): boolean {
  return (
    err.code === "SQLITE_READONLY" ||
    err.code === "SQLITE_CANTOPEN" ||
    err.code === "SQLITE_CORRUPT" ||
    err.code.startsWith("SQLITE_READONLY_") ||
    err.code.startsWith("SQLITE_IOERR") ||
    err.code.startsWith("SQLITE_CANTOPEN_") ||
    err.code.startsWith("SQLITE_CORRUPT_")
  );
}

function writeJson(
  res: ServerResponse,
  status: number,
  bodyValue: unknown,
  extraHeaders: Record<string, string> = {},
): void {
  const body = canonicalJSON(bodyValue);
  res.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Cache-Control": "no-store",
    "Content-Length": String(Buffer.byteLength(body, "utf8")),
    ...extraHeaders,
  });
  res.end(body);
}

function writeRouteError(
  res: ServerResponse,
  status: number,
  error: Parameters<typeof routeErrorToJsonValue>[0],
  extraHeaders: Record<string, string> = {},
): void {
  writeJsonRaw(res, status, jsonBuffer(routeErrorToJsonValue(error)), false, extraHeaders);
}

function writeJsonRaw(
  res: ServerResponse,
  status: number,
  payload: Buffer,
  replayed: boolean,
  extraHeaders: Record<string, string> = {},
): void {
  res.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Cache-Control": "no-store",
    "Content-Length": String(payload.byteLength),
    ...(replayed ? { "Idempotent-Replay": "true" } : {}),
    ...extraHeaders,
  });
  res.end(payload);
}

function jsonBuffer(value: Readonly<Record<string, unknown>>): Buffer {
  return Buffer.from(canonicalJSON(value), "utf8");
}

function notFound(res: ServerResponse): void {
  writeRouteError(res, 404, { error: "not_found" });
}

function methodNotAllowed(res: ServerResponse, allow: string): void {
  const body = jsonBuffer(routeErrorToJsonValue({ error: "method_not_allowed" }));
  res.writeHead(405, {
    Allow: allow,
    "Content-Type": "application/json; charset=utf-8",
    "Cache-Control": "no-store",
    "Content-Length": String(body.byteLength),
  });
  res.end(body);
}
