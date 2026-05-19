import type { IncomingMessage, ServerResponse } from "node:http";

import {
  asIdempotencyKey,
  asSha256Hex,
  asSignerIdentity,
  asThreadId,
  asThreadSpecRevisionId,
  type EventLsn,
  type JsonValue,
  type ReceiptId,
  type ReceiptSnapshot,
  type Sha256Hex,
  type SignerIdentity,
  type TaskId,
  THREAD_STATUS_VALUES,
  type Thread,
  type ThreadCreateCommand,
  type ThreadExternalRefs,
  type ThreadId,
  type ThreadSpecEditCommand,
  type ThreadStatus,
  type ThreadStatusChangeCommand,
  threadToJsonValue,
  validateThread,
  validateThreadReceiptIndex,
} from "@wuphf/protocol";
import BetterSqlite3 from "better-sqlite3";

import {
  InvalidListCursorError,
  InvalidListLimitError,
  MAX_LIST_LIMIT,
  type ReceiptStore,
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
  parseThreadIdempotencyKey,
  type ThreadCommand,
} from "./idempotency.ts";
import {
  type ThreadStateRow,
  type ThreadStateStore,
  threadStateRowToThread,
} from "./projections.ts";

const MAX_THREAD_BODY_BYTES = 512 * 1_024;
const ISO_DATE_RE = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/;
const THREAD_STATUS_SET: ReadonlySet<string> = new Set<string>(THREAD_STATUS_VALUES);

