import type { DatabaseSync } from "node:sqlite";
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
  type ThreadSpecRevisionId,
  type ThreadStatus,
  type ThreadStatusChangeCommand,
  type ThreadStatusChangedAuditPayload,
  type ThreadValidationError,
  threadAuditPayloadToBytes,
  threadSpecContentHash,
  validateThreadCommand,
  validateThreadSpecEditedAuditPayload,
  validateThreadSpecRevisionChain,
  validateThreadStatusChangedAuditPayload,
  validateThreadStatusFold,
} from "@wuphf/protocol";

import type { EventLog, EventLogRecord } from "../event-log/index.ts";
import { createTransaction } from "../internal/sqlite-transaction.ts";
import type { ParsedIdempotencyKey } from "./idempotency.ts";
import type { ThreadStateStore } from "./projections.ts";

export interface AppliedThreadCommand {
  readonly threadId: ThreadId;
  readonly headLsn: EventLsn;
  readonly revisionId: ThreadSpecRevisionId;
  readonly contentHash: Sha256Hex;
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
  readonly requestFingerprint: string;
  readonly nowMs: number;
  readonly render: (applied: AppliedThreadCommand) => ThreadCommandRenderResult;
}

export interface ThreadSpecEditIdempotentArgs {
  readonly command: ThreadSpecEditCommand;
  readonly baseContentHash: Sha256Hex;
  readonly idempotency: ParsedIdempotencyKey;
  readonly requestFingerprint: string;
  readonly nowMs: number;
  readonly render: (applied: AppliedThreadCommand) => ThreadCommandRenderResult;
}

export interface ThreadStatusChangeIdempotentArgs {
  readonly command: ThreadStatusChangeCommand;
  readonly idempotency: ParsedIdempotencyKey;
  readonly requestFingerprint: string;
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
  constructor(
    readonly code: "thread_exists" | "revision_exists" | "stale_spec_base" | "status_mismatch",
  ) {
    super(code);
  }
}

export class ThreadTerminalTransitionError extends Error {
  override readonly name = "ThreadTerminalTransitionError";
  constructor() {
    super("cannot transition out of terminal thread status");
  }
}

export class ThreadIdempotencyConflictError extends Error {
  override readonly name = "ThreadIdempotencyConflictError";
}

interface IdempotencyRow {
  readonly statusCode: number;
  readonly responsePayload: Uint8Array;
  readonly requestFingerprint: string | null;
}

