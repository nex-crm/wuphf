// Regression gate for the PR #918 triangulation findings. Each test reproduces
// the exact end-to-end shape of a HIGH/MEDIUM defect a 5-lens review caught
// on the cherry-picked #916 fix round, and asserts the post-fix invariant.
// Reverting any of GROUPs 2/3/4 will turn one of these red on `bun run test`.

import {
  type ApprovalRequestedAuditPayload,
  approvalAuditPayloadToBytes,
  approvalRequestCreateRequestToJsonValue,
  asAgentId,
  asApiToken,
  asApprovalClaimId,
  asApprovalRequestId,
  asApprovalRole,
  asIdempotencyKey,
  asReceiptId,
  asSignerIdentity,
  asThreadId,
  MAX_ROUTE_APPROVAL_LIST_ITEMS,
  sha256Hex,
  threadListResponseFromJson,
  threadMutationResponseFromJson,
} from "@wuphf/protocol";
import type Database from "better-sqlite3";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { type ApprovalProjection, createApprovalSubsystem } from "../../src/approvals/index.ts";
import {
  createEventLog,
  type EventLog,
  openDatabase,
  runMigrations,
} from "../../src/event-log/index.ts";
import { type BrokerHandle, createBroker } from "../../src/index.ts";
import { constructSqliteReceiptStoreForTesting } from "../../src/internal/sqlite-receipt-store-testing.ts";
import type { SqliteReceiptStore } from "../../src/sqlite-receipt-store.ts";
import { createThreadSubsystem } from "../../src/threads/index.ts";

const TOKEN = asApiToken("triangulation-spec-token-with-entropy-AA");
const THREAD_A = "01BRZ3NDEKTSV4RRFFQ69G5FA0";
const THREAD_B = "01BRZ3NDEKTSV4RRFFQ69G5FB0";
const ORPHAN_THREAD = "01BRZ3NDEKTSV4RRFFQ69G5FX0";
const APPROVAL_REQUEST_ID = "01MRZ3NDEKTSV4RRFFQ69G5FM0";

interface Fixture {
  readonly broker: BrokerHandle;
  readonly db: Database.Database;
  readonly eventLog: EventLog;
  readonly approvals: ApprovalProjection;
  readonly receiptStore: SqliteReceiptStore;
}

async function setup(): Promise<Fixture> {
  const db = openDatabase({ path: ":memory:" });
  runMigrations(db);
  const eventLog = createEventLog(db);
  const receiptStore = constructSqliteReceiptStoreForTesting(db, eventLog);
  const threads = createThreadSubsystem(db, eventLog, receiptStore);
  const approvals = createApprovalSubsystem(db, eventLog, {
    threadRefValidator: (threadId) => threads.state.getById(threadId) !== null,
  });
  const broker = await createBroker({
    port: 0,
    token: TOKEN,
    threads,
    approvals: {
      appender: approvals.appender,
      projection: approvals.projection,
      tokenAgentIds: new Map([[TOKEN, asAgentId("agent_alpha")]]),
    },
  });
  return { broker, db, eventLog, approvals: approvals.projection, receiptStore };
}

function authJson(): Record<string, string> {
  return { Authorization: `Bearer ${TOKEN}`, "Content-Type": "application/json" };
}

async function createThread(
  fix: Fixture,
  idempotencyKey: string,
): Promise<{ readonly id: string; readonly headLsn: string }> {
  const res = await fetch(`${fix.broker.url}/api/v1/threads`, {
    method: "POST",
    headers: authJson(),
    body: JSON.stringify({
      title: `triangulation thread ${idempotencyKey}`,
      specContent: { goal: "triangulation regression", key: idempotencyKey },
      externalRefs: { source_urls: [], entity_ids: [] },
      idempotencyKey,
    }),
  });
  expect(res.status).toBe(201);
  const body = threadMutationResponseFromJson((await res.json()) as unknown);
  return { id: body.threadId, headLsn: body.headLsn };
}

