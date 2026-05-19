import { request as httpRequest, type OutgoingHttpHeaders } from "node:http";

import {
  approvalDecisionRequestToJsonValue,
  approvalDecisionResponseFromJson,
  approvalRequestCreateRequestToJsonValue,
  approvalRequestCreateResponseFromJson,
  asAgentId,
  asAgentSlug,
  asApiToken,
  asApprovalClaimId,
  asApprovalRequestId,
  asApprovalRole,
  asIdempotencyKey,
  asProviderKind,
  asReceiptId,
  asTaskId,
  asThreadId,
  type EventLsn,
  type ReceiptSnapshot,
  SanitizedString,
  sha256Hex,
  threadGetResponseFromJson,
  threadListResponseFromJson,
  threadPinnedApprovalsResponseFromJson,
  threadSpecContentHash,
  validateThreadStreamEvent,
} from "@wuphf/protocol";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import {
  type ApprovalAppender,
  type ApprovalProjection,
  createApprovalSubsystem,
} from "../../src/approvals/index.ts";
import type { EventLog } from "../../src/event-log/index.ts";
import { createEventLog, openDatabase, runMigrations } from "../../src/event-log/index.ts";
import { type BrokerHandle, createBroker } from "../../src/index.ts";
import { constructSqliteReceiptStoreForTesting } from "../../src/internal/sqlite-receipt-store-testing.ts";
import { MAX_LIST_LIMIT } from "../../src/receipt-store.ts";
import type { SqliteReceiptStore } from "../../src/sqlite-receipt-store.ts";
import {
  createThreadSubsystem,
  SYSTEM_INBOX_THREAD_ID,
  type ThreadAppender,
  type ThreadStateStore,
  type ThreadSubsystem,
} from "../../src/threads/index.ts";

const TOKEN = asApiToken("thread-test-token-with-enough-entropy-A");
const CREATE_REVISION_ID = "01BRZ3NDEKTSV4RRFFQ69G5FB0";
const THREAD_ID = CREATE_REVISION_ID;
const CREATE_KEY = CREATE_REVISION_ID;
const SPEC_REVISION_ID = "01CRZ3NDEKTSV4RRFFQ69G5FC0";
const SPEC_KEY = SPEC_REVISION_ID;
const STATUS_KEY = "01ERZ3NDEKTSV4RRFFQ69G5FE0";
const TERMINAL_KEY = "01FRZ3NDEKTSV4RRFFQ69G5FF0";
const APPROVAL_REQUEST_ID = asApprovalRequestId("01MRZ3NDEKTSV4RRFFQ69G5FM0");
const INBOX_APPROVAL_REQUEST_ID = asApprovalRequestId("01NRZ3NDEKTSV4RRFFQ69G5FN0");
const APPROVAL_DECISION_KEY = asIdempotencyKey("thread-approval-decision-01");
const APPROVAL_RECEIPT_ID = asReceiptId("01PRZ3NDEKTSV4RRFFQ69G5FP0");
const APPROVAL_TASK_ID = asTaskId("01QRZ3NDEKTSV4RRFFQ69G5FQ0");
const APPROVAL_CLAIM_ID = asApprovalClaimId("claim_thread_route");
const APPROVAL_FROZEN_ARGS_HASH = sha256Hex("thread-route-approval-frozen-args");
const INITIAL_CONTENT = { goal: "route threads", version: 1 };
const ULID_TIME_PREFIX = "01ZRZ3NDEK";
const ULID_ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";

interface Fixture {
  readonly broker: BrokerHandle;
  readonly db: ReturnType<typeof openDatabase>;
  readonly eventLog: EventLog;
  readonly subsystem: ThreadSubsystem;
  readonly state: ThreadStateStore;
  readonly appender: ThreadAppender;
  readonly receiptStore: SqliteReceiptStore;
  readonly approvalAppender: ApprovalAppender;
  readonly approvalProjection: ApprovalProjection;
}

interface RawResponse {
  readonly status: number;
  readonly body: string;
}

interface RawHeaders {
  Host?: string;
  Authorization?: string;
  "Content-Type"?: string;
  "Content-Length"?: string;
}

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

