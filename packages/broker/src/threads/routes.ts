import type { IncomingMessage, ServerResponse } from "node:http";

import {
  type ApprovalRequest,
  asIdempotencyKey,
  asSignerIdentity,
  asThreadId,
  asThreadSpecRevisionId,
  canonicalJSON,
  type EventLsn,
  lsnFromV1Number,
  MAX_ROUTE_THREAD_LIST_ITEMS,
  parseLsn,
  routeErrorToJsonValue,
  type Sha256Hex,
  type SignerIdentity,
  THREAD_BOARD_COLUMN_VALUES,
  THREAD_EFFECTIVE_STATUS_VALUES,
  type Thread,
  type ThreadBoardColumn,
  type ThreadCreateCommand,
  type ThreadEffectiveStatus,
  type ThreadExternalRefs,
  type ThreadId,
  type ThreadSpecEditCommand,
  type ThreadStatusChangeCommand,
  type ThreadView,
  threadCreateRequestFromJson,
  threadCreateRequestToJsonValue,
  threadGetResponseToJsonValue,
  threadListResponseToJsonValue,
  threadMutationResponseToJsonValue,
  threadPinnedApprovalsResponseToJsonValue,
  threadSpecContentHash,
  threadSpecEditRequestFromJson,
  threadSpecEditRequestToJsonValue,
  threadStatusChangeRequestFromJson,
  threadStatusChangeRequestToJsonValue,
  validateThread,
} from "@wuphf/protocol";
import BetterSqlite3 from "better-sqlite3";
import { approvalViewFromApproval } from "../approvals/view.ts";
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
  ThreadIdempotencyConflictError,
  ThreadNotFoundError,
  ThreadTerminalTransitionError,
} from "./appender.ts";
import { deriveThreadEffectiveStatus } from "./effective-status.ts";
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
const DEFAULT_THREAD_LIST_LIMIT = MAX_ROUTE_THREAD_LIST_ITEMS;
const THREAD_EFFECTIVE_STATUS_SET: ReadonlySet<string> = new Set<string>(
  THREAD_EFFECTIVE_STATUS_VALUES,
);
const THREAD_BOARD_COLUMN_SET: ReadonlySet<string> = new Set<string>(THREAD_BOARD_COLUMN_VALUES);
const ROUTE_SIGNER: SignerIdentity = asSignerIdentity("broker");
const EMPTY_EXTERNAL_REFS: ThreadExternalRefs = Object.freeze({
  sourceUrls: Object.freeze([]),
  entityIds: Object.freeze([]),
});

export interface ThreadRouteDeps {
  readonly appender: ThreadAppender;
  readonly state: ThreadStateStore;
  readonly receiptIndex: ThreadReceiptIndexStore;
  readonly approvals: ThreadApprovalQuery | null;
  readonly logger: BrokerLogger;
  readonly nowMs: () => number;
  readonly emitThreadEvent: (event: ThreadRouteStreamEvent) => void;
}

export interface ThreadApprovalQuery {
  countPendingByThread(threadId: ThreadId): number;
  listPendingByThread(threadId: ThreadId): readonly ThreadApprovalQueryRow[];
  latestHeadLsnByThread(threadId: ThreadId): EventLsn | null;
  pendingByThreadSnapshot(threadId: ThreadId): ThreadApprovalQuerySnapshot;
}

export interface ThreadApprovalQueryRow {
  readonly approval: ApprovalRequest;
  readonly headLsn: EventLsn;
}

export interface ThreadApprovalQuerySnapshot {
  readonly rows: readonly ThreadApprovalQueryRow[];
  readonly headLsn: EventLsn | null;
}

export interface ThreadRouteStreamEvent {
  readonly kind: "thread.created" | "thread.updated" | "thread.pinned_approvals.changed";
  readonly threadId: ThreadId;
  readonly headLsn: EventLsn;
}

interface ThreadListViewPage {
  readonly threads: readonly ThreadView[];
  readonly nextCursor?: EventLsn;
}

