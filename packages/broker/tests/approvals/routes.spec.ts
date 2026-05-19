import { request as httpRequest, type OutgoingHttpHeaders } from "node:http";

import {
  type ApprovalDecidedAuditPayload,
  type ApprovalRequestedAuditPayload,
  type ApprovalStreamEvent,
  approvalAuditPayloadToJsonValue,
  approvalRequestFromJsonValue,
  asApiToken,
  asApprovalClaimId,
  asApprovalRequestId,
  asApprovalRole,
  asReceiptId,
  asSha256Hex,
  asSignerIdentity,
  asTaskId,
  asThreadId,
  validateApprovalStreamEvent,
} from "@wuphf/protocol";
import BetterSqlite3 from "better-sqlite3";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import type { ApprovalAppender, ApprovalProjection } from "../../src/approvals/index.ts";
import { createApprovalAppender, createApprovalProjection } from "../../src/approvals/index.ts";
import { createEventLog, openDatabase, runMigrations } from "../../src/event-log/index.ts";
import type { BrokerHandle } from "../../src/index.ts";
import { createBroker } from "../../src/index.ts";

const TOKEN = asApiToken("test-token-with-enough-entropy-AAAAAAAAA");
const REQUEST_ID = asApprovalRequestId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
const UNKNOWN_REQUEST_ID = asApprovalRequestId("01ARZ3NDEKTSV4RRFFQ69G5FAZ");
const RECEIPT_ID = asReceiptId("01BRZ3NDEKTSV4RRFFQ69G5FA0");
const THREAD_ID = asThreadId("01CRZ3NDEKTSV4RRFFQ69G5FA1");
const TASK_ID = asTaskId("01DRZ3NDEKTSV4RRFFQ69G5FA2");

interface Fixture {
  readonly db: ReturnType<typeof openDatabase>;
  readonly broker: BrokerHandle;
  readonly appender: ApprovalAppender;
  readonly projection: ApprovalProjection;
}

function requestedPayload(requestId = REQUEST_ID): ApprovalRequestedAuditPayload {
  const claimId = asApprovalClaimId("claim_route");
  const frozenArgsHash = asSha256Hex("b".repeat(64));
  const claim = {
    schemaVersion: 1,
    claimId,
    kind: "receipt_co_sign",
    receiptId: RECEIPT_ID,
    frozenArgsHash,
    riskClass: "critical",
  } as const;
  const scope = {
    mode: "single_use",
    claimId,
    claimKind: "receipt_co_sign",
    role: asApprovalRole("approver"),
    maxUses: 1,
    receiptId: RECEIPT_ID,
    frozenArgsHash,
  } as const;
  return {
    requestId,
    claim,
    scope,
    riskClass: "critical",
    threadId: THREAD_ID,
    taskId: TASK_ID,
    receiptId: RECEIPT_ID,
    requestedBy: asSignerIdentity("operator@example.com"),
    requestedAt: new Date("2026-05-18T11:00:00.000Z"),
  };
}

function decidedPayload(
  decision: ApprovalDecidedAuditPayload["decision"],
  requestId = REQUEST_ID,
): ApprovalDecidedAuditPayload {
  return {
    requestId,
    decision,
    decidedBy: asSignerIdentity("approver@example.com"),
    decidedAt: new Date("2026-05-18T11:01:00.000Z"),
  };
}

async function buildFixture(overrides?: {
  readonly appender?: (base: ApprovalAppender) => ApprovalAppender;
  readonly projection?: (base: ApprovalProjection) => ApprovalProjection;
}): Promise<Fixture> {
  const db = openDatabase({ path: ":memory:" });
  runMigrations(db);
  const eventLog = createEventLog(db);
  const baseProjection = createApprovalProjection(db);
  const baseAppender = createApprovalAppender(db, eventLog, baseProjection);
  const projection = overrides?.projection?.(baseProjection) ?? baseProjection;
  const appender = overrides?.appender?.(baseAppender) ?? baseAppender;
  const broker = await createBroker({
    port: 0,
    token: TOKEN,
    approvals: { appender, projection, db },
  });
  return { db, broker, appender, projection };
}

async function teardown(fix: Fixture | null): Promise<void> {
  if (fix === null) return;
  await fix.broker.stop();
  fix.db.close();
}

function authHeaders(extra: Record<string, string> = {}): Record<string, string> {
  return {
    Authorization: `Bearer ${TOKEN}`,
    "Content-Type": "application/json",
    ...extra,
  };
}