async function setup(): Promise<Fixture> {
  const db = openDatabase({ path: ":memory:" });
  runMigrations(db);
  const eventLog = createEventLog(db);
  const receiptStore = constructSqliteReceiptStoreForTesting(db, eventLog);
  const subsystem = createThreadSubsystem(db, eventLog, receiptStore);
  const approvals = createApprovalSubsystem(db, eventLog);
  const { state, appender } = subsystem;
  const broker = await createBroker({
    port: 0,
    token: TOKEN,
    threads: subsystem,
    approvals: {
      appender: approvals.appender,
      projection: approvals.projection,
      tokenAgentIds: new Map([[TOKEN, asAgentId("agent_alpha")]]),
    },
  });
  return {
    broker,
    db,
    eventLog,
    subsystem,
    state,
    appender,
    receiptStore,
    approvalAppender: approvals.appender,
    approvalProjection: approvals.projection,
  };
}

function authHeaders(extra: Record<string, string> = {}): Record<string, string> {
  return {
    Authorization: `Bearer ${TOKEN}`,
    ...extra,
  };
}

function jsonHeaders(extra: Record<string, string> = {}): Record<string, string> {
  return authHeaders({ "Content-Type": "application/json", ...extra });
}

function createBody() {
  return {
    title: "Thread foundation route",
    specContent: INITIAL_CONTENT,
    externalRefs: { source_urls: ["https://example.com/thread"], entity_ids: ["entity:route"] },
    idempotencyKey: CREATE_KEY,
  };
}

function specBody() {
  const content = { goal: "route threads", version: 2 };
  return {
    baseRevisionId: CREATE_REVISION_ID,
    baseContentHash: threadSpecContentHash(INITIAL_CONTENT),
    content,
    idempotencyKey: SPEC_KEY,
  };
}

function statusBody(fromStatus = "open", toStatus = "closed", idempotencyKey = STATUS_KEY) {
  return {
    fromStatus,
    toStatus,
    idempotencyKey,
  };
}

function approvalRequestBody(args: {
  readonly requestId: ReturnType<typeof asApprovalRequestId>;
  readonly threadId?: ReturnType<typeof asThreadId>;
  readonly taskId?: ReturnType<typeof asTaskId>;
}): string {
  const claim = {
    schemaVersion: 1,
    claimId: APPROVAL_CLAIM_ID,
    kind: "receipt_co_sign",
    receiptId: APPROVAL_RECEIPT_ID,
    frozenArgsHash: APPROVAL_FROZEN_ARGS_HASH,
    riskClass: "critical",
  } as const;
  const scope = {
    mode: "single_use",
    claimId: APPROVAL_CLAIM_ID,
    claimKind: "receipt_co_sign",
    role: asApprovalRole("approver"),
    maxUses: 1,
    receiptId: APPROVAL_RECEIPT_ID,
    frozenArgsHash: APPROVAL_FROZEN_ARGS_HASH,
  } as const;
  return JSON.stringify(
    approvalRequestCreateRequestToJsonValue({
      schemaVersion: 1,
      claim,
      scope,
      riskClass: "critical",
      ...(args.threadId === undefined ? {} : { threadId: args.threadId }),
      ...(args.taskId === undefined ? {} : { taskId: args.taskId }),
      receiptId: APPROVAL_RECEIPT_ID,
      idempotencyKey: asIdempotencyKey(args.requestId),
    }),
  );
}

function approvalDecisionBody(idempotencyKey = APPROVAL_DECISION_KEY): string {
  return JSON.stringify(
    approvalDecisionRequestToJsonValue({
      schemaVersion: 1,
      decision: "reject",
      idempotencyKey,
    }),
  );
}

async function createApproval(
  fix: Fixture,
  args: {
    readonly requestId?: ReturnType<typeof asApprovalRequestId>;
    readonly threadId?: ReturnType<typeof asThreadId>;
    readonly taskId?: ReturnType<typeof asTaskId>;
  } = {},
) {
  const requestId = args.requestId ?? APPROVAL_REQUEST_ID;
  const res = await fetch(`${fix.broker.url}/api/v1/approvals`, {
    method: "POST",
    headers: jsonHeaders(),
    body: approvalRequestBody({
      requestId,
      ...(args.threadId === undefined ? {} : { threadId: args.threadId }),
      ...(args.taskId === undefined ? {} : { taskId: args.taskId }),
    }),
  });
  expect(res.status).toBe(201);
  return approvalRequestCreateResponseFromJson((await res.json()) as unknown);
}

async function createThread(fix: Fixture): Promise<{ readonly headLsn: EventLsn }> {
  const res = await fetch(`${fix.broker.url}/api/v1/threads`, {
    method: "POST",
    headers: jsonHeaders(),
    body: JSON.stringify(createBody()),
  });
  expect(res.status).toBe(201);
  const body = (await res.json()) as {
    readonly headLsn: EventLsn;
    readonly revisionId: string;
    readonly contentHash: string;
  };
  expect(body.revisionId).toBe(CREATE_REVISION_ID);
  expect(body.contentHash).toBe(threadSpecContentHash(INITIAL_CONTENT));
  return body;
}

