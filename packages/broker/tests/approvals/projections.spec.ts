import {
  type ApprovalDecidedAuditPayload,
  type ApprovalRequestedAuditPayload,
  approvalAuditPayloadToBytes,
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
  MAX_ROUTE_APPROVAL_LIST_ITEMS,
  type SignedApprovalToken,
  signedApprovalTokenFromJson,
  signedApprovalTokenToJsonValue,
} from "@wuphf/protocol";
import { describe, expect, it } from "vitest";

import {
  ApprovalIdempotencyConflictError,
  ApprovalReplayPendingLimitExceededError,
  ApprovalReplayThreadNotFoundError,
  ApprovalRequestAlreadyDecidedError,
  ApprovalThreadNotFoundError,
  ApprovalTokenAlreadyUsedError,
  ApprovalTokenIssuedToMismatchError,
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
const ULID_TIME_PREFIX = "01ZRZ3NDEK";
const ULID_ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";

function setup() {
  const db = openDatabase({ path: ":memory:" });
  runMigrations(db);
  const eventLog = createEventLog(db);
  const projection = createApprovalProjection(db);
  const appender = createApprovalAppender(db, eventLog, projection);
  return { db, eventLog, projection, appender };
}

function setupWithThreadValidator(
  threadIds: readonly ReturnType<typeof asThreadId>[] = [THREAD_ID],
) {
  const db = openDatabase({ path: ":memory:" });
  runMigrations(db);
  const eventLog = createEventLog(db);
  for (const threadId of threadIds) {
    insertThreadProjectionRow(db, eventLog, threadId);
  }
  const threadExistsStmt = db.prepare<[string], { readonly present: 1 }>(
    "SELECT 1 AS present FROM threads WHERE thread_id = ?",
  );
  const projection = createApprovalProjection(db);
  const appender = createApprovalAppender(db, eventLog, projection, {
    threadRefValidator: (threadId) => threadExistsStmt.get(threadId) !== undefined,
  });
  return { db, eventLog, projection, appender };
}

function requestedPayload(
  requestId = REQUEST_ID,
  opts: { readonly thread?: boolean; readonly task?: boolean } = { thread: false, task: true },
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
    decidedBy: asSignerIdentity("agent_alpha"),
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

function mismatchedTokenDecisionPayload(): ApprovalDecidedAuditPayload {
  return {
    ...decidedPayloadWithToken(),
    decidedBy: asSignerIdentity("agent_beta"),
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

function indexedApprovalRequestId(index: number): ReturnType<typeof asApprovalRequestId> {
  let value = index;
  let suffix = "";
  for (let i = 0; i < 16; i += 1) {
    suffix = ULID_ALPHABET[value % ULID_ALPHABET.length] + suffix;
    value = Math.floor(value / ULID_ALPHABET.length);
  }
  return asApprovalRequestId(`${ULID_TIME_PREFIX}${suffix}`);
}

function insertThreadProjectionRow(
  db: ReturnType<typeof openDatabase>,
  eventLog: ReturnType<typeof createEventLog>,
  threadId = THREAD_ID,
): void {
  const lsn = eventLog.append({ type: "thread.created", payload: Buffer.from("{}") });
  db.prepare<[string, number, string, string], void>(
    `INSERT INTO threads
       (thread_id, title, status, head_lsn, created_by, created_at_ms, updated_at_ms,
        external_refs)
     VALUES (?, 'Replay thread', 'open', ?, ?, 0, 0, ?)`,
  ).run(threadId, lsn, OPERATOR, canonicalJSON({ source_urls: [], entity_ids: [] }));
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
        readonly tokenId: string | null;
      }
    >(
      `SELECT approval_id AS approvalId, status, head_lsn AS headLsn, claim, scope,
              risk_class AS riskClass, thread_id AS threadId, task_id AS taskId,
              receipt_id AS receiptId, requested_by AS requestedBy,
              requested_at_ms AS requestedAtMs, decided_by AS decidedBy,
              decided_at_ms AS decidedAtMs, decision, token, token_id AS tokenId
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
  it("rejects explicit thread references when constructed without a thread validator", () => {
    const { db, appender } = setup();
    try {
      expect(() =>
        appender.requestApproval(requestedPayload(REQUEST_ID, { thread: true, task: false })),
      ).toThrow(ApprovalThreadNotFoundError);
      expect(eventCount(db, "approval.requested")).toBe(0);
    } finally {
      db.close();
    }
  });

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

  it("rejects reusing one approval token across multiple approvals", () => {
    const { db, projection, appender } = setup();
    try {
      const firstRequest = requestedPayload(REQUEST_ID);
      const secondRequest = { ...firstRequest, requestId: SECOND_REQUEST_ID };
      const sharedToken = signedApprovalTokenFixture(firstRequest);
      appender.requestApproval(firstRequest);
      appender.requestApproval(secondRequest);

      appender.decideApproval({
        ...decidedPayload("approve", REQUEST_ID),
        token: sharedToken,
      });
      expect(() =>
        appender.decideApproval({
          ...decidedPayload("approve", SECOND_REQUEST_ID),
          token: sharedToken,
        }),
      ).toThrow(ApprovalTokenAlreadyUsedError);

      expect(eventCount(db, "approval.decided")).toBe(1);
      expect(projection.getById(SECOND_REQUEST_ID)?.approval.status).toBe("pending");
    } finally {
      db.close();
    }
  });

  it("rejects direct approval decisions when the token was issued to a different actor", () => {
    const { db, projection, appender } = setup();
    try {
      appender.requestApproval(requestedPayload());
      const before = projectionSnapshot(db);

      expect(() => appender.decideApproval(mismatchedTokenDecisionPayload())).toThrow(
        ApprovalTokenIssuedToMismatchError,
      );

      expect(eventCount(db, "approval.decided")).toBe(0);
      expect(projectionSnapshot(db)).toBe(before);
      expect(projection.getById(REQUEST_ID)?.approval.status).toBe("pending");
    } finally {
      db.close();
    }
  });

  it("rejects idempotent approval decisions when the token was issued to a different actor", () => {
    const { db, projection, appender } = setup();
    try {
      appender.requestApproval(requestedPayload());
      const before = projectionSnapshot(db);
      const decisionKey = parseApprovalIdempotencyKey(
        "cmd_approval.decided_01ARZ3NDEKTSV4RRFFQ69G5FAW",
        "approval.decided",
      );
      expect(decisionKey.ok).toBe(true);
      if (!decisionKey.ok) return;

      expect(() =>
        appender.decideApprovalIdempotent({
          payload: mismatchedTokenDecisionPayload(),
          idempotency: decisionKey.key,
          requestFingerprint: requestFingerprint("approval.decided"),
          nowMs: 1_700_000_000_000,
          render: () => {
            throw new Error("decision render must not run when token actor mismatches");
          },
        }),
      ).toThrow(ApprovalTokenIssuedToMismatchError);

      expect(eventCount(db, "approval.decided")).toBe(0);
      expect(projectionSnapshot(db)).toBe(before);
      expect(projection.getById(REQUEST_ID)?.approval.status).toBe("pending");
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

  it("prunes only approval command idempotency rows", () => {
    const { db, appender } = setup();
    try {
      db.prepare<[string, string, number], void>(
        `INSERT INTO command_idempotency
           (idempotency_key, command, status_code, response_payload, created_at_lsn,
            created_at_ms, request_fingerprint)
         VALUES (?, ?, 201, x'7B7D', NULL, ?, NULL)`,
      ).run("old-approval", "approval.requested", 1_700_000_000_000);
      db.prepare<[string, string, number], void>(
        `INSERT INTO command_idempotency
           (idempotency_key, command, status_code, response_payload, created_at_lsn,
            created_at_ms, request_fingerprint)
         VALUES (?, ?, 201, x'7B7D', NULL, ?, NULL)`,
      ).run("old-cost", "cost.event", 1_700_000_000_000);

      expect(appender.pruneIdempotencyOlderThan(1_700_000_000_001)).toBe(1);
      expect(
        db
          .prepare<[], { readonly command: string }>(
            "SELECT command FROM command_idempotency ORDER BY command ASC",
          )
          .all(),
      ).toEqual([{ command: "cost.event" }]);
    } finally {
      db.close();
    }
  });

  it("queries pinned approvals as pending rows scoped to a thread", () => {
    const otherThreadId = asThreadId("01FRZ3NDEKTSV4RRFFQ69G5FA4");
    const { db, projection, appender } = setupWithThreadValidator([THREAD_ID, otherThreadId]);
    try {
      appender.requestApproval(requestedPayload(REQUEST_ID, { thread: true }));
      appender.requestApproval({
        ...requestedPayload(SECOND_REQUEST_ID, { thread: true }),
        threadId: otherThreadId,
      });
      expect(projection.countPendingByThread(THREAD_ID)).toBe(1);
      expect(projection.listPendingByThread(THREAD_ID).map((row) => row.approval.id)).toEqual([
        REQUEST_ID,
      ]);
      expect(projection.latestHeadLsnByThread(THREAD_ID)).toBe("v1:3");

      appender.decideApproval(decidedPayload("reject", REQUEST_ID));
      expect(projection.countPendingByThread(THREAD_ID)).toBe(0);
      expect(projection.listPendingByThread(THREAD_ID)).toEqual([]);
      expect(projection.latestHeadLsnByThread(THREAD_ID)).toBe("v1:5");
      expect(projection.countPendingByThread(otherThreadId)).toBe(1);

      const indexes = db
        .prepare<[], { readonly name: string }>(
          "SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'pending_approvals_thread_status'",
        )
        .all();
      expect(indexes.map((row) => row.name)).toEqual(["pending_approvals_thread_status"]);
    } finally {
      db.close();
    }
  });

  it("rebuilds pending_approvals from the event log byte-equivalent to live projection", () => {
    const { db, eventLog, projection, appender } = setup();
    try {
      appender.requestApproval(requestedPayload(REQUEST_ID, { thread: false, task: false }));
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

  it("projection replay rejects requested events for missing threads", () => {
    const { db, eventLog, projection, appender } = setup();
    try {
      appender.requestApproval(requestedPayload(REQUEST_ID, { thread: false, task: false }));
      const live = projectionSnapshot(db);
      const bytes = approvalAuditPayloadToBytes(
        "approval_requested",
        requestedPayload(SECOND_REQUEST_ID, { thread: true, task: false }),
      );
      eventLog.append({ type: "approval.requested", payload: Buffer.from(bytes) });

      expect(() => projection.rebuildFromLog(eventLog)).toThrow(ApprovalReplayThreadNotFoundError);
      expect(projectionSnapshot(db)).toBe(live);
    } finally {
      db.close();
    }
  });

  it("projection replay rejects per-thread pending approval overflow", () => {
    const { db, eventLog, projection } = setup();
    try {
      insertThreadProjectionRow(db, eventLog);
      for (let index = 0; index <= MAX_ROUTE_APPROVAL_LIST_ITEMS; index += 1) {
        const bytes = approvalAuditPayloadToBytes(
          "approval_requested",
          requestedPayload(indexedApprovalRequestId(index), { thread: true, task: false }),
        );
        eventLog.append({ type: "approval.requested", payload: Buffer.from(bytes) });
      }

      expect(() => projection.rebuildFromLog(eventLog)).toThrow(
        ApprovalReplayPendingLimitExceededError,
      );
      expect(projection.countPendingByThread(THREAD_ID)).toBe(0);
    } finally {
      db.close();
    }
  });

  it("projection replay rejects forged duplicate decision events", () => {
    const { db, eventLog, projection, appender } = setup();
    try {
      appender.requestApproval(requestedPayload(REQUEST_ID, { thread: false, task: false }));
      const rejectBytes = approvalAuditPayloadToBytes("approval_decided", decidedPayload("reject"));
      const approveBytes = approvalAuditPayloadToBytes(
        "approval_decided",
        decidedPayload("approve"),
      );
      eventLog.append({ type: "approval.decided", payload: Buffer.from(rejectBytes) });
      eventLog.append({ type: "approval.decided", payload: Buffer.from(approveBytes) });
      expect(() => projection.rebuildFromLog(eventLog)).toThrow(
        "approval.decided has no pending request",
      );
    } finally {
      db.close();
    }
  });

  it("projection replay rejects duplicate requested events", () => {
    const { db, eventLog, projection } = setup();
    try {
      const bytes = approvalAuditPayloadToBytes(
        "approval_requested",
        requestedPayload(REQUEST_ID, { thread: false, task: false }),
      );
      eventLog.append({ type: "approval.requested", payload: Buffer.from(bytes) });
      eventLog.append({ type: "approval.requested", payload: Buffer.from(bytes) });

      expect(() => projection.rebuildFromLog(eventLog)).toThrow();
    } finally {
      db.close();
    }
  });
});
