// Approval appender.
//
// Each command reads the approval's current state from the transactionally
// updated projection, validates against that fold, appends the audit event,
// updates the projection, and stores the idempotency replay row in one
// BEGIN IMMEDIATE transaction.

import {
  type ApprovalDecidedAuditPayload,
  type ApprovalRequest,
  type ApprovalRequestedAuditPayload,
  type ApprovalRequestId,
  type ApprovalTokenId,
  approvalAuditPayloadToBytes,
  approvalRequestToJson,
  type EventLsn,
  lsnFromV1Number,
  MAX_ROUTE_APPROVAL_LIST_ITEMS,
  parseLsn,
  type ThreadId,
} from "@wuphf/protocol";
import type Database from "better-sqlite3";

import type { EventLog } from "../event-log/index.ts";
import type { ParsedApprovalIdempotencyKey } from "./idempotency.ts";
import {
  type ApprovalProjection,
  approvalWithDecision,
  type FoldedApprovalRow,
} from "./projections.ts";

export interface ApprovalAppendResult {
  readonly lsn: EventLsn;
  readonly approval: ApprovalRequest;
}

export interface IdempotentApprovalAppendResult {
  readonly replayed: boolean;
  readonly statusCode: number;
  readonly payload: Buffer;
  readonly lsn: EventLsn | null;
  readonly approval: ApprovalRequest | null;
}

export interface IdempotentApprovalRequestArgs {
  readonly payload: ApprovalRequestedAuditPayload;
  readonly idempotency: ParsedApprovalIdempotencyKey;
  readonly requestFingerprint: string;
  readonly nowMs: number;
  readonly render: (applied: ApprovalAppendResult) => {
    readonly statusCode: number;
    readonly payload: Buffer;
  };
}

export interface IdempotentApprovalDecisionArgs {
  readonly payload: ApprovalDecidedAuditPayload;
  readonly idempotency: ParsedApprovalIdempotencyKey;
  readonly requestFingerprint: string;
  readonly nowMs: number;
  readonly render: (applied: ApprovalAppendResult) => {
    readonly statusCode: number;
    readonly payload: Buffer;
  };
}

export interface ApprovalAppender {
  sharesProvenance(db: Database.Database, eventLog: EventLog): boolean;
  requestApproval(payload: ApprovalRequestedAuditPayload): ApprovalAppendResult;
  decideApproval(payload: ApprovalDecidedAuditPayload): ApprovalAppendResult;
  requestApprovalIdempotent(args: IdempotentApprovalRequestArgs): IdempotentApprovalAppendResult;
  decideApprovalIdempotent(args: IdempotentApprovalDecisionArgs): IdempotentApprovalAppendResult;
  pruneIdempotencyOlderThan(cutoffMs: number): number;
}

export interface ApprovalAppenderOptions {
  readonly threadRefValidator?: (threadId: ThreadId) => boolean;
}

export class ApprovalRequestAlreadyExistsError extends Error {
  override readonly name = "ApprovalRequestAlreadyExistsError";
}

export class ApprovalRequestNotFoundError extends Error {
  override readonly name = "ApprovalRequestNotFoundError";
}

export class ApprovalRequestAlreadyDecidedError extends Error {
  override readonly name = "ApprovalRequestAlreadyDecidedError";
  constructor(readonly approvalId: ApprovalRequestId) {
    super(`approval request is not pending: ${approvalId}`);
  }
}

export class ApprovalPendingLimitExceededError extends Error {
  override readonly name = "ApprovalPendingLimitExceededError";
  constructor(readonly threadId: ThreadId) {
    super(`thread ${threadId} already has ${MAX_ROUTE_APPROVAL_LIST_ITEMS} pending approvals`);
  }
}

export class ApprovalThreadNotFoundError extends Error {
  override readonly name = "ApprovalThreadNotFoundError";
  constructor(readonly threadId: ThreadId) {
    super(`thread not found: ${threadId}`);
  }
}

export class ApprovalDecisionInvalidError extends Error {
  override readonly name = "ApprovalDecisionInvalidError";
}

export class ApprovalTokenIssuedToMismatchError extends Error {
  override readonly name = "ApprovalTokenIssuedToMismatchError";
  constructor(
    readonly issuedTo: string,
    readonly decidedBy: string,
  ) {
    super(`approval token issued to ${issuedTo}, not ${decidedBy}`);
  }
}

export class ApprovalTokenAlreadyUsedError extends Error {
  override readonly name = "ApprovalTokenAlreadyUsedError";
  constructor(readonly tokenId: ApprovalTokenId) {
    super(`approval token already used: ${tokenId}`);
  }
}

export class ApprovalIdempotencyConflictError extends Error {
  override readonly name = "ApprovalIdempotencyConflictError";
}

interface IdempotencyRow {
  readonly statusCode: number;
  readonly responsePayload: Buffer;
  readonly createdAtLsn: number | null;
  readonly requestFingerprint: string | null;
}

