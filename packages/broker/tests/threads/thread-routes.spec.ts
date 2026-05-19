import { request as httpRequest, type OutgoingHttpHeaders } from "node:http";

import {
  asAgentSlug,
  asApiToken,
  asProviderKind,
  asReceiptId,
  asTaskId,
  asThreadId,
  type EventLsn,
  type ReceiptSnapshot,
  SanitizedString,
  sha256Hex,
  threadSpecContentHash,
  validateThreadStreamEvent,
} from "@wuphf/protocol";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { createEventLog, openDatabase, runMigrations } from "../../src/event-log/index.ts";
import { type BrokerHandle, createBroker } from "../../src/index.ts";
import { InMemoryReceiptStore } from "../../src/receipt-store.ts";
import {
  createThreadAppender,
  createThreadStateStore,
  type ThreadAppender,
  type ThreadStateStore,
} from "../../src/threads/index.ts";

const TOKEN = asApiToken("thread-test-token-with-enough-entropy-A");
const THREAD_ID = "01ARZ3NDEKTSV4RRFFQ69G5FAZ";
const CREATE_REVISION_ID = "01BRZ3NDEKTSV4RRFFQ69G5FB0";
const CREATE_KEY = `cmd_thread.create_${CREATE_REVISION_ID}`;
const SPEC_REVISION_ID = "01CRZ3NDEKTSV4RRFFQ69G5FC0";
const SPEC_KEY = "cmd_thread.spec.edit_01DRZ3NDEKTSV4RRFFQ69G5FD0";
const STATUS_KEY = "cmd_thread.status.change_01ERZ3NDEKTSV4RRFFQ69G5FE0";
const TERMINAL_KEY = "cmd_thread.status.change_01FRZ3NDEKTSV4RRFFQ69G5FF0";
const INITIAL_CONTENT = { goal: "route threads", version: 1 };

interface Fixture {
  readonly broker: BrokerHandle;
  readonly db: ReturnType<typeof openDatabase>;
  readonly state: ThreadStateStore;
  readonly appender: ThreadAppender;
  readonly receiptStore: InMemoryReceiptStore;
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
    fixture.db.close();
    fixture = null;
  }
});

async function setup(): Promise<Fixture> {
  const db = openDatabase({ path: ":memory:" });
  runMigrations(db);
  const eventLog = createEventLog(db);
  const state = createThreadStateStore(db);
  const appender = createThreadAppender(db, eventLog, state);
  const receiptStore = new InMemoryReceiptStore();
  const broker = await createBroker({
    port: 0,
    token: TOKEN,
    receiptStore,
    threads: { appender, state },
  });
  return { broker, db, state, appender, receiptStore };
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
    threadId: THREAD_ID,
    title: "Thread foundation route",
    createdBy: "operator@example.com",
    createdAt: "2026-05-18T10:00:00.000Z",
    externalRefs: { sourceUrls: ["https://example.com/thread"], entityIds: ["entity:route"] },
    content: INITIAL_CONTENT,
  };
}

function specBody() {
  const content = { goal: "route threads", version: 2 };
  return {
    revisionId: SPEC_REVISION_ID,
    baseRevisionId: CREATE_REVISION_ID,
    baseContentHash: threadSpecContentHash(INITIAL_CONTENT),
    content,
    contentHash: threadSpecContentHash(content),
    authoredBy: "operator@example.com",
    authoredAt: "2026-05-18T10:05:00.000Z",
  };
}

function statusBody(fromStatus = "open", toStatus = "closed") {
  return {
    fromStatus,
    toStatus,
    changedBy: "operator@example.com",
    changedAt: "2026-05-18T10:10:00.000Z",
  };
}