interface ThreadListViewItem {
  readonly thread: ThreadView;
  readonly viewLsn: number;
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
    methodNotAllowed(res, "GET, HEAD, POST");
    return true;
  }

  if (!pathname.startsWith("/api/v1/threads/")) {
    return false;
  }
  const suffix = pathname.slice("/api/v1/threads/".length);
  const parts = suffix.split("/");
  if (parts.length === 2 && parts[1] === "pinned-approvals") {
    if (req.method !== "GET" && req.method !== "HEAD") {
      methodNotAllowed(res, "GET, HEAD");
      return true;
    }
    handleThreadPinnedApprovalsGet(res, deps, parts[0] ?? "");
    return true;
  }
  if (parts.length === 1) {
    if (req.method !== "GET" && req.method !== "HEAD") {
      methodNotAllowed(res, "GET, HEAD");
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
  const query = parseThreadListQuery(url.searchParams);
  if (!query.ok) {
    writeRouteError(res, 400, query.error, query.reason);
    return;
  }
  try {
    const page = listThreadViewPage(deps, {
      limit: query.limit,
      ...(query.filter === undefined ? {} : { filter: query.filter }),
      ...(query.afterViewLsn === undefined ? {} : { afterViewLsn: query.afterViewLsn }),
    });
    writeJsonValue(
      res,
      200,
      threadListResponseToJsonValue({ threads: page.threads, nextCursor: page.nextCursor }),
    );
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
      threadGetResponseToJsonValue({ thread: threadViewFromRow(row, deps) }),
    );
  } catch (err) {
    if (writeStorageErrorResponse(res, err, deps.logger, "thread_get_rejected")) return;
    throw err;
  }
}

