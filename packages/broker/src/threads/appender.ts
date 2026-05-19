import {
  asThreadSpecRevisionId,
  type EventLsn,
  lsnFromV1Number,
  type Sha256Hex,
  type ThreadCreateCommand,
  type ThreadCreatedAuditPayload,
  type ThreadId,
  type ThreadSpecEditCommand,
  type ThreadSpecEditedAuditPayload,
  type ThreadStatus,
  type ThreadStatusChangeCommand,
  type ThreadStatusChangedAuditPayload,
  type ThreadValidationError,
  threadAuditPayloadFromJsonValue,
  threadAuditPayloadToBytes,
  threadSpecContentHash,
  validateThreadCommand,
  validateThreadForeignKeys,
  validateThreadSpecRevisionChain,
  validateThreadStatusFold,
} from "@wuphf/protocol";
import type Database from "better-sqlite3";

import type { EventLog, EventLogRecord } from "../event-log/index.ts";
import type { ParsedIdempotencyKey } from "./idempotency.ts";
import type { ThreadStateStore } from "./projections.ts";

export interface AppliedThreadCommand {
  readonly threadId: ThreadId;
  readonly headLsn: EventLsn;
  readonly streamKind: "thread.created" | "thread.updated";
}

export interface IdempotentThreadAppendResult {
  readonly replayed: boolean;
  readonly statusCode: number;
  readonly payload: Buffer;
  readonly applied: AppliedThreadCommand | null;
}

export interface ThreadCommandRenderResult {
  readonly statusCode: number;
  readonly payload: Buffer;
}

export interface ThreadCreateIdempotentArgs {
  readonly command: ThreadCreateCommand;
  readonly idempotency: ParsedIdempotencyKey;
  readonly nowMs: number;
  readonly render: (applied: AppliedThreadCommand) => ThreadCommandRenderResult;
}

export interface ThreadSpecEditIdempotentArgs {
  readonly command: ThreadSpecEditCommand;
  readonly baseContentHash: Sha256Hex;
  readonly idempotency: ParsedIdempotencyKey;
  readonly nowMs: number;
  readonly render: (applied: AppliedThreadCommand) => ThreadCommandRenderResult;
}

export interface ThreadStatusChangeIdempotentArgs {
  readonly command: ThreadStatusChangeCommand;
  readonly idempotency: ParsedIdempotencyKey;
  readonly nowMs: number;
  readonly render: (applied: AppliedThreadCommand) => ThreadCommandRenderResult;
}

export interface ThreadAppender {
  appendCreateIdempotent(args: ThreadCreateIdempotentArgs): IdempotentThreadAppendResult;
  appendSpecEditIdempotent(args: ThreadSpecEditIdempotentArgs): IdempotentThreadAppendResult;
  appendStatusChangeIdempotent(
    args: ThreadStatusChangeIdempotentArgs,
  ): IdempotentThreadAppendResult;
}

export class ThreadCommandValidationError extends Error {
  override readonly name = "ThreadCommandValidationError";
  constructor(readonly errors: readonly ThreadValidationError[]) {
    super(formatValidationErrors(errors));
  }
}

export class ThreadNotFoundError extends Error {
  override readonly name = "ThreadNotFoundError";
}

export class ThreadConflictError extends Error {
  override readonly name = "ThreadConflictError";
  constructor(readonly code: "thread_exists" | "stale_spec_base" | "status_mismatch") {
    super(code);
  }
}

export class ThreadTerminalTransitionError extends Error {
  override readonly name = "ThreadTerminalTransitionError";
  constructor() {
    super("cannot transition out of terminal thread status");
  }
}

interface IdempotencyRow {
  readonly statusCode: number;
  readonly responsePayload: Buffer;
}

interface ThreadEventLogRow {
  readonly lsn: number;
  readonly tsMs: number;
  readonly type: "thread.created" | "thread.spec_edited" | "thread.status_changed";
  readonly payload: Buffer;
}

interface FoldedThreadLog {
  readonly existingThreadIds: ReadonlySet<ThreadId>;
  readonly created: ThreadCreatedAuditPayload | null;
  readonly specEdits: readonly ThreadSpecEditedAuditPayload[];
  readonly statusChanges: readonly ThreadStatusChangedAuditPayload[];
  readonly currentSpec: ThreadSpecEditedAuditPayload | null;
  readonly currentStatus: ThreadStatus | null;
}