interface TokenUsageRow {
  readonly approvalId: string;
}

export function createApprovalAppender(
  db: Database.Database,
  eventLog: EventLog,
  projection: ApprovalProjection,
  options: ApprovalAppenderOptions = {},
): ApprovalAppender {
  const tokenUsageStmt = db.prepare<[string, string], TokenUsageRow>(
    `SELECT approval_id AS approvalId
     FROM pending_approvals
     WHERE token_id = ? AND approval_id != ?
     LIMIT 1`,
  );
  const idempotencyLookupStmt = db.prepare<[string, string], IdempotencyRow>(
    `SELECT status_code AS statusCode, response_payload AS responsePayload,
            created_at_lsn AS createdAtLsn, request_fingerprint AS requestFingerprint
     FROM command_idempotency
     WHERE idempotency_key = ? AND command = ?`,
  );
  const idempotencyInsertStmt = db.prepare<
    [string, string, number, Buffer, number | null, number, string]
  >(
    `INSERT INTO command_idempotency
       (idempotency_key, command, status_code, response_payload, created_at_lsn, created_at_ms,
        request_fingerprint)
     VALUES (?, ?, ?, ?, ?, ?, ?)`,
  );
  const pruneIdempotencyStmt = db.prepare<[number]>(
    `DELETE FROM command_idempotency
     WHERE created_at_ms < ?
       AND command IN ('approval.requested', 'approval.decided')`,
  );
  const threadRefValidator = options.threadRefValidator ?? null;

  const assertApprovalTokenUnused = (payload: ApprovalDecidedAuditPayload): void => {
    const suppliedApprovalToken = payload.decision === "approve" ? payload.token : undefined;
    if (suppliedApprovalToken === undefined) return;
    const existing = tokenUsageStmt.get(suppliedApprovalToken.tokenId, payload.requestId);
    if (existing !== undefined) {
      throw new ApprovalTokenAlreadyUsedError(suppliedApprovalToken.tokenId);
    }
  };

  const assertThreadReferenceExists = (payload: ApprovalRequestedAuditPayload): void => {
    if (payload.threadId === undefined) return;
    if (threadRefValidator === null || !threadRefValidator(payload.threadId)) {
      throw new ApprovalThreadNotFoundError(payload.threadId);
    }
  };

  const assertApprovalTokenIssuedToDecider = (payload: ApprovalDecidedAuditPayload): void => {
    const suppliedApprovalToken = payload.decision === "approve" ? payload.token : undefined;
    if (suppliedApprovalToken === undefined) return;
    if (String(suppliedApprovalToken.issuedTo) !== String(payload.decidedBy)) {
      throw new ApprovalTokenIssuedToMismatchError(
        String(suppliedApprovalToken.issuedTo),
        String(payload.decidedBy),
      );
    }
  };

  const requestApprovalInner = (payload: ApprovalRequestedAuditPayload): ApprovalAppendResult => {
    const existing = projection.getById(payload.requestId);
    if (existing !== null) {
      throw new ApprovalRequestAlreadyExistsError(
        `approval request already exists: ${payload.requestId}`,
      );
    }
    assertThreadReferenceExists(payload);
    // TODO(approval-overcap-repair): add repair or quarantine for upgraded DBs
    // already over this cap; pinned snapshots fail closed until then.
    if (
      payload.threadId !== undefined &&
      projection.countPendingByThread(payload.threadId) >= MAX_ROUTE_APPROVAL_LIST_ITEMS
    ) {
      throw new ApprovalPendingLimitExceededError(payload.threadId);
    }
    const bytes = approvalAuditPayloadToBytes("approval_requested", payload);
    const lsn = eventLog.append({ type: "approval.requested", payload: Buffer.from(bytes) });
    const applied = projection.applyEvent({
      lsn,
      type: "approval.requested",
      payload: Buffer.from(bytes),
    });
    if (applied === null) {
      throw new Error("approval.requested projection apply returned null");
    }
    return { lsn: lsnFromV1Number(lsn), approval: applied.approval };
  };

  const decideApprovalInner = (payload: ApprovalDecidedAuditPayload): ApprovalAppendResult => {
    const current = projection.getById(payload.requestId);
    if (current === null) {
      throw new ApprovalRequestNotFoundError(`approval request not found: ${payload.requestId}`);
    }
    if (current.approval.status !== "pending") {
      throw new ApprovalRequestAlreadyDecidedError(payload.requestId);
    }
    const folded = approvalWithDecision(current.approval, payload);
    if (folded.status === "pending") {
      throw new Error("approval decision did not produce a terminal status");
    }
    assertApprovalTokenUnused(payload);
    assertApprovalTokenIssuedToDecider(payload);
    try {
      approvalRequestToJson(folded);
    } catch (err) {
      throw new ApprovalDecisionInvalidError(
        err instanceof Error ? err.message : "invalid approval decision",
      );
    }
    // TODO(webauthn-module): verify SignedApprovalToken at this hook once the
    // WebAuthn approval verifier owns token trust decisions. This appender
    // records the token exactly as supplied.
    const bytes = approvalAuditPayloadToBytes("approval_decided", payload);
    const lsn = eventLog.append({ type: "approval.decided", payload: Buffer.from(bytes) });
    const applied = projection.applyEvent({
      lsn,
      type: "approval.decided",
      payload: Buffer.from(bytes),
    });
    if (applied === null) {
      throw new Error("approval.decided projection apply returned null");
    }
    return { lsn: lsnFromV1Number(lsn), approval: applied.approval };
  };

  const requestApprovalTransaction = db.transaction(requestApprovalInner);
  const decideApprovalTransaction = db.transaction(decideApprovalInner);

  const requestApprovalIdempotentTransaction = db.transaction(
    (args: IdempotentApprovalRequestArgs): IdempotentApprovalAppendResult => {
      assertRequestFingerprint(args.requestFingerprint);
      const cached = idempotencyLookupStmt.get(args.idempotency.raw, args.idempotency.command);
      if (cached !== undefined) {
        assertIdempotencyFingerprint(cached, args.requestFingerprint);
        return idempotentReplay(cached);
      }
      const applied = requestApprovalInner(args.payload);
      const rendered = args.render(applied);
      idempotencyInsertStmt.run(
        args.idempotency.raw,
        args.idempotency.command,
        rendered.statusCode,
        rendered.payload,
        parseLsn(applied.lsn).localLsn,
        args.nowMs,
        args.requestFingerprint,
      );
      return {
        replayed: false,
        statusCode: rendered.statusCode,
        payload: rendered.payload,
        lsn: applied.lsn,
        approval: applied.approval,
      };
    },
  );

  const decideApprovalIdempotentTransaction = db.transaction(
    (args: IdempotentApprovalDecisionArgs): IdempotentApprovalAppendResult => {
      assertRequestFingerprint(args.requestFingerprint);
      const cached = idempotencyLookupStmt.get(args.idempotency.raw, args.idempotency.command);
      if (cached !== undefined) {
        assertIdempotencyFingerprint(cached, args.requestFingerprint);
        return idempotentReplay(cached);
      }
      const applied = decideApprovalInner(args.payload);
      const rendered = args.render(applied);
      idempotencyInsertStmt.run(
        args.idempotency.raw,
        args.idempotency.command,
        rendered.statusCode,
        rendered.payload,
        parseLsn(applied.lsn).localLsn,
        args.nowMs,
        args.requestFingerprint,
      );
      return {
        replayed: false,
        statusCode: rendered.statusCode,
        payload: rendered.payload,
        lsn: applied.lsn,
        approval: applied.approval,
      };
    },
  );

  return {
    sharesProvenance(candidateDb: Database.Database, candidateEventLog: EventLog): boolean {
      return candidateDb === db && candidateEventLog === eventLog;
    },
    requestApproval(payload: ApprovalRequestedAuditPayload): ApprovalAppendResult {
      return requestApprovalTransaction.immediate(payload);
    },
    decideApproval(payload: ApprovalDecidedAuditPayload): ApprovalAppendResult {
      return decideApprovalTransaction.immediate(payload);
    },
    requestApprovalIdempotent(args: IdempotentApprovalRequestArgs): IdempotentApprovalAppendResult {
      return requestApprovalIdempotentTransaction.immediate(args);
    },
    decideApprovalIdempotent(args: IdempotentApprovalDecisionArgs): IdempotentApprovalAppendResult {
      return decideApprovalIdempotentTransaction.immediate(args);
    },
    pruneIdempotencyOlderThan(cutoffMs: number): number {
      if (!Number.isSafeInteger(cutoffMs)) {
        throw new Error("pruneIdempotencyOlderThan: cutoffMs must be a safe integer");
      }
      return pruneIdempotencyStmt.run(cutoffMs).changes;
    },
  };
}

function assertRequestFingerprint(requestFingerprint: string): void {
  if (requestFingerprint.length === 0) {
    throw new Error("approval idempotency request fingerprint must not be empty");
  }
}

function assertIdempotencyFingerprint(row: IdempotencyRow, requestFingerprint: string): void {
  if (row.requestFingerprint !== requestFingerprint) {
    throw new ApprovalIdempotencyConflictError("idempotency key reused for a different request");
  }
}

function idempotentReplay(row: IdempotencyRow): IdempotentApprovalAppendResult {
  return {
    replayed: true,
    statusCode: row.statusCode,
    payload: Buffer.from(row.responsePayload),
    lsn: row.createdAtLsn === null ? null : lsnFromV1Number(row.createdAtLsn),
    approval: null,
  };
}

export type { FoldedApprovalRow };
