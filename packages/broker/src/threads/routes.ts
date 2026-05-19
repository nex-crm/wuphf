import type { IncomingMessage, ServerResponse } from "node:http";

import {
  asIdempotencyKey,
  asSignerIdentity,
  asThreadId,
  asThreadSpecRevisionId,
  canonicalJSON,
  type EventLsn,
  routeErrorToJsonValue,
  type Sha256Hex,
  type SignerIdentity,
  THREAD_STATUS_VALUES,
  type Thread,
  type ThreadCreateCommand,
  type ThreadExternalRefs,
  type ThreadId,
  type ThreadSpecEditCommand,
  type ThreadStatus,
  type ThreadStatusChangeCommand,
  threadCreateRequestFromJson,
  threadGetResponseToJsonValue,
  threadListResponseToJsonValue,
  threadMutationResponseToJsonValue,
  threadSpecContentHash,
  threadSpecEditRequestFromJson,
  threadStatusChangeRequestFromJson,
  validateThread,
} from "@wuphf/protocol";
import BetterSqlite3 from "better-sqlite3";

import {
  InvalidListCursorError,
  InvalidListLimitError,
  ReceiptStoreBusyError,
  ReceiptStoreFullError,
  ReceiptStoreUnavailableError,
} from "../receipt-store.ts";
import type { BrokerLogger } from "../types.ts";
import {
  type ThreadAppender,
  ThreadCommandValidationError,
  ThreadConflictError,
  ThreadNotFoundError,
  ThreadTerminalTransitionError,
} from "./appender.ts";
import {
  type IdempotencyParseError,
  type ParsedIdempotencyKey,
  parseThreadIdempotencyKey,
  type ThreadCommand,
} from "./idempotency.ts";
import {
  type ThreadStateRow,
  type ThreadStateStore,
  threadStateRowToThread,
} from "./projections.ts";
import type { ThreadReceiptIndexStore } from "./receipt-index.ts";

const MAX_THREAD_BODY_BYTES = 512 * 1_024;
const THREAD_STATUS_SET: ReadonlySet<string> = new Set<string>(THREAD_STATUS_VALUES);
const ROUTE_SIGNER: SignerIdentity = asSignerIdentity("broker");
const EMPTY_EXTERNAL_REFS: ThreadExternalRefs = Object.freeze({
  sourceUrls: Object.freeze([]),
  entityIds: Object.freeze([]),
});

export interface ThreadRouteDeps {
  readonly appender: ThreadAppender;
  readonly state: ThreadStateStore;
  readonly receiptIndex: ThreadReceiptIndexStore;
  readonly logger: BrokerLogger;
  readonly nowMs: () => number;
  readonly emitThreadEvent: (event: ThreadRouteStreamEvent) => void;
}

export interface ThreadRouteStreamEvent {
  readonly kind: "thread.created" | "thread.updated";
  readonly threadId: ThreadId;
  readonly headLsn: EventLsn;
}

export async function handleThreadRoute(
  req: IncomingMessage,
  res: ServerResponse,
  pathname: string,
  deps: ThreadRouteDeps,
): Promise<boolean> {
  if (pathname === "/api/v1/threads") {
    if (req.method === "GET" || req.method === "HEAD") {
      handleThreadList(req, res, deps);
      return true;
    }
    if (req.method === "POST") {
      await handleThreadCreate(req, res, deps);
      return true;
    }
    methodNotAllowed(res, "GET, POST");
    return true;
  }

  if (!pathname.startsWith("/api/v1/threads/")) {
    return false;
  }
  const suffix = pathname.slice("/api/v1/threads/".length);
  const parts = suffix.split("/");
  if (parts.length === 1) {
    if (req.method !== "GET" && req.method !== "HEAD") {
      methodNotAllowed(res, "GET");
      return true;
    }
    handleThreadGet(res, deps, parts[0] ?? "");
    return true;
  }
  if (parts.length === 2 && parts[1] === "spec") {
    if (req.method !== "PATCH") {
      methodNotAllowed(res, "PATCH");
      return true;
    }
    await handleThreadSpecPatch(req, res, deps, parts[0] ?? "");
    return true;
  }
  if (parts.length === 2 && parts[1] === "status") {
    if (req.method !== "PATCH") {
      methodNotAllowed(res, "PATCH");
      return true;
    }
    await handleThreadStatusPatch(req, res, deps, parts[0] ?? "");
    return true;
  }
  notFound(res);
  return true;
}

