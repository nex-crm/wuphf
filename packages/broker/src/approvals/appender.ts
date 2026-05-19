// Approval appender.
//
// Each command folds the approval's current state from event_log, validates
// the command against that fold, appends the audit event, updates the
// projection, and stores the idempotency replay row in one BEGIN IMMEDIATE
// transaction.

import {
  type ApprovalDecidedAuditPayload,
  type ApprovalRequest,
  type ApprovalRequestedAuditPayload,
  type ApprovalRequestId,
  approvalAuditPayloadToBytes,
  type EventLsn,
  lsnFromV1Number,
  parseLsn,
} from "@wuphf/protocol";
import type Database from "better-sqlite3";

import type { EventLog } from "../event-log/index.ts";
import type { ParsedApprovalIdempotencyKey } from "./idempotency.ts";
import {
  type ApprovalProjection,
  approvalWithDecision,
  type FoldedApprovalRow,
  foldApprovalFromLog,
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
  readonly nowMs: number;
  readonly render: (applied: ApprovalAppendResult) => {
    readonly statusCode: number;
    readonly payload: Buffer;
  };
}

export interface IdempotentApprovalDecisionArgs {
  readonly payload: ApprovalDecidedAuditPayload;
  readonly idempotency: ParsedApprovalIdempotencyKey;
  readonly nowMs: number;
  readonly render: (applied: ApprovalAppendResult) => {
    readonly statusCode: number;
    readonly payload: Buffer;
  };
}

export interface ApprovalAppender {
  requestApproval(payload: ApprovalRequestedAuditPayload): ApprovalAppendResult;
  decideApproval(payload: ApprovalDecidedAuditPayload): ApprovalAppendResult;
  requestApprovalIdempotent(args: IdempotentApprovalRequestArgs): IdempotentApprovalAppendResult;
  decideApprovalIdempotent(args: IdempotentApprovalDecisionArgs): IdempotentApprovalAppendResult;
  pruneIdempotencyOlderThan(cutoffMs: number): number;
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

interface IdempotencyRow {
  readonly statusCode: number;
  readonly responsePayload: Buffer;
  readonly createdAtLsn: number | null;
}

export function createApprovalAppender(
  db: Database.Database,
  eventLog: EventLog,
  projection: ApprovalProjection,
): ApprovalAppender {
  const idempotencyLookupStmt = db.prepare<[string, string], IdempotencyRow>(
    `SELECT status_code AS statusCode, response_payload AS responsePayload,
            created_at_lsn AS createdAtLsn
     FROM command_idempotency
     WHERE idempotency_key = ? AND command = ?`,
  );
  const idempotencyInsertStmt = db.prepare<[string, string, number, Buffer, number | null, number]>(
    `INSERT INTO command_idempotency
       (idempotency_key, command, status_code, response_payload, created_at_lsn, created_at_ms)
     VALUES (?, ?, ?, ?, ?, ?)`,
  );
  const pruneIdempotencyStmt = db.prepare<[number]>(
    `DELETE FROM command_idempotency WHERE created_at_ms < ?`,
  );

  const requestApprovalInner = (payload: ApprovalRequestedAuditPayload): ApprovalAppendResult => {
    const existing = foldApprovalFromLog(db, payload.requestId);
    if (existing !== null) {
      throw new ApprovalRequestAlreadyExistsError(
        `approval request already exists: ${payload.requestId}`,
      );
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
    const current = foldApprovalFromLog(db, payload.requestId);
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
      const cached = idempotencyLookupStmt.get(args.idempotency.raw, args.idempotency.command);
      if (cached !== undefined) {
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
      const cached = idempotencyLookupStmt.get(args.idempotency.raw, args.idempotency.command);
      if (cached !== undefined) {
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