export interface ThreadRouteDeps {
  readonly appender: ThreadAppender;
  readonly state: ThreadStateStore;
  readonly receiptStore: ReceiptStore;
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
      await handleThreadList(req, res, deps);
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
    await handleThreadGet(res, deps, parts[0] ?? "");
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

async function handleThreadList(
  req: IncomingMessage,
  res: ServerResponse,
  deps: ThreadRouteDeps,
): Promise<void> {
  const url = new URL(req.url ?? "/", "http://127.0.0.1");
  const status = parseStatusFilter(url.searchParams);
  if (!status.ok) {
    writeJson(res, 400, { error: "invalid_status", reason: status.reason });
    return;
  }
  const rows = deps.state.list(status.status === undefined ? undefined : { status: status.status });
  try {
    const threads: ThreadResponse[] = [];
    for (const row of rows) {
      threads.push(await threadResponseFromRow(row, deps.receiptStore));
    }
    writeJson(res, 200, { threads });
  } catch (err) {
    if (writeStorageErrorResponse(res, err, deps.logger, "thread_list_rejected")) return;
    throw err;
  }
}

async function handleThreadGet(
  res: ServerResponse,
  deps: ThreadRouteDeps,
  encodedThreadId: string,
): Promise<void> {
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
    writeJson(res, 200, await threadResponseFromRow(row, deps.receiptStore));
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
  const idem = parseThreadIdempotencyKey(
    headerString(req.headers["idempotency-key"]),
    "thread.create",
  );
  if (!idem.ok) {
    writeIdempotencyError(res, idem.error, deps.logger, "thread_create");
    return;
  }
  if (!ensureJsonContentType(req, res)) return;

  let parsed: unknown;
  try {
    parsed = JSON.parse(await readBody(req, res, MAX_THREAD_BODY_BYTES)) as unknown;
  } catch (err) {
    if (!res.writableEnded) {
      writeJson(res, 400, { error: "invalid_json", reason: errorMessage(err) });
    }
    return;
  }

  let command: ThreadCreateCommand;
  try {
    command = threadCreateCommandFromRequest(parsed, idem.key.ulid);
  } catch (err) {
    writeJson(res, 400, { error: "invalid_thread_command", reason: errorMessage(err) });
    return;
  }

  try {
    const result = deps.appender.appendCreateIdempotent({
      command,
      idempotency: idem.key,
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
  const idem = parseThreadIdempotencyKey(
    headerString(req.headers["idempotency-key"]),
    "thread.spec.edit",
  );
  if (!idem.ok) {
    writeIdempotencyError(res, idem.error, deps.logger, "thread_spec_edit");
    return;
  }
  if (!ensureJsonContentType(req, res)) return;

  let parsed: unknown;
  try {
    parsed = JSON.parse(await readBody(req, res, MAX_THREAD_BODY_BYTES)) as unknown;
  } catch (err) {
    if (!res.writableEnded) {
      writeJson(res, 400, { error: "invalid_json", reason: errorMessage(err) });
    }
    return;
  }

  let command: ThreadSpecEditCommand;
  let baseContentHash: Sha256Hex;
  try {
    const parsedCommand = threadSpecEditCommandFromRequest(parsed, threadId, idem.key.ulid);
    command = parsedCommand.command;
    baseContentHash = parsedCommand.baseContentHash;
  } catch (err) {
    writeJson(res, 400, { error: "invalid_thread_command", reason: errorMessage(err) });
    return;
  }

  try {
    const result = deps.appender.appendSpecEditIdempotent({
      command,
      baseContentHash,
      idempotency: idem.key,
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
  const idem = parseThreadIdempotencyKey(
    headerString(req.headers["idempotency-key"]),
    "thread.status.change",
  );
  if (!idem.ok) {
    writeIdempotencyError(res, idem.error, deps.logger, "thread_status_change");
    return;
  }
  if (!ensureJsonContentType(req, res)) return;

  let parsed: unknown;
  try {
    parsed = JSON.parse(await readBody(req, res, MAX_THREAD_BODY_BYTES)) as unknown;
  } catch (err) {
    if (!res.writableEnded) {
      writeJson(res, 400, { error: "invalid_json", reason: errorMessage(err) });
    }
    return;
  }

  let command: ThreadStatusChangeCommand;
  try {
    command = threadStatusChangeCommandFromRequest(parsed, threadId, idem.key.ulid);
  } catch (err) {
    writeJson(res, 400, { error: "invalid_thread_command", reason: errorMessage(err) });
    return;
  }

  try {
    const result = deps.appender.appendStatusChangeIdempotent({
      command,
      idempotency: idem.key,
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
}) => {
  readonly statusCode: number;
  readonly payload: Buffer;
} {
  return (applied) => ({
    statusCode,
    payload: Buffer.from(
      JSON.stringify({ threadId: applied.threadId, headLsn: applied.headLsn }),
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

interface ThreadResponse {
  readonly thread: Record<string, unknown>;
  readonly head_lsn: EventLsn;
  readonly receipt_ids: readonly ReceiptId[];
}

async function threadResponseFromRow(
  row: ThreadStateRow,
  receiptStore: ReceiptStore,
): Promise<ThreadResponse> {
  const derived = await deriveThreadReceiptRefs(receiptStore, row.id);
  const thread = threadStateRowToThread(row, derived.taskIds);
  assertThreadValid(thread, derived.receipts);
  return {
    thread: threadToJsonValue(thread),
    head_lsn: row.headLsn,
    receipt_ids: derived.receiptIds,
  };
}

async function deriveThreadReceiptRefs(
  receiptStore: ReceiptStore,
  threadId: ThreadId,
): Promise<{
  readonly receipts: readonly ReceiptSnapshot[];
  readonly receiptIds: readonly ReceiptId[];
  readonly taskIds: readonly TaskId[];
}> {
  const receipts: ReceiptSnapshot[] = [];
  const receiptIds: ReceiptId[] = [];
  const taskIds: TaskId[] = [];
  const seenTaskIds = new Set<string>();
  let cursor: string | undefined;
  for (;;) {
    const page = await receiptStore.list({
      threadId,
      limit: MAX_LIST_LIMIT,
      ...(cursor === undefined ? {} : { cursor }),
    });
    for (const receipt of page.items) {
      receipts.push(receipt);
      receiptIds.push(receipt.id);
      if (!seenTaskIds.has(receipt.taskId)) {
        seenTaskIds.add(receipt.taskId);
        taskIds.push(receipt.taskId);
      }
    }
    if (page.nextCursor === null) break;
    cursor = page.nextCursor;
  }
  return { receipts, receiptIds, taskIds };
}

function assertThreadValid(thread: Thread, receipts: readonly ReceiptSnapshot[]): void {
  const threadValidation = validateThread(thread);
  if (!threadValidation.ok) {
    throw new Error(
      `thread projection failed validation: ${threadValidation.errors
        .map((error) => `${error.path}: ${error.message}`)
        .join("; ")}`,
    );
  }
  const indexValidation = validateThreadReceiptIndex(thread, receipts);
  if (!indexValidation.ok) {
    throw new Error(
      `thread receipt index failed validation: ${indexValidation.errors
        .map((error) => `${error.path}: ${error.message}`)
        .join("; ")}`,
    );
  }
}

function threadCreateCommandFromRequest(
  value: unknown,
  idempotencyKey: string,
): ThreadCreateCommand {
  const record = requireRecord(value, "thread.create");
  assertKnownKeys(record, "thread.create", [
    "threadId",
    "title",
    "createdBy",
    "createdAt",
    "externalRefs",
    "content",
  ]);
  return {
    kind: "thread.create",
    idempotencyKey: asIdempotencyKey(idempotencyKey),
    threadId: asThreadId(requiredString(record, "threadId", "thread.create")),
    title: requiredString(record, "title", "thread.create"),
    createdBy: parseSignerIdentity(record, "createdBy", "thread.create"),
    createdAt: parseIsoDate(record, "createdAt", "thread.create"),
    externalRefs: parseThreadExternalRefs(requiredField(record, "externalRefs", "thread.create")),
    content: requiredField(record, "content", "thread.create") as JsonValue,
  };
}

function threadSpecEditCommandFromRequest(
  value: unknown,
  pathThreadId: ThreadId,
  idempotencyKey: string,
): { readonly command: ThreadSpecEditCommand; readonly baseContentHash: Sha256Hex } {
  const record = requireRecord(value, "thread.spec.edit");
  assertKnownKeys(record, "thread.spec.edit", [
    "threadId",
    "revisionId",
    "baseRevisionId",
    "baseContentHash",
    "content",
    "contentHash",
    "authoredBy",
    "authoredAt",
  ]);
  assertOptionalThreadIdMatchesPath(record, pathThreadId, "thread.spec.edit");
  const command: ThreadSpecEditCommand = {
    kind: "thread.spec.edit",
    idempotencyKey: asIdempotencyKey(idempotencyKey),
    threadId: pathThreadId,
    revisionId: asThreadSpecRevisionId(requiredString(record, "revisionId", "thread.spec.edit")),
    baseRevisionId: asThreadSpecRevisionId(
      requiredString(record, "baseRevisionId", "thread.spec.edit"),
    ),
    content: requiredField(record, "content", "thread.spec.edit") as JsonValue,
    contentHash: asSha256Hex(requiredString(record, "contentHash", "thread.spec.edit")),
    authoredBy: parseSignerIdentity(record, "authoredBy", "thread.spec.edit"),
    authoredAt: parseIsoDate(record, "authoredAt", "thread.spec.edit"),
  };
  return {
    command,
    baseContentHash: asSha256Hex(requiredString(record, "baseContentHash", "thread.spec.edit")),
  };
}

function threadStatusChangeCommandFromRequest(
  value: unknown,
  pathThreadId: ThreadId,
  idempotencyKey: string,
): ThreadStatusChangeCommand {
  const record = requireRecord(value, "thread.status.change");
  assertKnownKeys(record, "thread.status.change", [
    "threadId",
    "fromStatus",
    "toStatus",
    "changedBy",
    "changedAt",
  ]);
  assertOptionalThreadIdMatchesPath(record, pathThreadId, "thread.status.change");
  return {
    kind: "thread.status.change",
    idempotencyKey: asIdempotencyKey(idempotencyKey),
    threadId: pathThreadId,
    fromStatus: parseThreadStatus(record, "fromStatus", "thread.status.change"),
    toStatus: parseThreadStatus(record, "toStatus", "thread.status.change"),
    changedBy: parseSignerIdentity(record, "changedBy", "thread.status.change"),
    changedAt: parseIsoDate(record, "changedAt", "thread.status.change"),
  };
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

function assertOptionalThreadIdMatchesPath(
  record: Readonly<Record<string, unknown>>,
  pathThreadId: ThreadId,
  context: string,
): void {
  if (!Object.hasOwn(record, "threadId")) return;
  const bodyThreadId = asThreadId(requiredString(record, "threadId", context));
  if (bodyThreadId !== pathThreadId) {
    throw new Error(`${context}.threadId: must match path thread id`);
  }
}

function parseThreadExternalRefs(value: unknown): ThreadExternalRefs {
  const record = requireRecord(value, "externalRefs");
  assertKnownKeys(record, "externalRefs", ["sourceUrls", "entityIds"]);
  return {
    sourceUrls: requiredStringArray(record, "sourceUrls", "externalRefs"),
    entityIds: requiredStringArray(record, "entityIds", "externalRefs"),
  };
}

function parseSignerIdentity(
  record: Readonly<Record<string, unknown>>,
  key: string,
  context: string,
): SignerIdentity {
  return asSignerIdentity(requiredString(record, key, context));
}

function parseThreadStatus(
  record: Readonly<Record<string, unknown>>,
  key: string,
  context: string,
): ThreadStatus {
  const value = requiredString(record, key, context);
  if (!THREAD_STATUS_SET.has(value)) {
    throw new Error(`${context}.${key}: must be a valid thread status`);
  }
  return value as ThreadStatus;
}

function parseIsoDate(
  record: Readonly<Record<string, unknown>>,
  key: string,
  context: string,
): Date {
  const raw = requiredString(record, key, context);
  if (!ISO_DATE_RE.test(raw)) {
    throw new Error(`${context}.${key}: must be an ISO 8601 instant`);
  }
  const date = new Date(raw);
  if (Number.isNaN(date.getTime()) || date.toISOString() !== raw) {
    throw new Error(`${context}.${key}: must be a valid ISO 8601 instant`);
  }
  return date;
}

function requireRecord(value: unknown, context: string): Readonly<Record<string, unknown>> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error(`${context}: must be a JSON object`);
  }
  return value as Readonly<Record<string, unknown>>;
}

function assertKnownKeys(
  record: Readonly<Record<string, unknown>>,
  context: string,
  allowed: readonly string[],
): void {
  const allowedSet = new Set<string>(allowed);
  for (const key of Object.keys(record)) {
    if (!allowedSet.has(key)) {
      throw new Error(`${context}.${key}: is not allowed`);
    }
  }
}

function requiredField(
  record: Readonly<Record<string, unknown>>,
  key: string,
  context: string,
): unknown {
  if (!Object.hasOwn(record, key) || record[key] === undefined) {
    throw new Error(`${context}.${key}: is required`);
  }
  return record[key];
}

function requiredString(
  record: Readonly<Record<string, unknown>>,
  key: string,
  context: string,
): string {
  const value = requiredField(record, key, context);
  if (typeof value !== "string") {
    throw new Error(`${context}.${key}: must be a string`);
  }
  return value;
}

function requiredStringArray(
  record: Readonly<Record<string, unknown>>,
  key: string,
  context: string,
): readonly string[] {
  const value = requiredField(record, key, context);
  if (!Array.isArray(value)) {
    throw new Error(`${context}.${key}: must be an array`);
  }
  return value.map((item, index) => {
    if (typeof item !== "string") {
      throw new Error(`${context}.${key}.${index}: must be a string`);
    }
    return item;
  });
}

function writeThreadAppenderError(
  res: ServerResponse,
  err: unknown,
  logger: BrokerLogger,
  rejectedEvent: string,
): void {
  if (err instanceof ThreadCommandValidationError) {
    logger.warn(rejectedEvent, { reason: "invalid_command" });
    writeJson(res, 400, { error: "invalid_thread_command", reason: err.message });
    return;
  }
  if (err instanceof ThreadNotFoundError) {
    logger.warn(rejectedEvent, { reason: "thread_not_found" });
    notFound(res);
    return;
  }
  if (err instanceof ThreadConflictError) {
    logger.warn(rejectedEvent, { reason: err.code });
    writeJson(res, 409, { error: err.code });
    return;
  }
  if (err instanceof ThreadTerminalTransitionError) {
    logger.warn(rejectedEvent, { reason: "terminal_status_transition" });
    writeJson(res, 422, { error: "terminal_status_transition" });
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
    writeJson(res, 507, { error: "store_full" });
    return true;
  }
  if (err instanceof ReceiptStoreBusyError) {
    logger.warn(rejectedEvent, { reason: "store_busy" });
    writeJson(res, 503, { error: "store_busy" }, { "Retry-After": "1" });
    return true;
  }
  if (err instanceof ReceiptStoreUnavailableError) {
    logger.error(rejectedEvent, { reason: "storage_error" });
    writeJson(res, 503, { error: "storage_error" });
    return true;
  }
  if (err instanceof InvalidListCursorError || err instanceof InvalidListLimitError) {
    logger.warn(rejectedEvent, { reason: "receipt_index_invalid" });
    writeJson(res, 500, { error: "receipt_index_invalid" });
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
    writeJson(res, 507, { error: "store_full" });
    return true;
  }
  if (isSqliteBusyError(err)) {
    logger.warn(rejectedEvent, { reason: "store_busy" });
    writeJson(res, 503, { error: "store_busy" }, { "Retry-After": "1" });
    return true;
  }
  if (isSqliteUnavailableError(err)) {
    logger.error(rejectedEvent, { reason: "storage_error" });
    writeJson(res, 503, { error: "storage_error" });
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
    writeJson(res, 400, { error: "idempotency_key_required" });
    return;
  }
  writeJson(res, 400, { error: "idempotency_key_invalid", reason: err });
}

function ensureJsonContentType(req: IncomingMessage, res: ServerResponse): boolean {
  const contentType = req.headers["content-type"];
  if (typeof contentType !== "string" || !isJsonMediaType(contentType)) {
    writeJson(res, 415, { error: "unsupported_media_type" });
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
          res.writeHead(413, { "Content-Type": "text/plain", Connection: "close" });
          res.end("body_too_large");
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

function headerString(value: string | string[] | undefined): string | undefined {
  if (typeof value === "string") return value;
  if (Array.isArray(value) && typeof value[0] === "string") return value[0];
  return undefined;
}

function writeJson(
  res: ServerResponse,
  status: number,
  body: unknown,
  extraHeaders: Record<string, string> = {},
): void {
  const text = JSON.stringify(body);
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

function notFound(res: ServerResponse): void {
  writeJson(res, 404, { error: "not_found" });
}

function methodNotAllowed(res: ServerResponse, allow: string): void {
  res.writeHead(405, {
    Allow: allow,
    "Content-Type": "application/json; charset=utf-8",
    "Cache-Control": "no-store",
  });
  res.end(JSON.stringify({ error: "method_not_allowed" }));
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

export type { ThreadCommand };