async function createApprovalForThread(fix: Fixture, threadId: string): Promise<void> {
  const claimId = asApprovalClaimId("claim_triangulation");
  const receiptId = asReceiptId("01PRZ3NDEKTSV4RRFFQ69G5FP0");
  const frozenArgsHash = sha256Hex("triangulation-spec-frozen-args");
  const body = approvalRequestCreateRequestToJsonValue({
    schemaVersion: 1,
    claim: {
      schemaVersion: 1,
      claimId,
      kind: "receipt_co_sign",
      receiptId,
      frozenArgsHash,
      riskClass: "critical",
    },
    scope: {
      mode: "single_use",
      claimId,
      claimKind: "receipt_co_sign",
      role: asApprovalRole("approver"),
      maxUses: 1,
      receiptId,
      frozenArgsHash,
    },
    riskClass: "critical",
    threadId: asThreadId(threadId),
    receiptId,
    idempotencyKey: asIdempotencyKey(APPROVAL_REQUEST_ID),
  });
  const res = await fetch(`${fix.broker.url}/api/v1/approvals`, {
    method: "POST",
    headers: authJson(),
    body: JSON.stringify(body),
  });
  expect(res.status).toBe(201);
}

async function listThreads(
  fix: Fixture,
  query: string,
): Promise<{ readonly ids: readonly string[]; readonly nextCursor: string | null }> {
  const res = await fetch(`${fix.broker.url}/api/v1/threads${query}`, {
    headers: { Authorization: `Bearer ${TOKEN}` },
  });
  expect(res.status).toBe(200);
  const body = threadListResponseFromJson((await res.json()) as unknown);
  return { ids: body.threads.map((t) => t.id), nextCursor: body.nextCursor ?? null };
}

// Build a well-formed ApprovalRequestedAuditPayload that varies per `index`
// so projection/log inserts don't collide on the primary key.
function buildApprovalPayload(threadIdStr: string, index: number): ApprovalRequestedAuditPayload {
  const suffix = index.toString().padStart(7, "0"); // crockford-safe digits
  const requestIdStr = `01BRZ3NDEKTSV4RRFFQ${suffix}`;
  const receiptIdStr = `01PRZ3NDEKTSV4RRFFP${suffix}`;
  const claimId = asApprovalClaimId(`claim_tri_${index}`);
  const receiptId = asReceiptId(receiptIdStr);
  const frozenArgsHash = sha256Hex(`triangulation-spec-frozen-${index}`);
  return {
    requestId: asApprovalRequestId(requestIdStr),
    claim: {
      schemaVersion: 1,
      claimId,
      kind: "receipt_co_sign",
      receiptId,
      frozenArgsHash,
      riskClass: "critical",
    },
    scope: {
      mode: "single_use",
      claimId,
      claimKind: "receipt_co_sign",
      role: asApprovalRole("approver"),
      maxUses: 1,
      receiptId,
      frozenArgsHash,
    },
    riskClass: "critical",
    threadId: asThreadId(threadIdStr),
    receiptId,
    requestedBy: asSignerIdentity("broker"),
    requestedAt: new Date(`2026-05-19T20:00:${(index % 60).toString().padStart(2, "0")}.000Z`),
  };
}

function appendApprovalRequestedToLog(
  eventLog: EventLog,
  payload: ApprovalRequestedAuditPayload,
): { readonly lsn: number; readonly bytes: Buffer } {
  const bytes = Buffer.from(approvalAuditPayloadToBytes("approval_requested", payload));
  const lsn = eventLog.append({ type: "approval.requested", payload: bytes });
  return { lsn, bytes };
}