function rawRequest(args: {
  readonly port: number;
  readonly path: string;
  readonly method: string;
  readonly hostHeader?: string;
  readonly authorization?: string;
  readonly body?: string;
}): Promise<RawResponse> {
  return new Promise((resolveFn, rejectFn) => {
    const headers: RawHeaders = {};
    if (args.hostHeader !== undefined) headers.Host = args.hostHeader;
    if (args.authorization !== undefined) headers.Authorization = args.authorization;
    if (args.body !== undefined) {
      headers["Content-Type"] = "application/json";
      headers["Content-Length"] = String(Buffer.byteLength(args.body));
    }
    const req = httpRequest(
      {
        host: "127.0.0.1",
        port: args.port,
        path: args.path,
        method: args.method,
        headers: headers as OutgoingHttpHeaders,
      },
      (res) => {
        const chunks: Buffer[] = [];
        res.on("data", (chunk: Buffer) => chunks.push(chunk));
        res.on("end", () => {
          resolveFn({
            status: res.statusCode ?? 0,
            body: Buffer.concat(chunks).toString("utf8"),
          });
        });
      },
    );
    req.on("error", rejectFn);
    if (args.body !== undefined) req.write(args.body);
    req.end();
  });
}

function minimalReceiptV1(
  id: string,
  taskId: string,
  status: ReceiptSnapshot["status"] = "ok",
): ReceiptSnapshot {
  return {
    id: asReceiptId(id),
    agentSlug: asAgentSlug("agent"),
    taskId: asTaskId(taskId),
    triggerKind: "human_message",
    triggerRef: "message",
    startedAt: new Date("2026-05-18T09:00:00.000Z"),
    finishedAt: new Date("2026-05-18T09:01:00.000Z"),
    status,
    providerKind: asProviderKind("anthropic"),
    model: "claude-opus-4-7",
    promptHash: sha256Hex("prompt"),
    toolManifest: sha256Hex("tools"),
    toolCalls: [],
    approvals: [],
    filesChanged: [],
    commits: [],
    sourceReads: [],
    writes: [],
    inputTokens: 0,
    outputTokens: 0,
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
    costUsd: 0,
    finalMessage: SanitizedString.fromUnknown(""),
    error: SanitizedString.fromUnknown(status === "error" ? "receipt failed" : ""),
    notebookWrites: [],
    wikiWrites: [],
    schemaVersion: 1,
  };
}

function minimalReceiptV2(
  id: string,
  taskId: string,
  status: ReceiptSnapshot["status"] = "ok",
): ReceiptSnapshot {
  return {
    ...minimalReceiptV1(id, taskId, status),
    schemaVersion: 2,
    threadId: asThreadId(THREAD_ID),
  };
}

function indexedReceiptId(index: number): string {
  let value = index;
  let suffix = "";
  for (let i = 0; i < 16; i += 1) {
    suffix = ULID_ALPHABET[value % ULID_ALPHABET.length] + suffix;
    value = Math.floor(value / ULID_ALPHABET.length);
  }
  return `${ULID_TIME_PREFIX}${suffix}`;
}

async function readUntil(reader: ReadableStreamDefaultReader<Uint8Array>, needle: string) {
  let text = "";
  for (let i = 0; i < 20; i += 1) {
    const chunk = await reader.read();
    if (chunk.done) break;
    text += new TextDecoder().decode(chunk.value);
    if (text.includes(needle)) return text;
  }
  throw new Error(`SSE stream did not include ${needle}`);
}

function parseThreadSse(
  text: string,
  kind: "thread.created" | "thread.updated" | "thread.pinned_approvals.changed",
) {
  const blocks = text.split("\n\n");
  for (const block of blocks) {
    if (!block.includes(`event: ${kind}`)) continue;
    const dataLine = block.split("\n").find((line) => line.startsWith("data: "));
    if (dataLine === undefined) throw new Error(`missing data line for ${kind}`);
    return JSON.parse(dataLine.slice("data: ".length)) as unknown;
  }
  throw new Error(`missing SSE event ${kind}`);
}

function nextLinkPath(headers: Headers): string {
  const link = headers.get("link");
  if (link === null) throw new Error("missing Link header");
  const match = /^<([^>]+)>; rel="next"$/.exec(link);
  if (match === null || match[1] === undefined) {
    throw new Error(`bad Link header: ${link}`);
  }
  return match[1];
}

