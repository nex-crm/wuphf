import { request as httpRequest, type OutgoingHttpHeaders } from "node:http";

import {
  type ApprovalDecidedAuditPayload,
  type ApprovalRequestedAuditPayload,
  type ApprovalStreamEvent,
  approvalDecisionRequestToJsonValue,
  approvalDecisionResponseFromJson,
  approvalGetResponseFromJson,
  approvalListResponseFromJson,
  approvalRequestCreateRequestToJsonValue,
  approvalRequestCreateResponseFromJson,
  asAgentId,
  asApiToken,
  asApprovalClaimId,
  asApprovalRequestId,
  asApprovalRole,
  asApprovalTokenId,
  asIdempotencyKey,
  asReceiptId,
  asSha256Hex,
  asSignerIdentity,
  asTaskId,
  asThreadId,
  asTimestampMs,
  routeErrorFromJson,
  type SignedApprovalToken,
  signedApprovalTokenFromJson,
  signedApprovalTokenToJsonValue,
  validateApprovalStreamEvent,
} from "@wuphf/protocol";
import BetterSqlite3 from "better-sqlite3";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import type { ApprovalAppender, ApprovalProjection } from "../../src/approvals/index.ts";
import { createApprovalSubsystem } from "../../src/approvals/index.ts";
import { createEventLog, openDatabase, runMigrations } from "../../src/event-log/index.ts";
import type { BrokerHandle } from "../../src/index.ts";
import { createBroker } from "../../src/index.ts";

const TOKEN = asApiToken("test-token-with-enough-entropy-AAAAAAAAA");
const REQUEST_ID = asApprovalRequestId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
const UNKNOWN_REQUEST_ID = asApprovalRequestId("01ARZ3NDEKTSV4RRFFQ69G5FAZ");
const RECEIPT_ID = asReceiptId("01BRZ3NDEKTSV4RRFFQ69G5FA0");
const THREAD_ID = asThreadId("01CRZ3NDEKTSV4RRFFQ69G5FA1");
const OTHER_THREAD_ID = asThreadId("01CRZ3NDEKTSV4RRFFQ69G5FA3");
const TASK_ID = asTaskId("01DRZ3NDEKTSV4RRFFQ69G5FA2");
const OTHER_TASK_ID = asTaskId("01DRZ3NDEKTSV4RRFFQ69G5FA4");
const FILTER_REQUEST_ID_1 = asApprovalRequestId("01FRZ3NDEKTSV4RRFFQ69G5FA5");
const FILTER_REQUEST_ID_2 = asApprovalRequestId("01GRZ3NDEKTSV4RRFFQ69G5FA6");
const FILTER_REQUEST_ID_3 = asApprovalRequestId("01HRZ3NDEKTSV4RRFFQ69G5FA7");
const ROUTE_NOW_MS = Date.UTC(2026, 4, 18, 11, 1, 0, 0);
const REQUEST_KEY = asIdempotencyKey("approval-request-01");
const DECISION_KEY = asIdempotencyKey("approval-decision-01");

interface FixtureOverrides {
  readonly appender?: (base: ApprovalAppender) => ApprovalAppender;
  readonly projection?: (base: ApprovalProjection) => ApprovalProjection;
  readonly tokenAgentIds?: ReadonlyMap<typeof TOKEN, ReturnType<typeof asAgentId>>;
}

interface Fixture {
  readonly db: ReturnType<typeof openDatabase>;
  readonly broker: BrokerHandle;
  readonly appender: ApprovalAppender;
  readonly projection: ApprovalProjection;
}

function requestedPayload(
  requestId = REQUEST_ID,
  overrides: {
    readonly threadId?: ReturnType<typeof asThreadId>;
    readonly taskId?: ReturnType<typeof asTaskId>;
  } = {},
): ApprovalRequestedAuditPayload {
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
    threadId: overrides.threadId ?? THREAD_ID,
    taskId: overrides.taskId ?? TASK_ID,
    receiptId: RECEIPT_ID,
    requestedBy: asSignerIdentity("operator@example.com"),
    requestedAt: new Date("2026-05-18T11:00:00.000Z"),
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
    decidedAt: new Date("2026-05-18T11:01:00.000Z"),
    ...(token === undefined ? {} : { token }),
  };
}