export function createThreadAppender(
  db: DatabaseSync,
  eventLog: EventLog,
  threadState: ThreadStateStore,
): ThreadAppender {
  const idempotencyLookupStmt = db.prepare(
    `SELECT status_code AS statusCode, response_payload AS responsePayload,
            request_fingerprint AS requestFingerprint
     FROM command_idempotency
     WHERE idempotency_key = ? AND command = ?`,
  );
  const idempotencyInsertStmt = db.prepare(
    `INSERT INTO command_idempotency
       (idempotency_key, command, status_code, response_payload, created_at_lsn, created_at_ms,
        request_fingerprint)
     VALUES (?, ?, ?, ?, ?, ?, ?)`,
  );

  const appendCreateInner = (args: ThreadCreateIdempotentArgs): IdempotentThreadAppendResult => {
    assertRequestFingerprint(args.requestFingerprint);
    const cached = idempotencyLookupStmt.get(args.idempotency.raw, args.idempotency.command) as
      | IdempotencyRow
      | undefined;
    if (cached !== undefined) {
      assertIdempotencyFingerprint(cached, args.requestFingerprint);
      return idempotentReplay(cached);
    }
    assertIdempotencyMatches(args.command.idempotencyKey, args.idempotency);
    assertThreadCommandValid(args.command);

    if (threadState.getById(args.command.threadId) !== null) {
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
    if (threadState.hasSpecRevision(initialSpecPayload.revisionId)) {
      throw new ThreadConflictError("revision_exists");
    }
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
      revisionId: initialSpecPayload.revisionId,
      contentHash: initialSpecPayload.contentHash,
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
      args.requestFingerprint,
    );
    return { replayed: false, statusCode: rendered.statusCode, payload: rendered.payload, applied };
  };

  const appendSpecEditInner = (
    args: ThreadSpecEditIdempotentArgs,
  ): IdempotentThreadAppendResult => {
    assertRequestFingerprint(args.requestFingerprint);
    const cached = idempotencyLookupStmt.get(args.idempotency.raw, args.idempotency.command) as
      | IdempotencyRow
      | undefined;
    if (cached !== undefined) {
      assertIdempotencyFingerprint(cached, args.requestFingerprint);
      return idempotentReplay(cached);
    }
    assertIdempotencyMatches(args.command.idempotencyKey, args.idempotency);
    assertThreadCommandValid(args.command);
    if (args.command.baseRevisionId === undefined) {
      throw new ThreadCommandValidationError([
        { path: "/baseRevisionId", message: "is required for spec edits" },
      ]);
    }
    const baseRevisionId = args.command.baseRevisionId;

    const current = threadState.getById(args.command.threadId);
    if (current === null) {
      throw new ThreadNotFoundError(`thread ${args.command.threadId} not found`);
    }
    if (
      current.spec.revisionId !== baseRevisionId ||
      current.spec.contentHash !== args.baseContentHash
    ) {
      throw new ThreadConflictError("stale_spec_base");
    }
    if (threadState.hasSpecRevision(args.command.revisionId)) {
      throw new ThreadConflictError("revision_exists");
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
    assertValidationOk(validateThreadSpecEditedAuditPayload(payload));
    if (payload.baseRevisionId === payload.revisionId) {
      throw new ThreadCommandValidationError([
        { path: "/baseRevisionId", message: "must not equal revisionId" },
      ]);
    }

    const lsn = appendThreadEvent("thread.spec_edited", "thread_spec_edited", payload);
    threadState.applyEvent(toEventLogRecord(lsn, "thread.spec_edited", payload));
    const applied: AppliedThreadCommand = {
      threadId: args.command.threadId,
      headLsn: lsnFromV1Number(lsn),
      revisionId: payload.revisionId,
      contentHash: payload.contentHash,
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
      args.requestFingerprint,
    );
    return { replayed: false, statusCode: rendered.statusCode, payload: rendered.payload, applied };
  };

  const appendStatusChangeInner = (
    args: ThreadStatusChangeIdempotentArgs,
  ): IdempotentThreadAppendResult => {
    assertRequestFingerprint(args.requestFingerprint);
    const cached = idempotencyLookupStmt.get(args.idempotency.raw, args.idempotency.command) as
      | IdempotencyRow
      | undefined;
    if (cached !== undefined) {
      assertIdempotencyFingerprint(cached, args.requestFingerprint);
      return idempotentReplay(cached);
    }
    assertIdempotencyMatches(args.command.idempotencyKey, args.idempotency);
    const current = threadState.getById(args.command.threadId);
    if (current === null) {
      throw new ThreadNotFoundError(`thread ${args.command.threadId} not found`);
    }
    if (isTerminalStatus(current.status)) {
      throw new ThreadTerminalTransitionError();
    }
    assertThreadCommandValid(args.command);
    if (current.status !== args.command.fromStatus) {
      throw new ThreadConflictError("status_mismatch");
    }

    const payload: ThreadStatusChangedAuditPayload = {
      threadId: args.command.threadId,
      fromStatus: args.command.fromStatus,
      toStatus: args.command.toStatus,
      changedBy: args.command.changedBy,
      changedAt: args.command.changedAt,
    };
    assertValidationOk(validateThreadStatusChangedAuditPayload(payload));

    const lsn = appendThreadEvent("thread.status_changed", "thread_status_changed", payload);
    threadState.applyEvent(toEventLogRecord(lsn, "thread.status_changed", payload));
    const applied: AppliedThreadCommand = {
      threadId: args.command.threadId,
      headLsn: lsnFromV1Number(lsn),
      revisionId: current.spec.revisionId,
      contentHash: current.spec.contentHash,
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
      args.requestFingerprint,
    );
    return { replayed: false, statusCode: rendered.statusCode, payload: rendered.payload, applied };
  };

  const appendCreateTransaction = createTransaction(db, appendCreateInner);
  const appendSpecEditTransaction = createTransaction(db, appendSpecEditInner);
  const appendStatusChangeTransaction = createTransaction(db, appendStatusChangeInner);

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

function assertRequestFingerprint(requestFingerprint: string): void {
  if (requestFingerprint.length === 0) {
    throw new Error("thread idempotency request fingerprint must not be empty");
  }
}

function assertIdempotencyFingerprint(row: IdempotencyRow, requestFingerprint: string): void {
  if (row.requestFingerprint !== requestFingerprint) {
    throw new ThreadIdempotencyConflictError("idempotency key reused for a different request");
  }
}

function idempotentReplay(row: IdempotencyRow): IdempotentThreadAppendResult {
  return {
    replayed: true,
    statusCode: row.statusCode,
    payload: Buffer.from(row.responsePayload),
    applied: null,
  };
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