describe("/api/v1/threads routes", () => {
  it("requires bearer auth for every thread route", async () => {
    if (fixture === null) throw new Error("fixture missing");
    const routeChecks = [
      fetch(`${fixture.broker.url}/api/v1/threads`),
      fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}`),
      fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}/pinned-approvals`),
      fetch(`${fixture.broker.url}/api/v1/threads`, {
        method: "POST",
        body: JSON.stringify(createBody()),
      }),
      fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}/spec`, {
        method: "PATCH",
        body: JSON.stringify(specBody()),
      }),
      fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}/status`, {
        method: "PATCH",
        body: JSON.stringify(statusBody()),
      }),
    ];
    const responses = await Promise.all(routeChecks);
    expect(responses.map((res) => res.status)).toEqual([401, 401, 401, 401, 401, 401]);
  });

  it("runs the loopback Host guard before every thread route", async () => {
    if (fixture === null) throw new Error("fixture missing");
    const body = JSON.stringify(createBody());
    const checks = await Promise.all([
      rawRequest({
        port: fixture.broker.port,
        path: "/api/v1/threads",
        method: "GET",
        hostHeader: "evil.example.com",
        authorization: `Bearer ${TOKEN}`,
      }),
      rawRequest({
        port: fixture.broker.port,
        path: `/api/v1/threads/${THREAD_ID}`,
        method: "GET",
        hostHeader: "evil.example.com",
        authorization: `Bearer ${TOKEN}`,
      }),
      rawRequest({
        port: fixture.broker.port,
        path: `/api/v1/threads/${THREAD_ID}/pinned-approvals`,
        method: "GET",
        hostHeader: "evil.example.com",
        authorization: `Bearer ${TOKEN}`,
      }),
      rawRequest({
        port: fixture.broker.port,
        path: "/api/v1/threads",
        method: "POST",
        hostHeader: "evil.example.com",
        authorization: `Bearer ${TOKEN}`,
        body,
      }),
      rawRequest({
        port: fixture.broker.port,
        path: `/api/v1/threads/${THREAD_ID}/spec`,
        method: "PATCH",
        hostHeader: "evil.example.com",
        authorization: `Bearer ${TOKEN}`,
        body: JSON.stringify(specBody()),
      }),
      rawRequest({
        port: fixture.broker.port,
        path: `/api/v1/threads/${THREAD_ID}/status`,
        method: "PATCH",
        hostHeader: "evil.example.com",
        authorization: `Bearer ${TOKEN}`,
        body: JSON.stringify(statusBody()),
      }),
    ]);
    expect(checks.map((res) => res.status)).toEqual([403, 403, 403, 403, 403, 403]);
  });

  it("creates, lists, reads, edits spec, changes status, and derives receipt indexes", async () => {
    if (fixture === null) throw new Error("fixture missing");
    await createThread(fixture);
    const taskA = "01GRZ3NDEKTSV4RRFFQ69G5FG0";
    const taskB = "01HRZ3NDEKTSV4RRFFQ69G5FH0";
    await fixture.receiptStore.put(minimalReceiptV2("01JRZ3NDEKTSV4RRFFQ69G5FJ0", taskA));
    await fixture.receiptStore.put(minimalReceiptV2("01KRZ3NDEKTSV4RRFFQ69G5FK0", taskB));
    await fixture.receiptStore.put(minimalReceiptV2("01SRZ3NDEKTSV4RRFFQ69G5FS0", taskA));

    const list = await fetch(`${fixture.broker.url}/api/v1/threads`, {
      headers: authHeaders(),
    });
    expect(list.status).toBe(200);
    const listBody = threadListResponseFromJson((await list.json()) as unknown);
    expect(listBody.threads.map((thread) => thread.id)).toEqual([
      SYSTEM_INBOX_THREAD_ID,
      THREAD_ID,
    ]);
    expect(listBody.threads.map((thread) => thread.effectiveStatus)).toEqual(["open", "open"]);
    expect(listBody.threads.map((thread) => thread.boardColumn)).toEqual(["running", "running"]);

    const get = await fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}`, {
      headers: authHeaders(),
    });
    expect(get.status).toBe(200);
    const getBody = threadGetResponseFromJson((await get.json()) as unknown);
    expect(getBody.thread.status).toBe("open");
    expect(getBody.thread.effectiveStatus).toBe("open");
    expect(getBody.thread.boardColumn).toBe("running");
    expect(getBody.thread.currentSeat).toBe("agent");
    expect(getBody.thread.pendingApprovalCount).toBe(0);
    expect(getBody.thread.spec.revisionId).toBe(CREATE_REVISION_ID);
    expect(getBody.thread.taskIds).toEqual([taskA, taskB]);

    const spec = await fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}/spec`, {
      method: "PATCH",
      headers: jsonHeaders(),
      body: JSON.stringify(specBody()),
    });
    expect(spec.status).toBe(200);
    expect((await spec.json()) as unknown).toMatchObject({
      revisionId: SPEC_REVISION_ID,
      contentHash: threadSpecContentHash({ goal: "route threads", version: 2 }),
    });

    const status = await fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}/status`, {
      method: "PATCH",
      headers: jsonHeaders(),
      body: JSON.stringify(statusBody("open", "closed")),
    });
    expect(status.status).toBe(200);

    const closed = await fetch(`${fixture.broker.url}/api/v1/threads?status=closed`, {
      headers: authHeaders(),
    });
    expect(closed.status).toBe(200);
    const closedBody = (await closed.json()) as { readonly threads: readonly unknown[] };
    expect(closedBody.threads).toHaveLength(1);
  });

  it("paginates canonical thread receipts and keeps thread task ids de-duped", async () => {
    if (fixture === null) throw new Error("fixture missing");
    await createThread(fixture);
    const taskA = "01GRZ3NDEKTSV4RRFFQ69G5FG0";
    const taskB = "01HRZ3NDEKTSV4RRFFQ69G5FH0";
    const expectedReceiptIds: string[] = [];
    for (let i = 1; i <= MAX_LIST_LIMIT + 1; i += 1) {
      const id = indexedReceiptId(i);
      expectedReceiptIds.push(id);
      await fixture.receiptStore.put(minimalReceiptV2(id, i % 2 === 0 ? taskB : taskA));
    }

    const get = await fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}`, {
      headers: authHeaders(),
    });
    expect(get.status).toBe(200);
    const getBody = (await get.json()) as { readonly thread: { readonly task_ids: string[] } };
    expect(getBody.thread.task_ids).toEqual([taskA, taskB]);

    const first = await fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}/receipts`, {
      headers: authHeaders(),
    });
    expect(first.status).toBe(200);
    const firstPage = (await first.json()) as Array<{ id: string }>;
    expect(firstPage.map((receipt) => receipt.id)).toEqual(
      expectedReceiptIds.slice(0, MAX_LIST_LIMIT),
    );
    const next = first.headers.get("link");
    expect(next).not.toBeNull();
    const second = await fetch(new URL(nextLinkPath(first.headers), fixture.broker.url), {
      headers: authHeaders(),
    });
    expect(second.status).toBe(200);
    const secondPage = (await second.json()) as Array<{ id: string }>;
    expect(secondPage.map((receipt) => receipt.id)).toEqual(
      expectedReceiptIds.slice(MAX_LIST_LIMIT),
    );
    expect(second.headers.get("link")).toBeNull();
  });

  it("maps stale spec bases to 409 and terminal status exits to 422", async () => {
    if (fixture === null) throw new Error("fixture missing");
    await createThread(fixture);
    const good = await fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}/spec`, {
      method: "PATCH",
      headers: jsonHeaders(),
      body: JSON.stringify(specBody()),
    });
    expect(good.status).toBe(200);

    const stale = await fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}/spec`, {
      method: "PATCH",
      headers: jsonHeaders(),
      body: JSON.stringify({
        ...specBody(),
        idempotencyKey: "01MRZ3NDEKTSV4RRFFQ69G5FM0",
      }),
    });
    expect(stale.status).toBe(409);
    expect((await stale.json()) as unknown).toMatchObject({ error: "stale_spec_base" });

    const terminal = await fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}/status`, {
      method: "PATCH",
      headers: jsonHeaders(),
      body: JSON.stringify(statusBody("open", "closed")),
    });
    expect(terminal.status).toBe(200);

    const out = await fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}/status`, {
      method: "PATCH",
      headers: jsonHeaders(),
      body: JSON.stringify(statusBody("closed", "merged", TERMINAL_KEY)),
    });
    expect(out.status).toBe(422);
    expect((await out.json()) as unknown).toMatchObject({ error: "terminal_status_transition" });
  });

  it("accepts exactly one concurrent HTTP spec edit for the same base revision", async () => {
    if (fixture === null) throw new Error("fixture missing");
    await createThread(fixture);
    const keys = [
      "01MRZ3NDEKTSV4RRFFQ69G5FM0",
      "01NRZ3NDEKTSV4RRFFQ69G5FN0",
      "01PRZ3NDEKTSV4RRFFQ69G5FP0",
      "01QRZ3NDEKTSV4RRFFQ69G5FQ0",
      "01RRZ3NDEKTSV4RRFFQ69G5FR0",
      "01SRZ3NDEKTSV4RRFFQ69G5FS0",
      "01TRZ3NDEKTSV4RRFFQ69G5FT0",
      "01VRZ3NDEKTSV4RRFFQ69G5FV0",
      "01WRZ3NDEKTSV4RRFFQ69G5FW0",
      "01XRZ3NDEKTSV4RRFFQ69G5FX0",
    ];
    const brokerUrl = fixture.broker.url;

    const responses = await Promise.all(
      keys.map((idempotencyKey, index) =>
        fetch(`${brokerUrl}/api/v1/threads/${THREAD_ID}/spec`, {
          method: "PATCH",
          headers: jsonHeaders(),
          body: JSON.stringify({
            ...specBody(),
            content: { goal: "route threads", attempt: index },
            idempotencyKey,
          }),
        }),
      ),
    );

    const statuses = responses.map((response) => response.status);
    expect(statuses.filter((status) => status === 200)).toHaveLength(1);
    expect(statuses.filter((status) => status === 409)).toHaveLength(9);
    const count = fixture.db
      .prepare<[], { readonly count: number }>(
        "SELECT COUNT(*) AS count FROM event_log WHERE type = 'thread.spec_edited'",
      )
      .get();
    expect(count?.count).toBe(3);
  });

  it("replays duplicate idempotency keys without duplicate thread events", async () => {
    if (fixture === null) throw new Error("fixture missing");
    const first = await fetch(`${fixture.broker.url}/api/v1/threads`, {
      method: "POST",
      headers: jsonHeaders(),
      body: JSON.stringify(createBody()),
    });
    expect(first.status).toBe(201);
    const firstText = await first.text();

    const second = await fetch(`${fixture.broker.url}/api/v1/threads`, {
      method: "POST",
      headers: jsonHeaders(),
      body: JSON.stringify(createBody()),
    });
    expect(second.status).toBe(201);
    expect(second.headers.get("Idempotent-Replay")).toBe("true");
    expect(await second.text()).toBe(firstText);

    const count = fixture.db
      .prepare<[], { readonly count: number }>(
        "SELECT COUNT(*) AS count FROM event_log WHERE type IN ('thread.created', 'thread.spec_edited')",
      )
      .get();
    expect(count?.count).toBe(4);
  });

  it("returns pinned approvals and derives effective status from the read-time query", async () => {
    if (fixture === null) throw new Error("fixture missing");
    await createThread(fixture);
    const created = await createApproval(fixture, {
      threadId: asThreadId(THREAD_ID),
      taskId: APPROVAL_TASK_ID,
    });

    const pinned = await fetch(
      `${fixture.broker.url}/api/v1/threads/${THREAD_ID}/pinned-approvals`,
      {
        headers: authHeaders(),
      },
    );
    expect(pinned.status).toBe(200);
    const pinnedBody = threadPinnedApprovalsResponseFromJson((await pinned.json()) as unknown);
    expect(pinnedBody.threadId).toBe(THREAD_ID);
    expect(pinnedBody.headLsn).toBe(created.headLsn);
    expect(pinnedBody.approvals.map((approval) => approval.id)).toEqual([APPROVAL_REQUEST_ID]);
    expect(pinnedBody.approvals[0]?.status).toBe("pending");
    expect(JSON.stringify(pinnedBody)).not.toContain("token");

    const attention = await fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}`, {
      headers: authHeaders(),
    });
    const attentionBody = threadGetResponseFromJson((await attention.json()) as unknown).thread;
    expect(attentionBody.effectiveStatus).toBe("needs_attention");
    expect(attentionBody.attentionReason).toBe("pending_approval");
    expect(attentionBody.boardColumn).toBe("needs_me");
    expect(attentionBody.currentSeat).toBe("human");
    expect(attentionBody.pendingApprovalCount).toBe(1);

    const byEffective = await fetch(`${fixture.broker.url}/api/v1/threads?status=needs_attention`, {
      headers: authHeaders(),
    });
    expect(
      threadListResponseFromJson((await byEffective.json()) as unknown).threads.map(
        (thread) => thread.id,
      ),
    ).toEqual([THREAD_ID]);

    const byColumn = await fetch(`${fixture.broker.url}/api/v1/threads?status=needs_me`, {
      headers: authHeaders(),
    });
    expect(
      threadListResponseFromJson((await byColumn.json()) as unknown).threads.map(
        (thread) => thread.id,
      ),
    ).toEqual([THREAD_ID]);

    const liveView = {
      effectiveStatus: attentionBody.effectiveStatus,
      attentionReason: attentionBody.attentionReason,
      boardColumn: attentionBody.boardColumn,
      currentSeat: attentionBody.currentSeat,
      pendingApprovalCount: attentionBody.pendingApprovalCount,
    };
    fixture.approvalProjection.rebuildFromLog(fixture.eventLog);
    fixture.subsystem.rebuildFromLog(0);
    const replayed = await fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}`, {
      headers: authHeaders(),
    });
    const replayedThread = threadGetResponseFromJson((await replayed.json()) as unknown).thread;
    expect({
      effectiveStatus: replayedThread.effectiveStatus,
      attentionReason: replayedThread.attentionReason,
      boardColumn: replayedThread.boardColumn,
      currentSeat: replayedThread.currentSeat,
      pendingApprovalCount: replayedThread.pendingApprovalCount,
    }).toEqual(liveView);

    const decided = await fetch(
      `${fixture.broker.url}/api/v1/approvals/${APPROVAL_REQUEST_ID}/decision`,
      {
        method: "POST",
        headers: jsonHeaders(),
        body: approvalDecisionBody(),
      },
    );
    expect(decided.status).toBe(201);
    const decidedBody = approvalDecisionResponseFromJson((await decided.json()) as unknown);

    const afterDecision = await fetch(
      `${fixture.broker.url}/api/v1/threads/${THREAD_ID}/pinned-approvals`,
      {
        headers: authHeaders(),
      },
    );
    const afterDecisionBody = threadPinnedApprovalsResponseFromJson(
      (await afterDecision.json()) as unknown,
    );
    expect(afterDecisionBody.headLsn).toBe(decidedBody.headLsn);
    expect(afterDecisionBody.approvals).toEqual([]);

    const cleared = await fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}`, {
      headers: authHeaders(),
    });
    const clearedThread = threadGetResponseFromJson((await cleared.json()) as unknown).thread;
    expect(clearedThread.effectiveStatus).toBe("open");
    expect(clearedThread.boardColumn).toBe("running");
    expect(clearedThread.currentSeat).toBe("agent");
    expect(clearedThread.pendingApprovalCount).toBe(0);
  });

  it("defaults threadless approval requests to the system inbox thread", async () => {
    if (fixture === null) throw new Error("fixture missing");
    const created = await createApproval(fixture, {
      requestId: INBOX_APPROVAL_REQUEST_ID,
      taskId: APPROVAL_TASK_ID,
    });
    expect(created.approvalRequest.threadId).toBe(SYSTEM_INBOX_THREAD_ID);

    const pinned = await fetch(
      `${fixture.broker.url}/api/v1/threads/${SYSTEM_INBOX_THREAD_ID}/pinned-approvals`,
      {
        headers: authHeaders(),
      },
    );
    expect(pinned.status).toBe(200);
    const pinnedBody = threadPinnedApprovalsResponseFromJson((await pinned.json()) as unknown);
    expect(pinnedBody.headLsn).toBe(created.headLsn);
    expect(pinnedBody.approvals.map((approval) => approval.id)).toEqual([
      INBOX_APPROVAL_REQUEST_ID,
    ]);
  });

  it("derives receipt error and stalled effective statuses from the latest thread receipt", async () => {
    if (fixture === null) throw new Error("fixture missing");
    await createThread(fixture);
    await fixture.receiptStore.put(
      minimalReceiptV2("01VRZ3NDEKTSV4RRFFQ69G5FV0", APPROVAL_TASK_ID, "error"),
    );
    const failed = await fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}`, {
      headers: authHeaders(),
    });
    const failedThread = threadGetResponseFromJson((await failed.json()) as unknown).thread;
    expect(failedThread.effectiveStatus).toBe("needs_attention");
    expect(failedThread.attentionReason).toBe("failed");
    expect(failedThread.boardColumn).toBe("needs_me");
    expect(failedThread.currentSeat).toBe("human");

    await fixture.receiptStore.put(
      minimalReceiptV2("01WRZ3NDEKTSV4RRFFQ69G5FW0", APPROVAL_TASK_ID, "stalled"),
    );
    const stalled = await fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}`, {
      headers: authHeaders(),
    });
    const stalledThread = threadGetResponseFromJson((await stalled.json()) as unknown).thread;
    expect(stalledThread.effectiveStatus).toBe("needs_attention");
    expect(stalledThread.attentionReason).toBe("stalled");
    expect(stalledThread.boardColumn).toBe("needs_me");
    expect(stalledThread.currentSeat).toBe("human");
  });

  it("emits validated invalidation-only SSE events for thread mutations", async () => {
    if (fixture === null) throw new Error("fixture missing");
    const controller = new AbortController();
    const events = await fetch(`${fixture.broker.url}/api/events`, {
      headers: { Authorization: `Bearer ${TOKEN}`, Accept: "text/event-stream" },
      signal: controller.signal,
    });
    expect(events.status).toBe(200);
    const reader = events.body?.getReader();
    expect(reader).toBeDefined();
    if (reader === undefined) throw new Error("missing SSE reader");
    await readUntil(reader, "event: ready");

    await createThread(fixture);
    const text = await readUntil(reader, "event: thread.created");

    const dataLine = text
      .split("\n")
      .find((line) => line.startsWith("data: ") && line.includes("thread.created"));
    expect(dataLine).toBeDefined();
    if (dataLine === undefined) return;
    const event = JSON.parse(dataLine.slice("data: ".length)) as unknown;
    expect(validateThreadStreamEvent(event).ok).toBe(true);
    expect(event).toMatchObject({
      id: "v1:4",
      kind: "thread.created",
      payload: { threadId: THREAD_ID, headLsn: "v1:4" },
    });

    const spec = await fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}/spec`, {
      method: "PATCH",
      headers: jsonHeaders(),
      body: JSON.stringify(specBody()),
    });
    expect(spec.status).toBe(200);
    const updatedText = await readUntil(reader, "event: thread.updated");
    controller.abort();
    const updatedDataLine = updatedText
      .split("\n")
      .find((line) => line.startsWith("data: ") && line.includes("thread.updated"));
    expect(updatedDataLine).toBeDefined();
    if (updatedDataLine === undefined) return;
    const updatedEvent = JSON.parse(updatedDataLine.slice("data: ".length)) as unknown;
    expect(validateThreadStreamEvent(updatedEvent).ok).toBe(true);
    expect(updatedEvent).toMatchObject({
      id: "v1:5",
      kind: "thread.updated",
      payload: { threadId: THREAD_ID, headLsn: "v1:5" },
    });
  });

  it("emits thread pinned-approvals invalidations on approval request and decision", async () => {
    if (fixture === null) throw new Error("fixture missing");
    const controller = new AbortController();
    const events = await fetch(`${fixture.broker.url}/api/events`, {
      headers: { Authorization: `Bearer ${TOKEN}`, Accept: "text/event-stream" },
      signal: controller.signal,
    });
    expect(events.status).toBe(200);
    const reader = events.body?.getReader();
    expect(reader).toBeDefined();
    if (reader === undefined) throw new Error("missing SSE reader");
    await readUntil(reader, "event: ready");

    await createThread(fixture);
    await readUntil(reader, "event: thread.created");

    const created = await createApproval(fixture, {
      threadId: asThreadId(THREAD_ID),
      taskId: APPROVAL_TASK_ID,
    });
    const requestedText = await readUntil(reader, "event: thread.pinned_approvals.changed");
    const requestedEvent = parseThreadSse(requestedText, "thread.pinned_approvals.changed");
    expect(validateThreadStreamEvent(requestedEvent).ok).toBe(true);
    expect(requestedEvent).toMatchObject({
      kind: "thread.pinned_approvals.changed",
      payload: { threadId: THREAD_ID, headLsn: created.headLsn },
    });

    const decided = await fetch(
      `${fixture.broker.url}/api/v1/approvals/${APPROVAL_REQUEST_ID}/decision`,
      {
        method: "POST",
        headers: jsonHeaders(),
        body: approvalDecisionBody(),
      },
    );
    expect(decided.status).toBe(201);
    const decidedBody = approvalDecisionResponseFromJson((await decided.json()) as unknown);
    const decidedText = await readUntil(reader, "event: thread.pinned_approvals.changed");
    controller.abort();
    const decidedEvent = parseThreadSse(decidedText, "thread.pinned_approvals.changed");
    expect(validateThreadStreamEvent(decidedEvent).ok).toBe(true);
    expect(decidedEvent).toMatchObject({
      kind: "thread.pinned_approvals.changed",
      payload: { threadId: THREAD_ID, headLsn: decidedBody.headLsn },
    });
  });
});