describe("PR #918 triangulation regressions", () => {
  let fixture: Fixture | null = null;

  beforeEach(async () => {
    fixture = await setup();
  });

  afterEach(async () => {
    if (fixture !== null) {
      await fixture.broker.stop();
      fixture.receiptStore.close();
      fixture = null;
    }
  });

  // GROUP 2 — thread-list cursor stays coherent with the derived
  // effective_status filter. The list route used to page on
  // `threads.head_lsn`, derive effective_status from approvals/receipts at
  // read time, then filter AFTER slicing. A thread paged past while it did
  // not match could later start matching (a new approval) without its
  // head_lsn moving — and a client following its cursor would never see it
  // again. The post-fix list route pages by an effective view LSN
  // (max of thread/approval/receipt LSNs), so this regression cannot happen.
  it("thread-list cursor returns a thread that later starts matching the filter", async () => {
    const fix = fixture as Fixture;
    const a = await createThread(fix, THREAD_A);
    await createThread(fix, THREAD_B);

    // A client paged past A while A was 'open' — its cursor is now A.headLsn.
    // Then A gains a pending approval -> effective_status flips to
    // needs_attention; A.head_lsn does NOT move.
    await createApprovalForThread(fix, a.id);

    const cursored = await listThreads(
      fix,
      `?status=needs_attention&cursor=${encodeURIComponent(a.headLsn)}`,
    );
    const full = await listThreads(fix, "?status=needs_attention");

    expect(full.ids).toContain(a.id);
    expect(cursored.ids).toContain(a.id);
  });

  // GROUP 4 — replay invariants per §15.B. The old appender validated
  // FK/per-thread cap at append time only. Rebuild re-inserted every
  // approval.requested verbatim, so a historically-bad event silently
  // reappeared on every restart. The post-fix rebuild asserts the invariants
  // and fails closed.
  it("rebuild fails closed on an orphan-thread approval event", async () => {
    const fix = fixture as Fixture;
    const { lsn } = appendApprovalRequestedToLog(
      fix.eventLog,
      buildApprovalPayload(ORPHAN_THREAD, 1),
    );
    expect(() => fix.approvals.rebuildFromLog(fix.eventLog)).toThrowError(
      expect.objectContaining({ name: "ApprovalReplayThreadNotFoundError", lsn }),
    );
  });

  it("rebuild fails closed on per-thread cap overflow", async () => {
    const fix = fixture as Fixture;
    const a = await createThread(fix, THREAD_A);
    let overflowLsn: number | null = null;
    for (let i = 0; i <= MAX_ROUTE_APPROVAL_LIST_ITEMS; i += 1) {
      const { lsn } = appendApprovalRequestedToLog(
        fix.eventLog,
        buildApprovalPayload(a.id, 1000 + i),
      );
      if (i === MAX_ROUTE_APPROVAL_LIST_ITEMS) {
        overflowLsn = lsn;
      }
    }
    if (overflowLsn === null) throw new Error("overflow event was not appended");
    expect(() => fix.approvals.rebuildFromLog(fix.eventLog)).toThrowError(
      expect.objectContaining({
        name: "ApprovalReplayPendingLimitExceededError",
        lsn: overflowLsn,
      }),
    );
  });

  // GROUP 3 — pinned-approvals fails loud instead of silently truncating.
  // The append-time cap is forward-only; a pre-fix DB can hold >cap rows
  // per thread. The old read returned 200 with the first cap rows + a
  // headLsn — clients believed they had the complete set. The post-fix
  // read returns a 5xx with a structured `pinned_approvals_overflow` error.
  it("pinned-approvals read fails loud when stored rows exceed the cap", async () => {
    const fix = fixture as Fixture;
    const a = await createThread(fix, THREAD_A);
    const count = MAX_ROUTE_APPROVAL_LIST_ITEMS + 1;
    for (let i = 0; i < count; i += 1) {
      const payload = buildApprovalPayload(a.id, 2000 + i);
      const { lsn, bytes } = appendApprovalRequestedToLog(fix.eventLog, payload);
      fix.approvals.applyEvent({ lsn, type: "approval.requested", payload: bytes });
    }

    const res = await fetch(`${fix.broker.url}/api/v1/threads/${a.id}/pinned-approvals`, {
      headers: { Authorization: `Bearer ${TOKEN}` },
    });
    expect(res.status).toBeGreaterThanOrEqual(500);
    expect(res.status).toBeLessThan(600);
    const body = (await res.json()) as { readonly error?: string };
    expect(body.error).toBe("pinned_approvals_overflow");
  });
});
