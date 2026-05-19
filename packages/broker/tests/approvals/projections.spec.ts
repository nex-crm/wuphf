import {
  type ApprovalDecidedAuditPayload,
  type ApprovalRequestedAuditPayload,
  approvalAuditPayloadToBytes,
  approvalRequestToJsonValue,
  asAgentId,
  asApprovalClaimId,
  asApprovalRequestId,
  asApprovalRole,
  asApprovalTokenId,
  asReceiptId,
  asSha256Hex,
  asSignerIdentity,
  asTaskId,
  asThreadId,
  asTimestampMs,
  canonicalJSON,
  type SignedApprovalToken,
  signedApprovalTokenFromJson,
  signedApprovalTokenToJsonValue,
} from "@wuphf/protocol";
import { describe, expect, it } from "vitest";

import {
  ApprovalIdempotencyConflictError,
  ApprovalRequestAlreadyDecidedError,
  createApprovalAppender,
  createApprovalProjection,
  parseApprovalIdempotencyKey,
  rebuildApprovalsProjectionFromLog,
} from "../../src/approvals/index.ts";
import { createEventLog, openDatabase, runMigrations } from "../../src/event-log/index.ts";

const REQUEST_ID = asApprovalRequestId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
const SECOND_REQUEST_ID = asApprovalRequestId("01ARZ3NDEKTSV4RRFFQ69G5FAW");
const RECEIPT_ID = asReceiptId("01BRZ3NDEKTSV4RRFFQ69G5FA0");
const THREAD_ID = asThreadId("01CRZ3NDEKTSV4RRFFQ69G5FA1");
const TASK_ID = asTaskId("01DRZ3NDEKTSV4RRFFQ69G5FA2");
const OPERATOR = asSignerIdentity("operator@example.com");

function setup() {
  const db = openDatabase({ path: ":memory:" });
  runMigrations(db);
  const eventLog = createEventLog(db);
  const projection = createApprovalProjection(db);
  const appender = createApprovalAppender(db, eventLog, projection);
  return { db, eventLog, projection, appender };
}

function requestedPayload(
  requestId = REQUEST_ID,
  opts: { readonly thread?: boolean; readonly task?: boolean } = { thread: true, task: true },
): ApprovalRequestedAuditPayload {
  const claimId = asApprovalClaimId(`claim_${requestId.slice(-6).toLowerCase()}`);
  const claim = {
    schemaVersion: 1,
    claimId,
    kind: "receipt_co_sign",
    receiptId: RECEIPT_ID,
    frozenArgsHash: asSha256Hex("a".repeat(64)),
    riskClass: "high",
  } as const;
  const scope = {
    mode: "single_use",
    claimId,
    claimKind: "receipt_co_sign",
    role: asApprovalRole("approver"),
    maxUses: 1,
    receiptId: RECEIPT_ID,
    frozenArgsHash: asSha256Hex("a".repeat(64)),
  } as const;
  return {
    requestId,
    claim,
    scope,
    riskClass: "high",
    ...(opts.thread === false ? {} : { threadId: THREAD_ID }),
    ...(opts.task === false ? {} : { taskId: TASK_ID }),
    receiptId: RECEIPT_ID,
    requestedBy: OPERATOR,
    requestedAt: new Date("2026-05-18T10:00:00.000Z"),
  };
}

function decidedPayload(
  decision: ApprovalDecidedAuditPayload["decision"],
  requestId = REQUEST_ID,
): ApprovalDecidedAuditPayload {
  const token =
    decision === "approve" ? signedApprovalTokenFixture(requestedPayload(requestId)) : undefined;
  return {
    requestId,
    decision,
    decidedBy: asSignerIdentity("approver@example.com"),
    decidedAt: new Date("2026-05-18T10:01:00.000Z"),
    ...(token === undefined ? {} : { token }),
  };
}

function decidedPayloadWithToken(): ApprovalDecidedAuditPayload {
  const requested = requestedPayload();
  return {
    ...decidedPayload("approve"),
    token: signedApprovalTokenFixture(requested),
  };
}

function signedApprovalTokenFixture(
  requested: ApprovalRequestedAuditPayload,
  tokenId = asApprovalTokenId("01ERZ3NDEKTSV4RRFFQ69G5FA3"),
): SignedApprovalToken {
  const token: SignedApprovalToken = {
    schemaVersion: 1,
    tokenId,
    claim: requested.claim,
    scope: requested.scope,
    notBefore: asTimestampMs(Date.UTC(2026, 4, 18, 10, 0, 0, 0)),
    expiresAt: asTimestampMs(Date.UTC(2026, 4, 18, 10, 30, 0, 0)),
    issuedTo: asAgentId("agent_alpha"),
    signature: {
      credentialId: "YQ",
      authenticatorData: "YQ",
      clientDataJson: "YQ",
      signature: "YQ",
    },
  };
  return signedApprovalTokenFromJson(signedApprovalTokenToJsonValue(token));
}