function signedApprovalTokenFixture(
  requested = requestedPayload(),
  tokenId = asApprovalTokenId("01ERZ3NDEKTSV4RRFFQ69G5FA3"),
): SignedApprovalToken {
  const token: SignedApprovalToken = {
    schemaVersion: 1,
    tokenId,
    claim: requested.claim,
    scope: requested.scope,
    notBefore: asTimestampMs(Date.UTC(2026, 4, 18, 11, 0, 0, 0)),
    expiresAt: asTimestampMs(Date.UTC(2026, 4, 18, 11, 30, 0, 0)),
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

async function buildFixture(overrides?: FixtureOverrides): Promise<Fixture> {
  const db = openDatabase({ path: ":memory:" });
  runMigrations(db);
  const eventLog = createEventLog(db);
  const { appender: baseAppender, projection: baseProjection } = createApprovalSubsystem(
    db,
    eventLog,
  );
  const projection = overrides?.projection?.(baseProjection) ?? baseProjection;
  const appender = overrides?.appender?.(baseAppender) ?? baseAppender;
  const broker = await createBroker({
    port: 0,
    token: TOKEN,
    clock: { now: () => ROUTE_NOW_MS },
    approvals: {
      appender,
      projection,
      tokenAgentIds: overrides?.tokenAgentIds ?? new Map([[TOKEN, asAgentId("agent_alpha")]]),
    },
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

function requestBody(payload: ApprovalRequestedAuditPayload, idempotencyKey = REQUEST_KEY): string {
  return JSON.stringify(
    approvalRequestCreateRequestToJsonValue({
      schemaVersion: 1,
      claim: payload.claim,
      scope: payload.scope,
      riskClass: payload.riskClass,
      threadId: payload.threadId,
      taskId: payload.taskId,
      receiptId: payload.receiptId,
      idempotencyKey,
    }),
  );
}

function decisionBody(payload: ApprovalDecidedAuditPayload, idempotencyKey = DECISION_KEY): string {
  return JSON.stringify(
    approvalDecisionRequestToJsonValue({
      schemaVersion: 1,
      decision: payload.decision,
      token: payload.decision === "approve" ? payload.token : undefined,
      idempotencyKey,
    }),
  );
}

function approveDecisionBodyWithoutToken(idempotencyKey = DECISION_KEY): string {
  return JSON.stringify(
    approvalDecisionRequestToJsonValue({
      schemaVersion: 1,
      decision: "approve",
      idempotencyKey,
    }),
  );
}

async function postApproval(fix: Fixture, idempotencyKey = REQUEST_KEY) {
  return await fetch(`${fix.broker.url}/api/v1/approvals`, {
    method: "POST",
    headers: authHeaders(),
    body: requestBody(requestedPayload(), idempotencyKey),
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
    const createdEnvelope = approvalRequestCreateResponseFromJson((await create.json()) as unknown);
    const created = createdEnvelope.approvalRequest;
    expect(createdEnvelope.headLsn).toBe("v1:1");
    expect(created.status).toBe("pending");

    const listPending = await fetch(`${fix.broker.url}/api/v1/approvals`, {
      headers: { Authorization: `Bearer ${TOKEN}` },
    });
    expect(listPending.status).toBe(200);
    const pendingBody = approvalListResponseFromJson((await listPending.json()) as unknown);
    expect(pendingBody.approvals.map((item) => item.id)).toEqual([created.id]);

    const get = await fetch(`${fix.broker.url}/api/v1/approvals/${created.id}`, {
      headers: { Authorization: `Bearer ${TOKEN}` },
    });
    expect(get.status).toBe(200);
    expect(approvalGetResponseFromJson((await get.json()) as unknown).approval.status).toBe(
      "pending",
    );

    const decided = await fetch(`${fix.broker.url}/api/v1/approvals/${created.id}/decision`, {
      method: "POST",
      headers: authHeaders(),
      body: decisionBody(decidedPayload("approve")),
    });
    expect(decided.status).toBe(201);
    const decidedBody = approvalDecisionResponseFromJson(
      (await decided.json()) as unknown,
    ).approvalRequest;
    expect(decidedBody.status).toBe("approved");
    expect(decidedBody.decision?.decision).toBe("approve");
    expect(decidedBody.decision?.decidedBy).toBe("agent_alpha");

    const getDecided = await fetch(`${fix.broker.url}/api/v1/approvals/${created.id}`, {
      headers: { Authorization: `Bearer ${TOKEN}` },
    });
    const getDecidedJson = (await getDecided.json()) as unknown;
    expect(JSON.stringify(getDecidedJson)).not.toContain("token");
    const getDecidedBody = approvalGetResponseFromJson(getDecidedJson).approval;
    expect(getDecidedBody.decisionSummary?.decision).toBe("approve");
    expect(getDecidedBody.decisionSummary?.decidedBy).toBe("agent_alpha");
  });

  it("paginates approval list responses with token-redacted ApprovalView items", async () => {
    if (fix === null) throw new Error("fixture missing");
    const first = approvalRequestCreateResponseFromJson(
      (await (await postApproval(fix, asIdempotencyKey("approval-page-01"))).json()) as unknown,
    ).approvalRequest;
    const second = approvalRequestCreateResponseFromJson(
      (await (await postApproval(fix, asIdempotencyKey("approval-page-02"))).json()) as unknown,
    ).approvalRequest;
    const third = approvalRequestCreateResponseFromJson(
      (await (await postApproval(fix, asIdempotencyKey("approval-page-03"))).json()) as unknown,
    ).approvalRequest;

    const firstPage = await fetch(`${fix.broker.url}/api/v1/approvals?limit=2`, {
      headers: { Authorization: `Bearer ${TOKEN}` },
    });
    expect(firstPage.status).toBe(200);
    const firstPageBody = approvalListResponseFromJson((await firstPage.json()) as unknown);
    expect(firstPageBody.approvals.map((approval) => approval.id)).toEqual([first.id, second.id]);
    expect(firstPageBody.nextCursor).toBe("v1:2");

    const secondPage = await fetch(
      `${fix.broker.url}/api/v1/approvals?limit=2&cursor=${firstPageBody.nextCursor}`,
      { headers: { Authorization: `Bearer ${TOKEN}` } },
    );
    expect(secondPage.status).toBe(200);
    const secondPageBody = approvalListResponseFromJson((await secondPage.json()) as unknown);
    expect(secondPageBody.approvals.map((approval) => approval.id)).toEqual([third.id]);
    expect(secondPageBody.nextCursor).toBeUndefined();
  });

  it("filters approvals by status, thread, and task across mixed rows", async () => {
    if (fix === null) throw new Error("fixture missing");
    const fixture = fix;
    const firstPayload = requestedPayload(FILTER_REQUEST_ID_1, {
      threadId: THREAD_ID,
      taskId: TASK_ID,
    });
    const secondPayload = requestedPayload(FILTER_REQUEST_ID_2, {
      threadId: OTHER_THREAD_ID,
      taskId: TASK_ID,
    });
    const thirdPayload = requestedPayload(FILTER_REQUEST_ID_3, {
      threadId: OTHER_THREAD_ID,
      taskId: OTHER_TASK_ID,
    });

    const create = async (payload: ApprovalRequestedAuditPayload) => {
      const res = await fetch(`${fixture.broker.url}/api/v1/approvals`, {
        method: "POST",
        headers: authHeaders(),
        body: requestBody(payload, asIdempotencyKey(payload.requestId)),
      });
      expect(res.status).toBe(201);
      return approvalRequestCreateResponseFromJson((await res.json()) as unknown).approvalRequest;
    };

    const first = await create(firstPayload);
    const second = await create(secondPayload);
    const third = await create(thirdPayload);
    const rejectSecond = await fetch(
      `${fixture.broker.url}/api/v1/approvals/${second.id}/decision`,
      {
        method: "POST",
        headers: authHeaders(),
        body: decisionBody(
          decidedPayload("reject", second.id),
          asIdempotencyKey("approval-filter-decision-02"),
        ),
      },
    );
    expect(rejectSecond.status).toBe(201);

    const pending = await fetch(`${fixture.broker.url}/api/v1/approvals?status=pending`, {
      headers: { Authorization: `Bearer ${TOKEN}` },
    });
    expect(pending.status).toBe(200);
    expect(
      approvalListResponseFromJson((await pending.json()) as unknown).approvals.map(
        (approval) => approval.id,
      ),
    ).toEqual([first.id, third.id]);

    const byThread = await fetch(
      `${fixture.broker.url}/api/v1/approvals?threadId=${OTHER_THREAD_ID}`,
      {
        headers: { Authorization: `Bearer ${TOKEN}` },
      },
    );
    expect(byThread.status).toBe(200);
    expect(
      approvalListResponseFromJson((await byThread.json()) as unknown).approvals.map(
        (approval) => approval.id,
      ),
    ).toEqual([third.id, second.id]);

    const byTask = await fetch(`${fixture.broker.url}/api/v1/approvals?taskId=${TASK_ID}`, {
      headers: { Authorization: `Bearer ${TOKEN}` },
    });
    expect(byTask.status).toBe(200);
    expect(
      approvalListResponseFromJson((await byTask.json()) as unknown).approvals.map(
        (approval) => approval.id,
      ),
    ).toEqual([first.id, second.id]);
  });

  it("uses a ULID create idempotency key as the approval request id", async () => {
    if (fix === null) throw new Error("fixture missing");

    const created = await postApproval(fix, asIdempotencyKey(REQUEST_ID));

    expect(created.status).toBe(201);
    expect(
      approvalRequestCreateResponseFromJson((await created.json()) as unknown).approvalRequest.id,
    ).toBe(REQUEST_ID);
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
    const createdApproval = approvalRequestCreateResponseFromJson(
      (await created.json()) as unknown,
    ).approvalRequest;
    const requestedText = await readUntil(reader, "event: approval.requested");
    const requestedEvent = parseApprovalSse(requestedText, "approval.requested");
    expect(validateApprovalStreamEvent(requestedEvent).ok).toBe(true);
    expect(requestedEvent.payload.requestId).toBe(createdApproval.id);
    expect(requestedEvent.payload.threadId).toBe(THREAD_ID);
    expect(requestedEvent.payload.headLsn).toBe("v1:1");

    const decided = await fetch(
      `${fix.broker.url}/api/v1/approvals/${createdApproval.id}/decision`,
      {
        method: "POST",
        headers: authHeaders(),
        body: decisionBody(decidedPayload("reject")),
      },
    );
    expect(decided.status).toBe(201);
    const decidedText = await readUntil(reader, "event: approval.decided");
    const decidedEvent = parseApprovalSse(decidedText, "approval.decided");
    expect(validateApprovalStreamEvent(decidedEvent).ok).toBe(true);
    expect(decidedEvent.payload.headLsn).toBe("v1:2");
    controller.abort();
  });

  it("duplicate command keys replay without appending duplicate events", async () => {
    if (fix === null) throw new Error("fixture missing");
    const requestKey = asIdempotencyKey("approval-request-dup");
    const decisionKey = asIdempotencyKey("approval-decision-dup");
    const first = await postApproval(fix, requestKey);
    const firstBody = await first.text();
    const created = approvalRequestCreateResponseFromJson(
      JSON.parse(firstBody) as unknown,
    ).approvalRequest;
    const second = await postApproval(fix, requestKey);
    expect(second.status).toBe(201);
    expect(second.headers.get("Idempotent-Replay")).toBe("true");
    expect(await second.text()).toBe(firstBody);
    expect(eventCount(fix.db, "approval.requested")).toBe(1);

    const firstDecision = await fetch(`${fix.broker.url}/api/v1/approvals/${created.id}/decision`, {
      method: "POST",
      headers: authHeaders(),
      body: decisionBody(decidedPayload("approve"), decisionKey),
    });
    expect(firstDecision.status).toBe(201);
    const firstDecisionBody = await firstDecision.text();
    const replayedDecision = await fetch(
      `${fix.broker.url}/api/v1/approvals/${created.id}/decision`,
      {
        method: "POST",
        headers: authHeaders(),
        body: decisionBody(decidedPayload("approve"), decisionKey),
      },
    );
    expect(replayedDecision.status).toBe(201);
    expect(replayedDecision.headers.get("Idempotent-Replay")).toBe("true");
    expect(await replayedDecision.text()).toBe(firstDecisionBody);
    expect(eventCount(fix.db, "approval.decided")).toBe(1);
  });

  it("rejects decision idempotency key reuse across approval paths", async () => {
    if (fix === null) throw new Error("fixture missing");
    const decisionKey = asIdempotencyKey("approval-decision-cross-resource");
    const firstCreated = approvalRequestCreateResponseFromJson(
      (await (
        await postApproval(fix, asIdempotencyKey("approval-request-first"))
      ).json()) as unknown,
    ).approvalRequest;
    const secondCreated = approvalRequestCreateResponseFromJson(
      (await (
        await postApproval(fix, asIdempotencyKey("approval-request-second"))
      ).json()) as unknown,
    ).approvalRequest;

    const firstDecision = await fetch(
      `${fix.broker.url}/api/v1/approvals/${firstCreated.id}/decision`,
      {
        method: "POST",
        headers: authHeaders(),
        body: decisionBody(decidedPayload("reject", firstCreated.id), decisionKey),
      },
    );
    expect(firstDecision.status).toBe(201);

    const secondDecision = await fetch(
      `${fix.broker.url}/api/v1/approvals/${secondCreated.id}/decision`,
      {
        method: "POST",
        headers: authHeaders(),
        body: decisionBody(decidedPayload("reject", secondCreated.id), decisionKey),
      },
    );
    expect(secondDecision.status).toBe(409);
    expect(await secondDecision.json()).toEqual({ error: "idempotency_key_conflict" });
    expect(eventCount(fix.db, "approval.decided")).toBe(1);
    expect(fix.projection.getById(secondCreated.id)?.approval.status).toBe("pending");
  });

  it("returns 409 for a second decision with a fresh key and appends no event", async () => {
    if (fix === null) throw new Error("fixture missing");
    const created = approvalRequestCreateResponseFromJson(
      (await (await postApproval(fix)).json()) as unknown,
    ).approvalRequest;
    const first = await fetch(`${fix.broker.url}/api/v1/approvals/${created.id}/decision`, {
      method: "POST",
      headers: authHeaders(),
      body: decisionBody(decidedPayload("approve")),
    });
    expect(first.status).toBe(201);
    const second = await fetch(`${fix.broker.url}/api/v1/approvals/${created.id}/decision`, {
      method: "POST",
      headers: authHeaders(),
      body: decisionBody(decidedPayload("reject"), asIdempotencyKey("approval-decision-02")),
    });
    expect(second.status).toBe(409);
    expect(await second.json()).toEqual({ error: "approval_not_pending" });
    expect(eventCount(fix.db, "approval.decided")).toBe(1);
  });

  it("returns 409 when an approve token is reused on another approval", async () => {
    if (fix === null) throw new Error("fixture missing");
    const sharedRequest = requestedPayload(REQUEST_ID);
    const sharedToken = signedApprovalTokenFixture(sharedRequest);
    const first = approvalRequestCreateResponseFromJson(
      (await (
        await fetch(`${fix.broker.url}/api/v1/approvals`, {
          method: "POST",
          headers: authHeaders(),
          body: requestBody(sharedRequest, asIdempotencyKey("approval-token-reuse-01")),
        })
      ).json()) as unknown,
    ).approvalRequest;
    const second = approvalRequestCreateResponseFromJson(
      (await (
        await fetch(`${fix.broker.url}/api/v1/approvals`, {
          method: "POST",
          headers: authHeaders(),
          body: requestBody(sharedRequest, asIdempotencyKey("approval-token-reuse-02")),
        })
      ).json()) as unknown,
    ).approvalRequest;

    const firstDecision = await fetch(`${fix.broker.url}/api/v1/approvals/${first.id}/decision`, {
      method: "POST",
      headers: authHeaders(),
      body: decisionBody(
        {
          ...decidedPayload("approve", first.id),
          token: sharedToken,
        },
        asIdempotencyKey("approval-token-reuse-decision-01"),
      ),
    });
    expect(firstDecision.status).toBe(201);

    const secondDecision = await fetch(`${fix.broker.url}/api/v1/approvals/${second.id}/decision`, {
      method: "POST",
      headers: authHeaders(),
      body: decisionBody(
        {
          ...decidedPayload("approve", second.id),
          token: sharedToken,
        },
        asIdempotencyKey("approval-token-reuse-decision-02"),
      ),
    });
    expect(secondDecision.status).toBe(409);
    expect(await secondDecision.json()).toEqual({ error: "approval_token_reused" });
    expect(eventCount(fix.db, "approval.decided")).toBe(1);
    expect(fix.projection.getById(second.id)?.approval.status).toBe("pending");
  });

  it("rejects decisions when the bearer cannot be resolved to an agent", async () => {
    await teardown(fix);
    fix = await buildFixture({ tokenAgentIds: new Map() });
    const created = approvalRequestCreateResponseFromJson(
      (await (await postApproval(fix)).json()) as unknown,
    ).approvalRequest;

    const decided = await fetch(`${fix.broker.url}/api/v1/approvals/${created.id}/decision`, {
      method: "POST",
      headers: authHeaders(),
      body: decisionBody(decidedPayload("reject", created.id)),
    });

    expect(decided.status).toBe(403);
    expect(await decided.json()).toEqual({ error: "approval_actor_unresolved" });
    expect(eventCount(fix.db, "approval.decided")).toBe(0);
  });

  it("rejects unknown approvals and missing decision payloads", async () => {
    if (fix === null) throw new Error("fixture missing");
    const unknown = await fetch(
      `${fix.broker.url}/api/v1/approvals/${UNKNOWN_REQUEST_ID}/decision`,
      {
        method: "POST",
        headers: authHeaders(),
        body: decisionBody(decidedPayload("reject", UNKNOWN_REQUEST_ID)),
      },
    );
    expect(unknown.status).toBe(404);

    const missingDecision = await fetch(
      `${fix.broker.url}/api/v1/approvals/${REQUEST_ID}/decision`,
      {
        method: "POST",
        headers: authHeaders(),
        body: JSON.stringify({
          schemaVersion: 1,
          idempotencyKey: "approval-decision-missing",
        }),
      },
    );
    expect(missingDecision.status).toBe(422);
    expect(routeErrorFromJson((await missingDecision.json()) as unknown).error).toBe(
      "invalid_payload",
    );
  });

  it("rejects approve decisions that omit the signed approval token", async () => {
    if (fix === null) throw new Error("fixture missing");
    const created = approvalRequestCreateResponseFromJson(
      (await (await postApproval(fix)).json()) as unknown,
    ).approvalRequest;

    const missingToken = await fetch(`${fix.broker.url}/api/v1/approvals/${created.id}/decision`, {
      method: "POST",
      headers: authHeaders(),
      body: approveDecisionBodyWithoutToken(asIdempotencyKey("approval-decision-no-token")),
    });

    expect(missingToken.status).toBe(422);
    expect(routeErrorFromJson((await missingToken.json()) as unknown).error).toBe(
      "invalid_payload",
    );
    expect(eventCount(fix.db, "approval.decided")).toBe(0);
  });

  it("returns 400 for malformed approval JSON before route-envelope validation", async () => {
    if (fix === null) throw new Error("fixture missing");

    const malformed = await fetch(`${fix.broker.url}/api/v1/approvals`, {
      method: "POST",
      headers: authHeaders(),
      body: "{",
    });

    expect(malformed.status).toBe(400);
    expect(routeErrorFromJson((await malformed.json()) as unknown).error).toBe("malformed_json");
  });

  it("returns 422 route errors for semantic approval command validation failures", async () => {
    if (fix === null) throw new Error("fixture missing");
    const valid = requestedPayload();
    const invalidCreate = await fetch(`${fix.broker.url}/api/v1/approvals`, {
      method: "POST",
      headers: authHeaders(),
      body: JSON.stringify({
        schemaVersion: 1,
        claim: valid.claim,
        scope: valid.scope,
        riskClass: "not-a-risk-class",
        idempotencyKey: "approval-invalid-create",
      }),
    });
    expect(invalidCreate.status).toBe(422);
    expect(routeErrorFromJson((await invalidCreate.json()) as unknown).error).toBe(
      "invalid_payload",
    );

    const invalidDecision = await fetch(
      `${fix.broker.url}/api/v1/approvals/${REQUEST_ID}/decision`,
      {
        method: "POST",
        headers: authHeaders(),
        body: JSON.stringify({
          schemaVersion: 1,
          decision: "defer",
          idempotencyKey: "approval-invalid-decision",
        }),
      },
    );
    expect(invalidDecision.status).toBe(422);
    expect(routeErrorFromJson((await invalidDecision.json()) as unknown).error).toBe(
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

  const routeCases = (code: string) =>
    [
      {
        name: "create",
        overrides: {
          appender: (base) => ({
            ...base,
            requestApprovalIdempotent: () => {
              throw sqliteError(code);
            },
          }),
        },
        request: (fixture) => postApproval(fixture),
      },
      {
        name: "decide",
        overrides: {
          appender: (base) => ({
            ...base,
            decideApprovalIdempotent: () => {
              throw sqliteError(code);
            },
          }),
        },
        request: (fixture) =>
          fetch(`${fixture.broker.url}/api/v1/approvals/${REQUEST_ID}/decision`, {
            method: "POST",
            headers: authHeaders(),
            body: decisionBody(decidedPayload("reject", REQUEST_ID)),
          }),
      },
      {
        name: "list",
        overrides: {
          projection: (base) => ({
            ...base,
            listPage: () => {
              throw sqliteError(code);
            },
          }),
        },
        request: (fixture) =>
          fetch(`${fixture.broker.url}/api/v1/approvals`, {
            headers: { Authorization: `Bearer ${TOKEN}` },
          }),
      },
      {
        name: "get",
        overrides: {
          projection: (base) => ({
            ...base,
            getById: () => {
              throw sqliteError(code);
            },
          }),
        },
        request: (fixture) =>
          fetch(`${fixture.broker.url}/api/v1/approvals/${REQUEST_ID}`, {
            headers: { Authorization: `Bearer ${TOKEN}` },
          }),
      },
    ] satisfies readonly {
      readonly name: string;
      readonly overrides: FixtureOverrides;
      readonly request: (fixture: Fixture) => Promise<Response>;
    }[];

  for (const code of ["SQLITE_BUSY", "SQLITE_LOCKED"] as const) {
    for (const routeCase of routeCases(code)) {
      it(`maps ${code} on ${routeCase.name} to 503 + Retry-After`, async () => {
        currentFixture = await buildFixture(routeCase.overrides);
        const res = await routeCase.request(currentFixture);
        expect(res.status).toBe(503);
        expect(res.headers.get("Retry-After")).toBe("1");
        expect(routeErrorFromJson((await res.json()) as unknown)).toEqual({
          error: "store_busy",
          retryAfterMs: 1000,
        });
      });
    }
  }

  for (const routeCase of routeCases("SQLITE_FULL")) {
    it(`maps SQLITE_FULL on ${routeCase.name} to 507`, async () => {
      currentFixture = await buildFixture(routeCase.overrides);
      const res = await routeCase.request(currentFixture);
      expect(res.status).toBe(507);
      expect(res.headers.get("Retry-After")).toBeNull();
      expect(routeErrorFromJson((await res.json()) as unknown)).toEqual({ error: "store_full" });
    });
  }
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