export function createThreadAppender(
  db: Database.Database,
  eventLog: EventLog,
  threadState: ThreadStateStore,
): ThreadAppender {
  const idempotencyLookupStmt = db.prepare<[string, string], IdempotencyRow>(
    `SELECT status_code AS statusCode, response_payload AS responsePayload
     FROM command_idempotency
     WHERE idempotency_key = ? AND command = ?`,
  );
  const idempotencyInsertStmt = db.prepare<[string, string, number, Buffer, number | null, number]>(
    `INSERT INTO command_idempotency
       (idempotency_key, command, status_code, response_payload, created_at_lsn, created_at_ms)
     VALUES (?, ?, ?, ?, ?, ?)`,
  );
  const threadEventsStmt = db.prepare<[], ThreadEventLogRow>(
    `SELECT lsn, ts_ms AS tsMs, type, payload
     FROM event_log
     WHERE type IN ('thread.created', 'thread.spec_edited', 'thread.status_changed')
     ORDER BY lsn ASC`,
  );

  const foldThreadLog = (threadId: ThreadId): FoldedThreadLog => {
    const existingThreadIds = new Set<ThreadId>();
    let created: ThreadCreatedAuditPayload | null = null;
    const specEdits: ThreadSpecEditedAuditPayload[] = [];
    const statusChanges: ThreadStatusChangedAuditPayload[] = [];
    let currentSpec: ThreadSpecEditedAuditPayload | null = null;
    let currentStatus: ThreadStatus | null = null;

    for (const row of threadEventsStmt.all()) {
      const parsed = JSON.parse(row.payload.toString("utf8")) as unknown;
      if (row.type === "thread.created") {
        const payload = parsedThreadCreatedPayload(parsed);
        existingThreadIds.add(payload.threadId);
        if (payload.threadId === threadId) {
          created = payload;
          currentStatus = "open";
        }
        continue;
      }
      if (row.type === "thread.spec_edited") {
        const payload = parsedThreadSpecEditedPayload(parsed);
        if (payload.threadId === threadId) {
          specEdits.push(payload);
          currentSpec = payload;
        }
        continue;
      }
      const payload = parsedThreadStatusChangedPayload(parsed);
      if (payload.threadId === threadId) {
        statusChanges.push(payload);
        currentStatus = payload.toStatus;
      }
    }

    return {
      existingThreadIds,
      created,
      specEdits,
      statusChanges,
      currentSpec,
      currentStatus,
    };
  };

  const appendCreateInner = (args: ThreadCreateIdempotentArgs): IdempotentThreadAppendResult => {
    const cached = idempotencyLookupStmt.get(args.idempotency.raw, args.idempotency.command);
    if (cached !== undefined) {
      return {
        replayed: true,
        statusCode: cached.statusCode,
        payload: Buffer.from(cached.responsePayload),
        applied: null,
      };
    }
    assertIdempotencyMatches(args.command.idempotencyKey, args.idempotency);
    assertThreadCommandValid(args.command);

    const folded = foldThreadLog(args.command.threadId);
    if (folded.existingThreadIds.has(args.command.threadId)) {
      throw new ThreadConflictError("thread_exists");
    }

    const createdPayload: ThreadCreatedAuditPayload = {
      threadId: args.command.threadId,
      title: args.command.title,
      createdBy: args.command.createdBy,
      createdAt: args.command.createdAt,
      externalRefs: args.command.externalRefs,
    };
    const initialSpecPayload: ThreadSpecEditedAuditPayload = {
      threadId: args.command.threadId,
      // ThreadCreateCommand has no revisionId field; the idempotency-key ULID
      // gives the initial spec a stable, retry-safe revision id.
      revisionId: asThreadSpecRevisionId(args.idempotency.ulid),
      content: args.command.content,
      contentHash: threadSpecContentHash(args.command.content),
      authoredBy: args.command.createdBy,
      authoredAt: args.command.createdAt,
    };
    assertValidationOk(validateThreadSpecRevisionChain([initialSpecPayload]));
    assertValidationOk(
      validateThreadStatusFold([{ kind: "thread_created", threadId: args.command.threadId }]),
    );

    const createdLsn = appendThreadEvent("thread.created", "thread_created", createdPayload);
    threadState.applyEvent(toEventLogRecord(createdLsn, "thread.created", createdPayload));
    const specLsn = appendThreadEvent(
      "thread.spec_edited",
      "thread_spec_edited",
      initialSpecPayload,
    );
    threadState.applyEvent(toEventLogRecord(specLsn, "thread.spec_edited", initialSpecPayload));

    const applied: AppliedThreadCommand = {
      threadId: args.command.threadId,
      headLsn: lsnFromV1Number(specLsn),
      streamKind: "thread.created",
    };
    const rendered = args.render(applied);
    idempotencyInsertStmt.run(
      args.idempotency.raw,
      args.idempotency.command,
      rendered.statusCode,
      rendered.payload,
      specLsn,
      args.nowMs,
    );
    return { replayed: false, statusCode: rendered.statusCode, payload: rendered.payload, applied };
  };

  const appendSpecEditInner = (
    args: ThreadSpecEditIdempotentArgs,
  ): IdempotentThreadAppendResult => {
    const cached = idempotencyLookupStmt.get(args.idempotency.raw, args.idempotency.command);
    if (cached !== undefined) {
      return {
        replayed: true,
        statusCode: cached.statusCode,
        payload: Buffer.from(cached.responsePayload),
        applied: null,
      };
    }
    assertIdempotencyMatches(args.command.idempotencyKey, args.idempotency);
    assertThreadCommandValid(args.command);
    if (args.command.baseRevisionId === undefined) {
      throw new ThreadCommandValidationError([
        { path: "/baseRevisionId", message: "is required for spec edits" },
      ]);
    }
    const baseRevisionId = args.command.baseRevisionId;

    const folded = foldThreadLog(args.command.threadId);
    if (folded.created === null || !folded.existingThreadIds.has(args.command.threadId)) {
      throw new ThreadNotFoundError(`thread ${args.command.threadId} not found`);
    }
    if (folded.currentSpec === null) {
      throw new ThreadConflictError("stale_spec_base");
    }
    if (
      folded.currentSpec.revisionId !== baseRevisionId ||
      folded.currentSpec.contentHash !== args.baseContentHash
    ) {
      throw new ThreadConflictError("stale_spec_base");
    }

    const payload: ThreadSpecEditedAuditPayload = {
      threadId: args.command.threadId,
      revisionId: args.command.revisionId,
      baseRevisionId,
      content: args.command.content,
      contentHash: args.command.contentHash,
      authoredBy: args.command.authoredBy,
      authoredAt: args.command.authoredAt,
    };
    assertValidationOk(
      validateThreadForeignKeys({
        existingThreadIds: folded.existingThreadIds,
        specEdits: [payload],
        statusChanges: [],
        receipts: [],
      }),
    );
    assertValidationOk(validateThreadSpecRevisionChain([...folded.specEdits, payload]));

    const lsn = appendThreadEvent("thread.spec_edited", "thread_spec_edited", payload);
    threadState.applyEvent(toEventLogRecord(lsn, "thread.spec_edited", payload));
    const applied: AppliedThreadCommand = {
      threadId: args.command.threadId,
      headLsn: lsnFromV1Number(lsn),
      streamKind: "thread.updated",
    };
    const rendered = args.render(applied);
    idempotencyInsertStmt.run(
      args.idempotency.raw,
      args.idempotency.command,
      rendered.statusCode,
      rendered.payload,
      lsn,
      args.nowMs,
    );
    return { replayed: false, statusCode: rendered.statusCode, payload: rendered.payload, applied };
  };

  const appendStatusChangeInner = (
    args: ThreadStatusChangeIdempotentArgs,
  ): IdempotentThreadAppendResult => {
    const cached = idempotencyLookupStmt.get(args.idempotency.raw, args.idempotency.command);
    if (cached !== undefined) {
      return {
        replayed: true,
        statusCode: cached.statusCode,
        payload: Buffer.from(cached.responsePayload),
        applied: null,
      };
    }
    assertIdempotencyMatches(args.command.idempotencyKey, args.idempotency);
    if (isTerminalStatus(args.command.fromStatus)) {
      throw new ThreadTerminalTransitionError();
    }
    assertThreadCommandValid(args.command);

    const folded = foldThreadLog(args.command.threadId);
    if (folded.created === null || !folded.existingThreadIds.has(args.command.threadId)) {
      throw new ThreadNotFoundError(`thread ${args.command.threadId} not found`);
    }
    if (folded.currentStatus === null) {
      throw new ThreadConflictError("status_mismatch");
    }
    if (isTerminalStatus(folded.currentStatus)) {
      throw new ThreadTerminalTransitionError();
    }
    if (folded.currentStatus !== args.command.fromStatus) {
      throw new ThreadConflictError("status_mismatch");
    }

    const payload: ThreadStatusChangedAuditPayload = {
      threadId: args.command.threadId,
      fromStatus: args.command.fromStatus,
      toStatus: args.command.toStatus,
      changedBy: args.command.changedBy,
      changedAt: args.command.changedAt,
    };
    assertValidationOk(
      validateThreadForeignKeys({
        existingThreadIds: folded.existingThreadIds,
        specEdits: [],
        statusChanges: [payload],
        receipts: [],
      }),
    );
    assertValidationOk(
      validateThreadStatusFold([
        { kind: "thread_created", threadId: args.command.threadId },
        ...folded.statusChanges.map((event) => ({
          kind: "thread_status_changed" as const,
          ...event,
        })),
        { kind: "thread_status_changed", ...payload },
      ]),
    );

    const lsn = appendThreadEvent("thread.status_changed", "thread_status_changed", payload);
    threadState.applyEvent(toEventLogRecord(lsn, "thread.status_changed", payload));
    const applied: AppliedThreadCommand = {
      threadId: args.command.threadId,
      headLsn: lsnFromV1Number(lsn),
      streamKind: "thread.updated",
    };
    const rendered = args.render(applied);
    idempotencyInsertStmt.run(
      args.idempotency.raw,
      args.idempotency.command,
      rendered.statusCode,
      rendered.payload,
      lsn,
      args.nowMs,
    );
    return { replayed: false, statusCode: rendered.statusCode, payload: rendered.payload, applied };
  };

  const appendCreateTransaction = db.transaction(appendCreateInner);
  const appendSpecEditTransaction = db.transaction(appendSpecEditInner);
  const appendStatusChangeTransaction = db.transaction(appendStatusChangeInner);

  const appendThreadEvent = (
    type: "thread.created" | "thread.spec_edited" | "thread.status_changed",
    kind: "thread_created" | "thread_spec_edited" | "thread_status_changed",
    payload:
      | ThreadCreatedAuditPayload
      | ThreadSpecEditedAuditPayload
      | ThreadStatusChangedAuditPayload,
  ): number => {
    const bytes = threadAuditPayloadToBytes(kind, payload);
    return eventLog.append({ type, payload: Buffer.from(bytes) });
  };

  const toEventLogRecord = (
    lsn: number,
    type: "thread.created" | "thread.spec_edited" | "thread.status_changed",
    payload:
      | ThreadCreatedAuditPayload
      | ThreadSpecEditedAuditPayload
      | ThreadStatusChangedAuditPayload,
  ): EventLogRecord => ({
    lsn,
    tsMs: 0,
    type,
    payload: Buffer.from(threadAuditPayloadToBytes(auditKindForEventType(type), payload)),
  });

  return {
    appendCreateIdempotent(args: ThreadCreateIdempotentArgs): IdempotentThreadAppendResult {
      return appendCreateTransaction.immediate(args);
    },
    appendSpecEditIdempotent(args: ThreadSpecEditIdempotentArgs): IdempotentThreadAppendResult {
      return appendSpecEditTransaction.immediate(args);
    },
    appendStatusChangeIdempotent(
      args: ThreadStatusChangeIdempotentArgs,
    ): IdempotentThreadAppendResult {
      return appendStatusChangeTransaction.immediate(args);
    },
  };
}