function eventCount(db: ReturnType<typeof openDatabase>, type: string): number {
  return (
    db
      .prepare<[string], { readonly n: number }>(
        "SELECT COUNT(*) AS n FROM event_log WHERE type = ?",
      )
      .get(type)?.n ?? 0
  );
}

function projectionSnapshot(db: ReturnType<typeof openDatabase>): string {
  const rows = db
    .prepare<
      [],
      {
        readonly approvalId: string;
        readonly status: string;
        readonly headLsn: number;
        readonly claim: string;
        readonly scope: string;
        readonly riskClass: string;
        readonly threadId: string | null;
        readonly taskId: string | null;
        readonly receiptId: string | null;
        readonly requestedBy: string;
        readonly requestedAtMs: number;
        readonly decidedBy: string | null;
        readonly decidedAtMs: number | null;
        readonly decision: string | null;
        readonly token: string | null;
      }
    >(
      `SELECT approval_id AS approvalId, status, head_lsn AS headLsn, claim, scope,
              risk_class AS riskClass, thread_id AS threadId, task_id AS taskId,
              receipt_id AS receiptId, requested_by AS requestedBy,
              requested_at_ms AS requestedAtMs, decided_by AS decidedBy,
              decided_at_ms AS decidedAtMs, decision, token
       FROM pending_approvals ORDER BY approval_id ASC`,
    )
    .all();
  return canonicalJSON(rows);
}

function requestFingerprint(
  command: "approval.requested" | "approval.decided",
  requestId = REQUEST_ID,
  body: Readonly<Record<string, unknown>> = {},
): string {
  return canonicalJSON({ command, approvalId: requestId, body });
}