function handleThreadList(req: IncomingMessage, res: ServerResponse, deps: ThreadRouteDeps): void {
  const url = new URL(req.url ?? "/", "http://127.0.0.1");
  const status = parseStatusFilter(url.searchParams);
  if (!status.ok) {
    writeRouteError(res, 400, "invalid_status", status.reason);
    return;
  }
  try {
    const rows = deps.state.list(
      status.status === undefined ? undefined : { status: status.status },
    );
    const threads = rows.map((row) => threadFromRow(row, deps.receiptIndex));
    writeJsonValue(res, 200, threadListResponseToJsonValue({ threads }));
  } catch (err) {
    if (writeStorageErrorResponse(res, err, deps.logger, "thread_list_rejected")) return;
    throw err;
  }
}

function handleThreadGet(
  res: ServerResponse,
  deps: ThreadRouteDeps,
  encodedThreadId: string,
): void {
  const threadId = parseThreadIdPath(encodedThreadId);
  if (threadId === null) {
    notFound(res);
    return;
  }
  const row = deps.state.getById(threadId);
  if (row === null) {
    notFound(res);
    return;
  }
  try {
    writeJsonValue(
      res,
      200,
      threadGetResponseToJsonValue({ thread: threadFromRow(row, deps.receiptIndex) }),
    );
  } catch (err) {
    if (writeStorageErrorResponse(res, err, deps.logger, "thread_get_rejected")) return;
    throw err;
  }
}

async function handleThreadCreate(
  req: IncomingMessage,
  res: ServerResponse,
  deps: ThreadRouteDeps,
): Promise<void> {
  if (!ensureJsonContentType(req, res)) return;
  let parsed: unknown;
  try {
    parsed = await readJsonBody(req, res);
  } catch (err) {
    writeBodyReadError(res, err);
    return;
  }

  let command: ThreadCreateCommand;
  let idempotency: ParsedIdempotencyKey;
  try {
    const request = threadCreateRequestFromJson(parsed);
    const parsedIdempotency = parseThreadIdempotencyKey(request.idempotencyKey, "thread.create");
    if (!parsedIdempotency.ok) {
      writeIdempotencyError(res, parsedIdempotency.error, deps.logger, "thread_create");
      return;
    }
    idempotency = parsedIdempotency.key;
    const now = routeDate(deps.nowMs());
    command = {
      kind: "thread.create",
      idempotencyKey: asIdempotencyKey(idempotency.ulid),
      threadId: asThreadId(idempotency.ulid),
      title: request.title,
      createdBy: ROUTE_SIGNER,
      createdAt: now,
      externalRefs: request.externalRefs ?? EMPTY_EXTERNAL_REFS,
      content: request.specContent,
    };
  } catch (err) {
    writeRouteError(res, 400, "invalid_thread_command", errorMessage(err));
    return;
  }

  try {
    const result = deps.appender.appendCreateIdempotent({
      command,
      idempotency,
      nowMs: deps.nowMs(),
      render: renderThreadCommandAccepted(201),
    });
    if (result.applied !== null) {
      deps.emitThreadEvent(threadRouteStreamEventFromApplied(result.applied));
    }
    writeJsonRaw(res, result.statusCode, result.payload, result.replayed);
  } catch (err) {
    writeThreadAppenderError(res, err, deps.logger, "thread_create_rejected");
  }
}