async function createThread(fix: Fixture): Promise<{ readonly headLsn: EventLsn }> {
  const res = await fetch(`${fix.broker.url}/api/v1/threads`, {
    method: "POST",
    headers: jsonHeaders({ "Idempotency-Key": CREATE_KEY }),
    body: JSON.stringify(createBody()),
  });
  expect(res.status).toBe(201);
  return (await res.json()) as { readonly headLsn: EventLsn };
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

function minimalReceiptV1(id: string, taskId: string): ReceiptSnapshot {
  return {
    id: asReceiptId(id),
    agentSlug: asAgentSlug("agent"),
    taskId: asTaskId(taskId),
    triggerKind: "human_message",
    triggerRef: "message",
    startedAt: new Date("2026-05-18T09:00:00.000Z"),
    finishedAt: new Date("2026-05-18T09:01:00.000Z"),
    status: "ok",
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
    error: SanitizedString.fromUnknown(""),
    notebookWrites: [],
    wikiWrites: [],
    schemaVersion: 1,
  };
}

function minimalReceiptV2(id: string, taskId: string): ReceiptSnapshot {
  return {
    ...minimalReceiptV1(id, taskId),
    schemaVersion: 2,
    threadId: asThreadId(THREAD_ID),
  };
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

describe("/api/v1/threads routes", () => {
  it("requires bearer auth for every thread route", async () => {
    if (fixture === null) throw new Error("fixture missing");
    const routeChecks = [
      fetch(`${fixture.broker.url}/api/v1/threads`),
      fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}`),
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
    expect(responses.map((res) => res.status)).toEqual([401, 401, 401, 401, 401]);
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
    expect(checks.map((res) => res.status)).toEqual([403, 403, 403, 403, 403]);
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
    const listBody = (await list.json()) as {
      readonly threads: readonly { readonly thread: { readonly thread_id: string } }[];
    };
    expect(listBody.threads.map((entry) => entry.thread.thread_id)).toEqual([THREAD_ID]);

    const get = await fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}`, {
      headers: authHeaders(),
    });
    expect(get.status).toBe(200);
    const getBody = (await get.json()) as {
      readonly thread: {
        readonly status: string;
        readonly spec: { readonly revision_id: string };
        readonly task_ids: readonly string[];
      };
      readonly receipt_ids: readonly string[];
    };
    expect(getBody.thread.status).toBe("open");
    expect(getBody.thread.spec.revision_id).toBe(CREATE_REVISION_ID);
    expect(getBody.thread.task_ids).toEqual([taskA, taskB]);
    expect(getBody.receipt_ids).toEqual([
      "01JRZ3NDEKTSV4RRFFQ69G5FJ0",
      "01KRZ3NDEKTSV4RRFFQ69G5FK0",
      "01SRZ3NDEKTSV4RRFFQ69G5FS0",
    ]);

    const spec = await fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}/spec`, {
      method: "PATCH",
      headers: jsonHeaders({ "Idempotency-Key": SPEC_KEY }),
      body: JSON.stringify(specBody()),
    });
    expect(spec.status).toBe(200);

    const status = await fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}/status`, {
      method: "PATCH",
      headers: jsonHeaders({ "Idempotency-Key": STATUS_KEY }),
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

  it("maps stale spec bases to 409 and terminal status exits to 422", async () => {
    if (fixture === null) throw new Error("fixture missing");
    await createThread(fixture);
    const good = await fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}/spec`, {
      method: "PATCH",
      headers: jsonHeaders({ "Idempotency-Key": SPEC_KEY }),
      body: JSON.stringify(specBody()),
    });
    expect(good.status).toBe(200);

    const stale = await fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}/spec`, {
      method: "PATCH",
      headers: jsonHeaders({
        "Idempotency-Key": "cmd_thread.spec.edit_01MRZ3NDEKTSV4RRFFQ69G5FM0",
      }),
      body: JSON.stringify({
        ...specBody(),
        revisionId: "01NRZ3NDEKTSV4RRFFQ69G5FN0",
      }),
    });
    expect(stale.status).toBe(409);
    expect((await stale.json()) as unknown).toMatchObject({ error: "stale_spec_base" });

    const terminal = await fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}/status`, {
      method: "PATCH",
      headers: jsonHeaders({ "Idempotency-Key": STATUS_KEY }),
      body: JSON.stringify(statusBody("open", "closed")),
    });
    expect(terminal.status).toBe(200);

    const out = await fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}/status`, {
      method: "PATCH",
      headers: jsonHeaders({ "Idempotency-Key": TERMINAL_KEY }),
      body: JSON.stringify(statusBody("closed", "merged")),
    });
    expect(out.status).toBe(422);
    expect((await out.json()) as unknown).toMatchObject({ error: "terminal_status_transition" });
  });

  it("replays duplicate idempotency keys without duplicate thread events", async () => {
    if (fixture === null) throw new Error("fixture missing");
    const first = await fetch(`${fixture.broker.url}/api/v1/threads`, {
      method: "POST",
      headers: jsonHeaders({ "Idempotency-Key": CREATE_KEY }),
      body: JSON.stringify(createBody()),
    });
    expect(first.status).toBe(201);
    const firstText = await first.text();

    const second = await fetch(`${fixture.broker.url}/api/v1/threads`, {
      method: "POST",
      headers: jsonHeaders({ "Idempotency-Key": CREATE_KEY }),
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
    expect(count?.count).toBe(2);
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
      kind: "thread.created",
      payload: { threadId: THREAD_ID, headLsn: "v1:2" },
    });

    const spec = await fetch(`${fixture.broker.url}/api/v1/threads/${THREAD_ID}/spec`, {
      method: "PATCH",
      headers: jsonHeaders({ "Idempotency-Key": SPEC_KEY }),
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
      kind: "thread.updated",
      payload: { threadId: THREAD_ID, headLsn: "v1:3" },
    });
  });
});