function requestBody(payload: ApprovalRequestedAuditPayload): string {
  return JSON.stringify(approvalAuditPayloadToJsonValue("approval_requested", payload));
}

function decisionBody(payload: ApprovalDecidedAuditPayload): string {
  return JSON.stringify(approvalAuditPayloadToJsonValue("approval_decided", payload));
}

async function postApproval(fix: Fixture, keyTail = "01ARZ3NDEKTSV4RRFFQ69G5FAV") {
  return await fetch(`${fix.broker.url}/api/v1/approvals`, {
    method: "POST",
    headers: authHeaders({ "Idempotency-Key": `cmd_approval.requested_${keyTail}` }),
    body: requestBody(requestedPayload()),
  });
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

function sqliteError(code: string): Error {
  return new BetterSqlite3.SqliteError("test sqlite error", code);
}

function rawRequest(args: {
  readonly port: number;
  readonly method: string;
  readonly path: string;
  readonly headers?: OutgoingHttpHeaders;
  readonly body?: string;
}): Promise<{ readonly status: number; readonly body: string }> {
  return new Promise((resolveFn, rejectFn) => {
    const req = httpRequest(
      {
        host: "127.0.0.1",
        port: args.port,
        path: args.path,
        method: args.method,
        headers: args.headers,
      },
      (res) => {
        const chunks: Buffer[] = [];
        res.on("data", (chunk: Buffer) => chunks.push(chunk));
        res.on("end", () =>
          resolveFn({
            status: res.statusCode ?? 0,
            body: Buffer.concat(chunks).toString("utf8"),
          }),
        );
      },
    );
    req.on("error", rejectFn);
    if (args.body !== undefined) req.write(args.body);
    req.end();
  });
}

async function readUntil(reader: ReadableStreamDefaultReader<Uint8Array>, needle: string) {
  let text = "";
  for (let i = 0; i < 8; i += 1) {
    const next = await reader.read();
    if (next.done) break;
    text += new TextDecoder().decode(next.value);
    if (text.includes(needle)) return text;
  }
  return text;
}

function parseApprovalSse(text: string, kind: "approval.requested" | "approval.decided") {
  const blocks = text.split("\n\n");
  for (const block of blocks) {
    if (!block.includes(`event: ${kind}`)) continue;
    const dataLine = block.split("\n").find((line) => line.startsWith("data: "));
    if (dataLine === undefined) throw new Error(`missing data line for ${kind}`);
    return JSON.parse(dataLine.slice("data: ".length)) as ApprovalStreamEvent;
  }
  throw new Error(`missing SSE event ${kind}`);
}

describe("/api/v1/approvals routes", () => {
  let fix: Fixture | null = null;

  beforeEach(async () => {
    fix = await buildFixture();
  });

  afterEach(async () => {
    await teardown(fix);
    fix = null;
  });

  it("records, lists, fetches, and decides approval requests through protocol codecs", async () => {
    if (fix === null) throw new Error("fixture missing");
    const create = await postApproval(fix);
    expect(create.status).toBe(201);
    const created = approvalRequestFromJsonValue((await create.json()) as unknown);
    expect(created.id).toBe(REQUEST_ID);
    expect(created.status).toBe("pending");

    const listPending = await fetch(`${fix.broker.url}/api/v1/approvals?status=pending`, {
      headers: { Authorization: `Bearer ${TOKEN}` },
    });
    expect(listPending.status).toBe(200);
    const pendingBody = (await listPending.json()) as { readonly approvals: readonly unknown[] };
    expect(pendingBody.approvals.map((item) => approvalRequestFromJsonValue(item).id)).toEqual([
      REQUEST_ID,
    ]);

    const byThread = await fetch(
      `${fix.broker.url}/api/v1/approvals?threadId=${THREAD_ID}&taskId=${TASK_ID}`,
      { headers: { Authorization: `Bearer ${TOKEN}` } },
    );
    expect(byThread.status).toBe(200);
    expect(
      ((await byThread.json()) as { readonly approvals: readonly unknown[] }).approvals.length,
    ).toBe(1);

    const get = await fetch(`${fix.broker.url}/api/v1/approvals/${REQUEST_ID}`, {
      headers: { Authorization: `Bearer ${TOKEN}` },
    });
    expect(get.status).toBe(200);
    expect(approvalRequestFromJsonValue((await get.json()) as unknown).status).toBe("pending");

    const decided = await fetch(`${fix.broker.url}/api/v1/approvals/${REQUEST_ID}/decision`, {
      method: "POST",
      headers: authHeaders({
        "Idempotency-Key": "cmd_approval.decided_01ARZ3NDEKTSV4RRFFQ69G5FAW",
      }),
      body: decisionBody(decidedPayload("approve")),
    });
    expect(decided.status).toBe(201);
    const decidedBody = approvalRequestFromJsonValue((await decided.json()) as unknown);
    expect(decidedBody.status).toBe("approved");
    expect(decidedBody.decision?.decision).toBe("approve");
  });

  it("emits validated approval.requested and approval.decided SSE invalidations", async () => {
    if (fix === null) throw new Error("fixture missing");
    const controller = new AbortController();
    const stream = await fetch(`${fix.broker.url}/api/events`, {
      headers: { Authorization: `Bearer ${TOKEN}`, Accept: "text/event-stream" },
      signal: controller.signal,
    });
    expect(stream.status).toBe(200);
    const reader = stream.body?.getReader();
    if (reader === undefined) throw new Error("missing SSE reader");
    await readUntil(reader, "event: ready");

    const created = await postApproval(fix);
    expect(created.status).toBe(201);
    const requestedText = await readUntil(reader, "event: approval.requested");
    const requestedEvent = parseApprovalSse(requestedText, "approval.requested");
    expect(validateApprovalStreamEvent(requestedEvent).ok).toBe(true);
    expect(requestedEvent.payload.requestId).toBe(REQUEST_ID);
    expect(requestedEvent.payload.threadId).toBe(THREAD_ID);
    expect(requestedEvent.payload.headLsn).toBe("v1:1");

    const decided = await fetch(`${fix.broker.url}/api/v1/approvals/${REQUEST_ID}/decision`, {
      method: "POST",
      headers: authHeaders({
        "Idempotency-Key": "cmd_approval.decided_01ARZ3NDEKTSV4RRFFQ69G5FAW",
      }),
      body: decisionBody(decidedPayload("reject")),
    });
    expect(decided.status).toBe(201);
    const decidedText = await readUntil(reader, "event: approval.decided");
    const decidedEvent = parseApprovalSse(decidedText, "approval.decided");
    expect(validateApprovalStreamEvent(decidedEvent).ok).toBe(true);
    expect(decidedEvent.payload.headLsn).toBe("v1:2");
    controller.abort();
  });

  it("duplicate command keys replay without appending duplicate events", async () => {
    if (fix === null) throw new Error("fixture missing");
    const keyTail = "01ARZ3NDEKTSV4RRFFQ69G5FAV";
    const first = await postApproval(fix, keyTail);
    const firstBody = await first.text();
    const second = await postApproval(fix, keyTail);
    expect(second.status).toBe(201);
    expect(second.headers.get("Idempotent-Replay")).toBe("true");
    expect(await second.text()).toBe(firstBody);
    expect(eventCount(fix.db, "approval.requested")).toBe(1);

    const decisionHeaders = authHeaders({
      "Idempotency-Key": "cmd_approval.decided_01ARZ3NDEKTSV4RRFFQ69G5FAW",
    });
    const firstDecision = await fetch(`${fix.broker.url}/api/v1/approvals/${REQUEST_ID}/decision`, {
      method: "POST",
      headers: decisionHeaders,
      body: decisionBody(decidedPayload("approve")),
    });
    const firstDecisionBody = await firstDecision.text();
    const replayedDecision = await fetch(
      `${fix.broker.url}/api/v1/approvals/${REQUEST_ID}/decision`,
      {
        method: "POST",
        headers: decisionHeaders,
        body: decisionBody(decidedPayload("reject")),
      },
    );
    expect(replayedDecision.status).toBe(201);
    expect(replayedDecision.headers.get("Idempotent-Replay")).toBe("true");
    expect(await replayedDecision.text()).toBe(firstDecisionBody);
    expect(eventCount(fix.db, "approval.decided")).toBe(1);
  });

  it("returns 409 for a second decision with a fresh key and appends no event", async () => {
    if (fix === null) throw new Error("fixture missing");
    expect((await postApproval(fix)).status).toBe(201);
    const first = await fetch(`${fix.broker.url}/api/v1/approvals/${REQUEST_ID}/decision`, {
      method: "POST",
      headers: authHeaders({
        "Idempotency-Key": "cmd_approval.decided_01ARZ3NDEKTSV4RRFFQ69G5FAW",
      }),
      body: decisionBody(decidedPayload("approve")),
    });
    expect(first.status).toBe(201);
    const second = await fetch(`${fix.broker.url}/api/v1/approvals/${REQUEST_ID}/decision`, {
      method: "POST",
      headers: authHeaders({
        "Idempotency-Key": "cmd_approval.decided_01ARZ3NDEKTSV4RRFFQ69G5FAX",
      }),
      body: decisionBody(decidedPayload("reject")),
    });
    expect(second.status).toBe(409);
    expect(await second.json()).toEqual({ error: "approval_not_pending" });
    expect(eventCount(fix.db, "approval.decided")).toBe(1);
  });

  it("rejects unknown approvals and missing decision payloads", async () => {
    if (fix === null) throw new Error("fixture missing");
    const unknown = await fetch(
      `${fix.broker.url}/api/v1/approvals/${UNKNOWN_REQUEST_ID}/decision`,
      {
        method: "POST",
        headers: authHeaders({
          "Idempotency-Key": "cmd_approval.decided_01ARZ3NDEKTSV4RRFFQ69G5FAW",
        }),
        body: decisionBody(decidedPayload("approve", UNKNOWN_REQUEST_ID)),
      },
    );
    expect(unknown.status).toBe(404);

    const missingDecision = await fetch(
      `${fix.broker.url}/api/v1/approvals/${REQUEST_ID}/decision`,
      {
        method: "POST",
        headers: authHeaders({
          "Idempotency-Key": "cmd_approval.decided_01ARZ3NDEKTSV4RRFFQ69G5FAX",
        }),
        body: JSON.stringify({
          requestId: REQUEST_ID,
          decidedBy: "approver@example.com",
          decidedAt: "2026-05-18T11:01:00.000Z",
        }),
      },
    );
    expect(missingDecision.status).toBe(400);
    expect(((await missingDecision.json()) as { readonly error: string }).error).toBe(
      "invalid_payload",
    );
  });

  it("requires bearer auth and loopback host on every approvals route", async () => {
    if (fix === null) throw new Error("fixture missing");
    const routes = [
      { method: "POST", path: "/api/v1/approvals" },
      { method: "GET", path: "/api/v1/approvals" },
      { method: "GET", path: `/api/v1/approvals/${REQUEST_ID}` },
      { method: "POST", path: `/api/v1/approvals/${REQUEST_ID}/decision` },
    ] as const;
    for (const route of routes) {
      const noAuth = await rawRequest({
        port: fix.broker.port,
        method: route.method,
        path: route.path,
      });
      expect(noAuth.status).toBe(401);

      const badHost = await rawRequest({
        port: fix.broker.port,
        method: route.method,
        path: route.path,
        headers: {
          Authorization: `Bearer ${TOKEN}`,
          Host: "evil.example.com",
        },
      });
      expect(badHost.status).toBe(403);
    }
  });
});

describe("approval route SQLite error mapping", () => {
  afterEach(async () => {
    await teardown(currentFixture);
    currentFixture = null;
  });

  let currentFixture: Fixture | null = null;

  it("maps SQLITE_BUSY / LOCKED to 503 + Retry-After", async () => {
    currentFixture = await buildFixture({
      appender: (base) => ({
        ...base,
        requestApprovalIdempotent: () => {
          throw sqliteError("SQLITE_BUSY");
        },
      }),
    });
    const res = await postApproval(currentFixture);
    expect(res.status).toBe(503);
    expect(res.headers.get("Retry-After")).toBe("1");
    expect(await res.json()).toEqual({ error: "store_busy" });
  });

  it("maps SQLITE_FULL to 507", async () => {
    currentFixture = await buildFixture({
      appender: (base) => ({
        ...base,
        requestApprovalIdempotent: () => {
          throw sqliteError("SQLITE_FULL");
        },
      }),
    });
    const res = await postApproval(currentFixture);
    expect(res.status).toBe(507);
    expect(await res.json()).toEqual({ error: "store_full" });
  });
});

describe("approval routes when broker has no approvals config", () => {
  it("falls through to 404 for authenticated approvals paths", async () => {
    const broker = await createBroker({ port: 0, token: TOKEN });
    try {
      const res = await fetch(`${broker.url}/api/v1/approvals`, {
        headers: { Authorization: `Bearer ${TOKEN}` },
      });
      expect(res.status).toBe(404);
    } finally {
      await broker.stop();
    }
  });
});