async function handleThreadSpecPatch(
  req: IncomingMessage,
  res: ServerResponse,
  deps: ThreadRouteDeps,
  encodedThreadId: string,
): Promise<void> {
  const threadId = parseThreadIdPath(encodedThreadId);
  if (threadId === null) {
    notFound(res);
    return;
  }
  if (!ensureJsonContentType(req, res)) return;
  let parsed: unknown;
  try {
    parsed = await readJsonBody(req, res);
  } catch (err) {
    writeBodyReadError(res, err);
    return;
  }

  let command: ThreadSpecEditCommand;
  let baseContentHash: Sha256Hex;
  let idempotency: ParsedIdempotencyKey;
  try {
    const request = threadSpecEditRequestFromJson(parsed);
    const parsedIdempotency = parseThreadIdempotencyKey(request.idempotencyKey, "thread.spec.edit");
    if (!parsedIdempotency.ok) {
      writeIdempotencyError(res, parsedIdempotency.error, deps.logger, "thread_spec_edit");
      return;
    }
    idempotency = parsedIdempotency.key;
    const content = request.content;
    const contentHash = threadSpecContentHash(content);
    command = {
      kind: "thread.spec.edit",
      idempotencyKey: asIdempotencyKey(idempotency.ulid),
      threadId,
      revisionId: asThreadSpecRevisionId(idempotency.ulid),
      baseRevisionId: request.baseRevisionId,
      content,
      contentHash,
      authoredBy: ROUTE_SIGNER,
      authoredAt: routeDate(deps.nowMs()),
    };
    baseContentHash = request.baseContentHash;
  } catch (err) {
    writeRouteError(res, 400, "invalid_thread_command", errorMessage(err));
    return;
  }

  try {
    const result = deps.appender.appendSpecEditIdempotent({
      command,
      baseContentHash,
      idempotency,
      nowMs: deps.nowMs(),
      render: renderThreadCommandAccepted(200),
    });
    if (result.applied !== null) {
      deps.emitThreadEvent(threadRouteStreamEventFromApplied(result.applied));
    }
    writeJsonRaw(res, result.statusCode, result.payload, result.replayed);
  } catch (err) {
    writeThreadAppenderError(res, err, deps.logger, "thread_spec_edit_rejected");
  }
}

async function handleThreadStatusPatch(
  req: IncomingMessage,
  res: ServerResponse,
  deps: ThreadRouteDeps,
  encodedThreadId: string,
): Promise<void> {
  const threadId = parseThreadIdPath(encodedThreadId);
  if (threadId === null) {
    notFound(res);
    return;
  }
  if (!ensureJsonContentType(req, res)) return;
  let parsed: unknown;
  try {
    parsed = await readJsonBody(req, res);
  } catch (err) {
    writeBodyReadError(res, err);
    return;
  }

  let command: ThreadStatusChangeCommand;
  let idempotency: ParsedIdempotencyKey;
  try {
    const request = threadStatusChangeRequestFromJson(parsed);
    const parsedIdempotency = parseThreadIdempotencyKey(
      request.idempotencyKey,
      "thread.status.change",
    );
    if (!parsedIdempotency.ok) {
      writeIdempotencyError(res, parsedIdempotency.error, deps.logger, "thread_status_change");
      return;
    }
    idempotency = parsedIdempotency.key;
    command = {
      kind: "thread.status.change",
      idempotencyKey: asIdempotencyKey(idempotency.ulid),
      threadId,
      fromStatus: request.fromStatus,
      toStatus: request.toStatus,
      changedBy: ROUTE_SIGNER,
      changedAt: routeDate(deps.nowMs()),
    };
  } catch (err) {
    writeRouteError(res, 400, "invalid_thread_command", errorMessage(err));
    return;
  }

  try {
    const result = deps.appender.appendStatusChangeIdempotent({
      command,
      idempotency,
      nowMs: deps.nowMs(),
      render: renderThreadCommandAccepted(200),
    });
    if (result.applied !== null) {
      deps.emitThreadEvent(threadRouteStreamEventFromApplied(result.applied));
    }
    writeJsonRaw(res, result.statusCode, result.payload, result.replayed);
  } catch (err) {
    writeThreadAppenderError(res, err, deps.logger, "thread_status_change_rejected");
  }
}

function renderThreadCommandAccepted(statusCode: number): (applied: {
  readonly threadId: ThreadId;
  readonly headLsn: EventLsn;
  readonly revisionId: string;
  readonly contentHash: Sha256Hex;
}) => {
  readonly statusCode: number;
  readonly payload: Buffer;
} {
  return (applied) => ({
    statusCode,
    payload: Buffer.from(
      canonicalJSON(
        threadMutationResponseToJsonValue({
          threadId: applied.threadId,
          headLsn: applied.headLsn,
          revisionId: asThreadSpecRevisionId(applied.revisionId),
          contentHash: applied.contentHash,
        }),
      ),
      "utf8",
    ),
  });
}

function threadRouteStreamEventFromApplied(applied: {
  readonly threadId: ThreadId;
  readonly headLsn: EventLsn;
  readonly streamKind: "thread.created" | "thread.updated";
}): ThreadRouteStreamEvent {
  return {
    kind: applied.streamKind,
    threadId: applied.threadId,
    headLsn: applied.headLsn,
  };
}