describe("approval projection and appender", () => {
  it("folds request then decision in LSN order and rejects a second decision", () => {
    const { db, projection, appender } = setup();
    try {
      const requested = appender.requestApproval(requestedPayload());
      expect(requested.lsn).toBe("v1:1");
      expect(requested.approval.status).toBe("pending");

      const decided = appender.decideApproval(decidedPayload("reject"));
      expect(decided.lsn).toBe("v1:2");
      expect(decided.approval.status).toBe("rejected");
      expect(decided.approval.decision?.decision).toBe("reject");

      expect(() => appender.decideApproval(decidedPayload("approve"))).toThrow(
        ApprovalRequestAlreadyDecidedError,
      );
      expect(eventCount(db, "approval.decided")).toBe(1);
      expect(projection.getById(REQUEST_ID)?.approval.status).toBe("rejected");
    } finally {
      db.close();
    }
  });

  it("derives status from decision and records SignedApprovalToken without verification", () => {
    const { db, projection, appender } = setup();
    try {
      appender.requestApproval(requestedPayload());
      appender.decideApproval(decidedPayloadWithToken());
      const row = projection.getById(REQUEST_ID);
      expect(row?.approval.status).toBe("approved");
      expect(row?.approval.decision?.decision).toBe("approve");
      expect(row?.approval.decision?.token?.tokenId).toBe("01ERZ3NDEKTSV4RRFFQ69G5FA3");

      expect(() => {
        db.exec("UPDATE pending_approvals SET status = 'rejected' WHERE approval_id IS NOT NULL");
      }).toThrow();
    } finally {
      db.close();
    }
  });

  it("replays duplicate idempotency keys without duplicate events or projection rows", () => {
    const { db, appender } = setup();
    try {
      const requestKey = parseApprovalIdempotencyKey(
        "cmd_approval.requested_01ARZ3NDEKTSV4RRFFQ69G5FAV",
        "approval.requested",
      );
      const decisionKey = parseApprovalIdempotencyKey(
        "cmd_approval.decided_01ARZ3NDEKTSV4RRFFQ69G5FAW",
        "approval.decided",
      );
      expect(requestKey.ok).toBe(true);
      expect(decisionKey.ok).toBe(true);
      if (!requestKey.ok || !decisionKey.ok) return;

      const render = (applied: { readonly approval: unknown }) => ({
        statusCode: 201,
        payload: Buffer.from(JSON.stringify(applied.approval), "utf8"),
      });
      const firstRequest = appender.requestApprovalIdempotent({
        payload: requestedPayload(),
        idempotency: requestKey.key,
        requestFingerprint: requestFingerprint("approval.requested"),
        nowMs: 1_700_000_000_000,
        render,
      });
      const replayedRequest = appender.requestApprovalIdempotent({
        payload: requestedPayload(),
        idempotency: requestKey.key,
        requestFingerprint: requestFingerprint("approval.requested"),
        nowMs: 1_700_000_000_001,
        render: () => {
          throw new Error("request render must not run on replay");
        },
      });
      expect(replayedRequest.replayed).toBe(true);
      expect(replayedRequest.payload.toString("utf8")).toBe(firstRequest.payload.toString("utf8"));

      const firstDecision = appender.decideApprovalIdempotent({
        payload: decidedPayload("approve"),
        idempotency: decisionKey.key,
        requestFingerprint: requestFingerprint("approval.decided"),
        nowMs: 1_700_000_000_002,
        render,
      });
      const replayedDecision = appender.decideApprovalIdempotent({
        payload: decidedPayload("approve"),
        idempotency: decisionKey.key,
        requestFingerprint: requestFingerprint("approval.decided"),
        nowMs: 1_700_000_000_003,
        render: () => {
          throw new Error("decision render must not run on replay");
        },
      });
      expect(replayedDecision.replayed).toBe(true);
      expect(replayedDecision.payload.toString("utf8")).toBe(
        firstDecision.payload.toString("utf8"),
      );
      expect(eventCount(db, "approval.requested")).toBe(1);
      expect(eventCount(db, "approval.decided")).toBe(1);
      expect(
        db.prepare<[], { readonly n: number }>("SELECT COUNT(*) AS n FROM pending_approvals").get()
          ?.n,
      ).toBe(1);
    } finally {
      db.close();
    }
  });

  it("rejects idempotency key reuse with a different resource fingerprint", () => {
    const { db, appender } = setup();
    try {
      const requestKey = parseApprovalIdempotencyKey(
        "cmd_approval.requested_01ARZ3NDEKTSV4RRFFQ69G5FAV",
        "approval.requested",
      );
      expect(requestKey.ok).toBe(true);
      if (!requestKey.ok) return;

      const render = (applied: { readonly approval: unknown }) => ({
        statusCode: 201,
        payload: Buffer.from(JSON.stringify(applied.approval), "utf8"),
      });
      appender.requestApprovalIdempotent({
        payload: requestedPayload(),
        idempotency: requestKey.key,
        requestFingerprint: requestFingerprint("approval.requested", REQUEST_ID),
        nowMs: 1_700_000_000_000,
        render,
      });
      expect(() =>
        appender.requestApprovalIdempotent({
          payload: requestedPayload(SECOND_REQUEST_ID),
          idempotency: requestKey.key,
          requestFingerprint: requestFingerprint("approval.requested", SECOND_REQUEST_ID),
          nowMs: 1_700_000_000_001,
          render,
        }),
      ).toThrow(ApprovalIdempotencyConflictError);
      expect(eventCount(db, "approval.requested")).toBe(1);
    } finally {
      db.close();
    }
  });

  it("rebuilds pending_approvals from the event log byte-equivalent to live projection", () => {
    const { db, eventLog, projection, appender } = setup();
    try {
      appender.requestApproval(requestedPayload(REQUEST_ID));
      appender.decideApproval(decidedPayload("abstain", REQUEST_ID));
      appender.requestApproval(requestedPayload(SECOND_REQUEST_ID, { thread: false, task: false }));

      const live = projectionSnapshot(db);
      db.exec("DELETE FROM pending_approvals");
      const rebuilt = rebuildApprovalsProjectionFromLog(db, eventLog);
      expect(rebuilt.eventsApplied).toBe(3);
      expect(projectionSnapshot(db)).toBe(live);
      expect(projection.list({ status: "pending" }).map((row) => row.approval.id)).toEqual([
        SECOND_REQUEST_ID,
      ]);
    } finally {
      db.close();
    }
  });

  it("projection replay follows LSN order for forged duplicate decision events", () => {
    const { db, eventLog, projection, appender } = setup();
    try {
      appender.requestApproval(requestedPayload());
      const rejectBytes = approvalAuditPayloadToBytes("approval_decided", decidedPayload("reject"));
      const approveBytes = approvalAuditPayloadToBytes(
        "approval_decided",
        decidedPayload("approve"),
      );
      eventLog.append({ type: "approval.decided", payload: Buffer.from(rejectBytes) });
      eventLog.append({ type: "approval.decided", payload: Buffer.from(approveBytes) });
      projection.rebuildFromLog(eventLog);
      const row = projection.getById(REQUEST_ID);
      if (row === null) throw new Error("missing approval row");
      expect(row.approval.status).toBe("approved");
      expect(approvalRequestToJsonValue(row.approval)).toMatchObject({ status: "approved" });
    } finally {
      db.close();
    }
  });
});