function assertThreadCommandValid(
  command: ThreadCreateCommand | ThreadSpecEditCommand | ThreadStatusChangeCommand,
): void {
  assertValidationOk(validateThreadCommand(command));
}

function assertValidationOk(
  result:
    | { readonly ok: true }
    | { readonly ok: false; readonly errors: readonly ThreadValidationError[] },
): void {
  if (!result.ok) {
    throw new ThreadCommandValidationError(result.errors);
  }
}

function assertIdempotencyMatches(commandKey: string, idempotency: ParsedIdempotencyKey): void {
  if (commandKey !== idempotency.ulid) {
    throw new ThreadCommandValidationError([
      { path: "/idempotencyKey", message: "must match Idempotency-Key ULID suffix" },
    ]);
  }
}

function formatValidationErrors(errors: readonly ThreadValidationError[]): string {
  return errors.map((error) => `${error.path}: ${error.message}`).join("; ");
}

function isTerminalStatus(status: ThreadStatus): boolean {
  return status === "merged" || status === "closed";
}

function auditKindForEventType(
  type: "thread.created" | "thread.spec_edited" | "thread.status_changed",
): "thread_created" | "thread_spec_edited" | "thread_status_changed" {
  if (type === "thread.created") return "thread_created";
  if (type === "thread.spec_edited") return "thread_spec_edited";
  return "thread_status_changed";
}

function parsedThreadCreatedPayload(value: unknown): ThreadCreatedAuditPayload {
  return threadAuditPayloadFromJsonValue("thread_created", value) as ThreadCreatedAuditPayload;
}

function parsedThreadSpecEditedPayload(value: unknown): ThreadSpecEditedAuditPayload {
  return threadAuditPayloadFromJsonValue(
    "thread_spec_edited",
    value,
  ) as ThreadSpecEditedAuditPayload;
}

function parsedThreadStatusChangedPayload(value: unknown): ThreadStatusChangedAuditPayload {
  return threadAuditPayloadFromJsonValue(
    "thread_status_changed",
    value,
  ) as ThreadStatusChangedAuditPayload;
}