function threadFromRow(row: ThreadStateRow, receiptIndex: ThreadReceiptIndexStore): Thread {
  const refs = receiptIndex.refsForThread(row.id);
  const thread = threadStateRowToThread(row, refs.taskIds);
  assertThreadValid(thread);
  return thread;
}

function assertThreadValid(thread: Thread): void {
  const threadValidation = validateThread(thread);
  if (!threadValidation.ok) {
    throw new Error(
      `thread projection failed validation: ${threadValidation.errors
        .map((error) => `${error.path}: ${error.message}`)
        .join("; ")}`,
    );
  }
}

function parseStatusFilter(
  params: URLSearchParams,
):
  | { readonly ok: true; readonly status?: ThreadStatus }
  | { readonly ok: false; readonly reason: string } {
  const values = params.getAll("status");
  if (values.length === 0) return { ok: true };
  if (values.length > 1) return { ok: false, reason: "status may appear only once" };
  const raw = values[0] ?? "";
  if (!THREAD_STATUS_SET.has(raw)) {
    return { ok: false, reason: "unknown thread status" };
  }
  return { ok: true, status: raw as ThreadStatus };
}

function parseThreadIdPath(encoded: string): ThreadId | null {
  if (encoded.length === 0 || encoded.includes("/")) return null;
  try {
    return asThreadId(decodeURIComponent(encoded));
  } catch {
    return null;
  }
}

function writeThreadAppenderError(
  res: ServerResponse,
  err: unknown,
  logger: BrokerLogger,
  rejectedEvent: string,
): void {
  if (err instanceof ThreadCommandValidationError) {
    logger.warn(rejectedEvent, { reason: "invalid_command" });
    writeRouteError(res, 400, "invalid_thread_command", err.message);
    return;
  }
  if (err instanceof ThreadNotFoundError) {
    logger.warn(rejectedEvent, { reason: "thread_not_found" });
    notFound(res);
    return;
  }
  if (err instanceof ThreadConflictError) {
    logger.warn(rejectedEvent, { reason: err.code });
    writeRouteError(res, 409, err.code);
    return;
  }
  if (err instanceof ThreadTerminalTransitionError) {
    logger.warn(rejectedEvent, { reason: "terminal_status_transition" });
    writeRouteError(res, 422, "terminal_status_transition");
    return;
  }
  if (writeSqliteErrorResponse(res, err, logger, rejectedEvent)) return;
  throw err;
}

function writeStorageErrorResponse(
  res: ServerResponse,
  err: unknown,
  logger: BrokerLogger,
  rejectedEvent: string,
): boolean {
  if (err instanceof ReceiptStoreFullError) {
    logger.warn(rejectedEvent, { reason: "store_full" });
    writeRouteError(res, 507, "store_full");
    return true;
  }
  if (err instanceof ReceiptStoreBusyError) {
    logger.warn(rejectedEvent, { reason: "store_busy" });
    writeRouteError(res, 503, "store_busy", undefined, { "Retry-After": "1" }, 1_000);
    return true;
  }
  if (err instanceof ReceiptStoreUnavailableError) {
    logger.error(rejectedEvent, { reason: "storage_error" });
    writeRouteError(res, 503, "storage_error");
    return true;
  }
  if (err instanceof InvalidListCursorError || err instanceof InvalidListLimitError) {
    logger.warn(rejectedEvent, { reason: "receipt_index_invalid" });
    writeRouteError(res, 500, "receipt_index_invalid");
    return true;
  }
  return writeSqliteErrorResponse(res, err, logger, rejectedEvent);
}

function writeSqliteErrorResponse(
  res: ServerResponse,
  err: unknown,
  logger: BrokerLogger,
  rejectedEvent: string,
): boolean {
  if (isSqliteFullError(err)) {
    logger.warn(rejectedEvent, { reason: "store_full" });
    writeRouteError(res, 507, "store_full");
    return true;
  }
  if (isSqliteBusyError(err)) {
    logger.warn(rejectedEvent, { reason: "store_busy" });
    writeRouteError(res, 503, "store_busy", undefined, { "Retry-After": "1" }, 1_000);
    return true;
  }
  if (isSqliteUnavailableError(err)) {
    logger.error(rejectedEvent, { reason: "storage_error" });
    writeRouteError(res, 503, "storage_error");
    return true;
  }
  return false;
}