function handleThreadPinnedApprovalsGet(
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
    const snapshot = deps.approvals?.pendingByThreadSnapshot(threadId) ?? {
      rows: [],
      headLsn: null,
    };
    writeJsonValue(
      res,
      200,
      threadPinnedApprovalsResponseToJsonValue({
        threadId,
        headLsn: maxHeadLsn(row.headLsn, snapshot.rows, snapshot.headLsn),
        approvals: snapshot.rows.map((approval) => approvalViewFromApproval(approval.approval)),
      }),
    );
  } catch (err) {
    if (writeStorageErrorResponse(res, err, deps.logger, "thread_pinned_approvals_rejected")) {
      return;
    }
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
  let requestFingerprint: string;
  try {
    const request = threadCreateRequestFromJson(parsed);
    const parsedIdempotency = parseThreadIdempotencyKey(request.idempotencyKey, "thread.create");
    if (!parsedIdempotency.ok) {
      writeIdempotencyError(res, parsedIdempotency.error, deps.logger, "thread_create");
      return;
    }
    idempotency = parsedIdempotency.key;
    const threadId = asThreadId(idempotency.ulid);
    const externalRefs = request.externalRefs ?? EMPTY_EXTERNAL_REFS;
    requestFingerprint = threadRouteRequestFingerprint(
      "thread.create",
      threadId,
      threadCreateRequestToJsonValue({ ...request, externalRefs }),
    );
    const now = routeDate(deps.nowMs());
    command = {
      kind: "thread.create",
      idempotencyKey: asIdempotencyKey(idempotency.ulid),
      threadId,
      title: request.title,
      createdBy: ROUTE_SIGNER,
      createdAt: now,
      externalRefs,
      content: request.specContent,
    };
  } catch (err) {
    writeRouteError(res, 422, "invalid_payload", errorMessage(err));
    return;
  }

  try {
    const result = deps.appender.appendCreateIdempotent({
      command,
      idempotency,
      requestFingerprint,
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
  let requestFingerprint: string;
  try {
    const request = threadSpecEditRequestFromJson(parsed);
    const parsedIdempotency = parseThreadIdempotencyKey(request.idempotencyKey, "thread.spec.edit");
    if (!parsedIdempotency.ok) {
      writeIdempotencyError(res, parsedIdempotency.error, deps.logger, "thread_spec_edit");
      return;
    }
    idempotency = parsedIdempotency.key;
    requestFingerprint = threadRouteRequestFingerprint(
      "thread.spec.edit",
      threadId,
      threadSpecEditRequestToJsonValue(request),
    );
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
    writeRouteError(res, 422, "invalid_payload", errorMessage(err));
    return;
  }

  try {
    const result = deps.appender.appendSpecEditIdempotent({
      command,
      baseContentHash,
      idempotency,
      requestFingerprint,
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
  let requestFingerprint: string;
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
    requestFingerprint = threadRouteRequestFingerprint(
      "thread.status.change",
      threadId,
      threadStatusChangeRequestToJsonValue(request),
    );
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
    writeRouteError(res, 422, "invalid_payload", errorMessage(err));
    return;
  }

  try {
    const result = deps.appender.appendStatusChangeIdempotent({
      command,
      idempotency,
      requestFingerprint,
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

function threadViewFromRow(row: ThreadStateRow, deps: ThreadRouteDeps): ThreadView {
  return threadViewItemFromRow(row, deps).thread;
}

function listThreadViewPage(
  deps: ThreadRouteDeps,
  args: {
    readonly limit: number;
    readonly filter?: ThreadStatusFilter;
    readonly afterViewLsn?: number;
  },
): ThreadListViewPage {
  if (!Number.isSafeInteger(args.limit) || args.limit < 1) {
    throw new Error("thread view page limit must be a positive safe integer");
  }
  const afterViewLsn = args.afterViewLsn ?? 0;
  const candidates = deps.state
    .list()
    .map((row) => threadViewItemFromRow(row, deps))
    .filter((item) => item.viewLsn > afterViewLsn)
    .filter((item) => threadMatchesStatusFilter(item.thread, args.filter))
    .sort((left, right) => left.viewLsn - right.viewLsn);
  const visibleItems = candidates.slice(0, args.limit);
  const last = visibleItems.at(-1);
  return {
    threads: visibleItems.map((item) => item.thread),
    ...(candidates.length > args.limit && last !== undefined
      ? { nextCursor: lsnFromV1Number(last.viewLsn) }
      : {}),
  };
}

function threadViewItemFromRow(row: ThreadStateRow, deps: ThreadRouteDeps): ThreadListViewItem {
  const refs = deps.receiptIndex.refsForThread(row.id);
  const thread = threadStateRowToThread(row, refs.taskIds);
  assertThreadValid(thread);
  const pendingApprovalCount = deps.approvals?.countPendingByThread(row.id) ?? 0;
  const latestReceipt = deps.receiptIndex.latestForThread(row.id);
  const approvalHeadLsn = deps.approvals?.latestHeadLsnByThread(row.id) ?? null;
  const viewLsn = Math.max(
    parseLsn(row.headLsn).localLsn,
    latestReceipt?.lsn ?? 0,
    approvalHeadLsn === null ? 0 : parseLsn(approvalHeadLsn).localLsn,
  );
  return {
    viewLsn,
    thread: {
      ...thread,
      ...deriveThreadEffectiveStatus({
        storedStatus: row.status,
        pendingApprovalCount,
        latestReceiptStatus: latestReceipt?.status,
      }),
      pendingApprovalCount,
    },
  };
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

function parseThreadListQuery(params: URLSearchParams):
  | {
      readonly ok: true;
      readonly filter?: ThreadStatusFilter;
      readonly limit: number;
      readonly afterViewLsn?: number;
    }
  | { readonly ok: false; readonly error: string; readonly reason?: string } {
  const status = parseStatusFilter(params);
  if (!status.ok) return status;

  const limitParam = singleQueryParam(params, "limit");
  if (limitParam === "multiple") {
    return { ok: false, error: "invalid_limit", reason: "limit may appear only once" };
  }
  let limit = DEFAULT_THREAD_LIST_LIMIT;
  if (limitParam !== undefined) {
    if (!/^\d+$/.test(limitParam)) {
      return { ok: false, error: "invalid_limit", reason: "limit must be a positive integer" };
    }
    const parsedLimit = Number.parseInt(limitParam, 10);
    if (!Number.isSafeInteger(parsedLimit) || parsedLimit < 1) {
      return { ok: false, error: "invalid_limit", reason: "limit must be a positive integer" };
    }
    limit = Math.min(parsedLimit, MAX_ROUTE_THREAD_LIST_ITEMS);
  }

  const cursor = singleQueryParam(params, "cursor");
  if (cursor === "multiple") {
    return { ok: false, error: "invalid_cursor", reason: "cursor may appear only once" };
  }
  if (cursor !== undefined) {
    try {
      return {
        ok: true,
        ...(status.filter === undefined ? {} : { filter: status.filter }),
        limit,
        afterViewLsn: parseLsn(cursor as EventLsn).localLsn,
      };
    } catch {
      return { ok: false, error: "invalid_cursor", reason: "cursor must be an EventLsn" };
    }
  }

  return { ok: true, ...(status.filter === undefined ? {} : { filter: status.filter }), limit };
}

function parseStatusFilter(
  params: URLSearchParams,
):
  | { readonly ok: true; readonly filter?: ThreadStatusFilter }
  | { readonly ok: false; readonly error: string; readonly reason: string } {
  const values = params.getAll("status");
  if (values.length === 0) return { ok: true };
  if (values.length > 1) {
    return { ok: false, error: "invalid_status", reason: "status may appear only once" };
  }
  const raw = values[0] ?? "";
  if (THREAD_EFFECTIVE_STATUS_SET.has(raw)) {
    return {
      ok: true,
      filter: { kind: "effective_status", value: raw as ThreadEffectiveStatus },
    };
  }
  if (THREAD_BOARD_COLUMN_SET.has(raw)) {
    return { ok: true, filter: { kind: "board_column", value: raw as ThreadBoardColumn } };
  }
  return {
    ok: false,
    error: "invalid_status",
    reason: "unknown thread status, effective status, or board column",
  };
}

function singleQueryParam(params: URLSearchParams, key: string): string | "multiple" | undefined {
  const values = params.getAll(key);
  if (values.length === 0) return undefined;
  if (values.length > 1) return "multiple";
  return values[0] ?? "";
}

type ThreadStatusFilter =
  | { readonly kind: "effective_status"; readonly value: ThreadEffectiveStatus }
  | { readonly kind: "board_column"; readonly value: ThreadBoardColumn };

function threadMatchesStatusFilter(thread: ThreadView, filter: ThreadStatusFilter | undefined) {
  if (filter === undefined) return true;
  if (filter.kind === "effective_status") return thread.effectiveStatus === filter.value;
  return thread.boardColumn === filter.value;
}

function maxHeadLsn(
  threadHeadLsn: EventLsn,
  approvals: readonly ThreadApprovalQueryRow[],
  approvalHeadLsn: EventLsn | null,
): EventLsn {
  let max = parseLsn(threadHeadLsn).localLsn;
  if (approvalHeadLsn !== null) {
    max = Math.max(max, parseLsn(approvalHeadLsn).localLsn);
  }
  for (const approval of approvals) {
    max = Math.max(max, parseLsn(approval.headLsn).localLsn);
  }
  return lsnFromV1Number(max);
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
    writeRouteError(res, 422, "invalid_payload", err.message);
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
  if (err instanceof ThreadIdempotencyConflictError) {
    logger.warn(rejectedEvent, { reason: "idempotency_key_conflict" });
    writeRouteError(res, 409, "idempotency_key_conflict");
    return;
  }
  if (isCommandIdempotencyConstraintError(err)) {
    logger.warn(rejectedEvent, { reason: "idempotency_key_conflict" });
    writeRouteError(res, 409, "idempotency_key_conflict");
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

function isCommandIdempotencyConstraintError(err: unknown): boolean {
  return (
    err instanceof BetterSqlite3.SqliteError &&
    (err.code === "SQLITE_CONSTRAINT_PRIMARYKEY" || err.code === "SQLITE_CONSTRAINT_UNIQUE") &&
    err.message.includes("command_idempotency.idempotency_key")
  );
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

function threadRouteRequestFingerprint(
  command: ThreadCommand,
  threadId: ThreadId,
  body: Readonly<Record<string, unknown>>,
): string {
  return canonicalJSON({ command, threadId, body });
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
