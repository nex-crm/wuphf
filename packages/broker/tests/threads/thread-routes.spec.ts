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
import { constructSqliteReceiptStoreForTesting } from "../../src/internal/sqlite-receipt-store-testing.ts";
import { MAX_LIST_LIMIT } from "../../src/receipt-store.ts";
import type { SqliteReceiptStore } from "../../src/sqlite-receipt-store.ts";
import {
  createThreadSubsystem,
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
const INITIAL_CONTENT = { goal: "route threads", version: 1 };
const ULID_TIME_PREFIX = "01ZRZ3NDEK";
const ULID_ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";

interface Fixture {
  readonly broker: BrokerHandle;
  readonly db: ReturnType<typeof openDatabase>;
  readonly subsystem: ThreadSubsystem;
  readonly state: ThreadStateStore;
  readonly appender: ThreadAppender;
  readonly receiptStore: SqliteReceiptStore;
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
  const { state, appender } = subsystem;
  const broker = await createBroker({
    port: 0,
    token: TOKEN,
    threads: subsystem,
  });
  return { broker, db, subsystem, state, appender, receiptStore };
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
      readonly threads: readonly { readonly thread_id: string }[];
    };
    expect(listBody.threads.map((thread) => thread.thread_id)).toEqual([THREAD_ID]);

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
    };
    expect(getBody.thread.status).toBe("open");
    expect(getBody.thread.spec.revision_id).toBe(CREATE_REVISION_ID);
    expect(getBody.thread.task_ids).toEqual([taskA, taskB]);

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
    expect(count?.count).toBe(2);
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
      id: "v1:2",
      kind: "thread.created",
      payload: { threadId: THREAD_ID, headLsn: "v1:2" },
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
      id: "v1:3",
      kind: "thread.updated",
      payload: { threadId: THREAD_ID, headLsn: "v1:3" },
    });
  });
});