function isSqliteFullError(err: unknown): boolean {
  return err instanceof BetterSqlite3.SqliteError && err.code === "SQLITE_FULL";
}

function isSqliteBusyError(err: unknown): boolean {
  if (!(err instanceof BetterSqlite3.SqliteError)) return false;
  return (
    err.code === "SQLITE_BUSY" ||
    err.code === "SQLITE_LOCKED" ||
    err.code.startsWith("SQLITE_BUSY_") ||
    err.code.startsWith("SQLITE_LOCKED_")
  );
}

function isSqliteUnavailableError(err: unknown): boolean {
  if (!(err instanceof BetterSqlite3.SqliteError)) return false;
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

function writeIdempotencyError(
  res: ServerResponse,
  err: IdempotencyParseError,
  logger: BrokerLogger,
  routeName: string,
): void {
  logger.warn(`${routeName}_rejected`, { reason: `idempotency_${err.code}` });
  if (err.code === "missing") {
    writeRouteError(res, 400, "idempotency_key_required");
    return;
  }
  writeRouteError(res, 400, "idempotency_key_invalid", idempotencyErrorMessage(err));
}

function idempotencyErrorMessage(err: Exclude<IdempotencyParseError, { code: "missing" }>): string {
  if (err.code === "malformed") return err.reason;
  if (err.code === "unknown_command") return `unknown command ${err.command}`;
  return `expected ${err.expected}, got ${err.actual}`;
}

function ensureJsonContentType(req: IncomingMessage, res: ServerResponse): boolean {
  const contentType = req.headers["content-type"];
  if (typeof contentType !== "string" || !isJsonMediaType(contentType)) {
    writeRouteError(res, 415, "unsupported_media_type");
    return false;
  }
  return true;
}

function isJsonMediaType(value: string): boolean {
  const semi = value.indexOf(";");
  const head = (semi === -1 ? value : value.slice(0, semi)).trim().toLowerCase();
  return head === "application/json";
}

async function readJsonBody(req: IncomingMessage, res: ServerResponse): Promise<unknown> {
  return JSON.parse(await readBody(req, res, MAX_THREAD_BODY_BYTES)) as unknown;
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
          writeRouteError(res, 413, "body_too_large", undefined, { Connection: "close" });
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

function writeBodyReadError(res: ServerResponse, err: unknown): void {
  if (res.writableEnded) return;
  writeRouteError(res, 400, "invalid_json", errorMessage(err));
}

function routeDate(nowMs: number): Date {
  if (!Number.isSafeInteger(nowMs) || nowMs < 0) {
    throw new Error(`thread route clock returned invalid timestamp: ${nowMs}`);
  }
  const date = new Date(nowMs);
  if (Number.isNaN(date.getTime())) {
    throw new Error(`thread route clock returned invalid timestamp: ${nowMs}`);
  }
  return date;
}

function writeJsonValue(
  res: ServerResponse,
  status: number,
  body: Readonly<Record<string, unknown>>,
  extraHeaders: Record<string, string> = {},
): void {
  const text = canonicalJSON(body);
  res.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Cache-Control": "no-store",
    "Content-Length": String(Buffer.byteLength(text, "utf8")),
    ...extraHeaders,
  });
  res.end(text);
}

function writeJsonRaw(
  res: ServerResponse,
  status: number,
  payload: Buffer,
  replayed: boolean,
): void {
  res.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Cache-Control": "no-store",
    "Content-Length": String(payload.byteLength),
    ...(replayed ? { "Idempotent-Replay": "true" } : {}),
  });
  res.end(payload);
}

function writeRouteError(
  res: ServerResponse,
  status: number,
  error: string,
  message?: string,
  extraHeaders: Record<string, string> = {},
  retryAfterMs?: number,
): void {
  writeJsonValue(
    res,
    status,
    routeErrorToJsonValue({ error, ...(message === undefined ? {} : { message }), retryAfterMs }),
    extraHeaders,
  );
}

function notFound(res: ServerResponse): void {
  writeRouteError(res, 404, "not_found");
}

function methodNotAllowed(res: ServerResponse, allow: string): void {
  writeRouteError(res, 405, "method_not_allowed", undefined, { Allow: allow });
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

export type { ThreadCommand };
