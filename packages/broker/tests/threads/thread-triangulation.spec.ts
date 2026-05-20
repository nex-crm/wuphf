import {
  asAgentSlug,
  asApiToken,
  asProviderKind,
  asReceiptId,
  asTaskId,
  asThreadId,
  type EventLsn,
  MAX_ROUTE_THREAD_LIST_ITEMS,
  type ReceiptSnapshot,
  receiptToJson,
  SanitizedString,
  sha256Hex,
  threadListResponseFromJson,
  threadMutationResponseFromJson,
  validateThreadStreamEvent,
} from "@wuphf/protocol";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { createEventLog, openDatabase, runMigrations } from "../../src/event-log/index.ts";
import { type BrokerHandle, createBroker } from "../../src/index.ts";
import { constructSqliteReceiptStoreForTesting } from "../../src/internal/sqlite-receipt-store-testing.ts";
import type { SqliteReceiptStore } from "../../src/sqlite-receipt-store.ts";
import { createThreadSubsystem, SYSTEM_INBOX_THREAD_ID } from "../../src/threads/index.ts";

const TOKEN = asApiToken("thread-triangulation-test-token-AAAAA");
const ULID_TIME_PREFIX = "01ZRZ3NDEK";
const ULID_ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";
const SSE_NEGATIVE_TIMEOUT_MS = 100;

interface Fixture {
  readonly broker: BrokerHandle;
  readonly db: ReturnType<typeof openDatabase>;
  readonly receiptStore: SqliteReceiptStore;
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
  const threads = createThreadSubsystem(db, eventLog, receiptStore);
  const broker = await createBroker({
    port: 0,
    token: TOKEN,
    threads,
  });
  return { broker, db, receiptStore };
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

function indexedUlid(index: number): string {
  let value = index;
  let suffix = "";
  for (let i = 0; i < 16; i += 1) {
    suffix = ULID_ALPHABET[value % ULID_ALPHABET.length] + suffix;
    value = Math.floor(value / ULID_ALPHABET.length);
  }
  return `${ULID_TIME_PREFIX}${suffix}`;
}

function threadCreateBody(index: number) {
  const idempotencyKey = indexedUlid(10_000 + index);
  return {
    title: `Thread cap ${index}`,
    specContent: { goal: "prove route cap", index },
    externalRefs: { source_urls: [], entity_ids: [] },
    idempotencyKey,
  };
}

async function createThread(fix: Fixture, index: number): Promise<ReturnType<typeof asThreadId>> {
  const body = threadCreateBody(index);
  const response = await fetch(`${fix.broker.url}/api/v1/threads`, {
    method: "POST",
    headers: jsonHeaders(),
    body: JSON.stringify(body),
  });
  expect(response.status).toBe(201);
  const created = threadMutationResponseFromJson((await response.json()) as unknown);
  expect(created.threadId).toBe(body.idempotencyKey);
  return created.threadId;
}

function minimalReceiptV1(
  id: string,
  taskId: ReturnType<typeof asTaskId>,
  status: ReceiptSnapshot["status"] = "ok",
): ReceiptSnapshot {
  return {
    id: asReceiptId(id),
    agentSlug: asAgentSlug("agent"),
    taskId,
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
  threadId: ReturnType<typeof asThreadId>,
  taskId: ReturnType<typeof asTaskId>,
  status: ReceiptSnapshot["status"] = "ok",
): ReceiptSnapshot {
  return {
    ...minimalReceiptV1(id, taskId, status),
    schemaVersion: 2,
    threadId,
  };
}

const sseReaderBuffers = new WeakMap<ReadableStreamDefaultReader<Uint8Array>, string>();

async function readUntil(
  reader: ReadableStreamDefaultReader<Uint8Array>,
  needle: string,
): Promise<string> {
  let text = sseReaderBuffers.get(reader) ?? "";
  for (let i = 0; i < 20; i += 1) {
    const buffered = takeBufferedSse(text, needle);
    text = buffered.remaining;
    if (buffered.match !== null) {
      sseReaderBuffers.set(reader, buffered.remaining);
      return buffered.match;
    }
    const chunk = await reader.read();
    if (chunk.done) break;
    text += new TextDecoder().decode(chunk.value);
  }
  sseReaderBuffers.set(reader, text);
  throw new Error(`SSE stream did not include ${needle}`);
}

async function readUntilWithin(
  reader: ReadableStreamDefaultReader<Uint8Array>,
  needle: string,
  timeoutMs: number,
): Promise<string | null> {
  let timedOut = false;
  let timeout: ReturnType<typeof setTimeout> | undefined;
  const timeoutPromise = new Promise<null>((resolve) => {
    timeout = setTimeout(() => {
      timedOut = true;
      resolve(null);
    }, timeoutMs);
  });
  const readPromise = readUntil(reader, needle).catch((err: unknown) => {
    if (timedOut) return null;
    throw err;
  });
  try {
    return await Promise.race([readPromise, timeoutPromise]);
  } finally {
    if (timeout !== undefined) clearTimeout(timeout);
  }
}

function takeBufferedSse(
  text: string,
  needle: string,
): { readonly match: string | null; readonly remaining: string } {
  let cursor = 0;
  for (;;) {
    const blockEnd = text.indexOf("\n\n", cursor);
    if (blockEnd === -1) {
      return { match: null, remaining: text.slice(cursor) };
    }
    const nextCursor = blockEnd + 2;
    if (text.slice(cursor, nextCursor).includes(needle)) {
      return { match: text.slice(0, nextCursor), remaining: text.slice(nextCursor) };
    }
    cursor = nextCursor;
  }
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

async function postReceipt(fix: Fixture, receipt: ReceiptSnapshot): Promise<Response> {
  return await fetch(`${fix.broker.url}/api/receipts`, {
    method: "POST",
    headers: jsonHeaders(),
    body: receiptToJson(receipt),
  });
}

function latestReceiptPutLsn(fix: Fixture): EventLsn {
  const row = fix.db
    .prepare<[], { readonly lsn: number }>(
      "SELECT lsn FROM event_log WHERE type = 'receipt.put' ORDER BY lsn DESC LIMIT 1",
    )
    .get();
  if (row === undefined) throw new Error("missing receipt.put event");
  return `v1:${row.lsn}` as EventLsn;
}

async function expectNoThreadUpdated(
  reader: ReadableStreamDefaultReader<Uint8Array>,
): Promise<void> {
  await expect(
    readUntilWithin(reader, "event: thread.updated", SSE_NEGATIVE_TIMEOUT_MS),
  ).resolves.toBeNull();
}

async function openSse(fix: Fixture, controller: AbortController) {
  return await openSseForBroker(fix.broker, controller);
}

async function openSseForBroker(broker: BrokerHandle, controller: AbortController) {
  const events = await fetch(`${broker.url}/api/events`, {
    headers: { Authorization: `Bearer ${TOKEN}`, Accept: "text/event-stream" },
    signal: controller.signal,
  });
  expect(events.status).toBe(200);
  const reader = events.body?.getReader();
  expect(reader).toBeDefined();
  if (reader === undefined) throw new Error("missing SSE reader");
  await readUntil(reader, "event: ready");
  return reader;
}

describe("thread triangulation route coverage", () => {
  it("clamps thread list pages to MAX_ROUTE_THREAD_LIST_ITEMS before encoding", async () => {
    if (fixture === null) throw new Error("fixture missing");
    const createdThreadIds: string[] = [];
    for (let index = 0; index < MAX_ROUTE_THREAD_LIST_ITEMS + 1; index += 1) {
      createdThreadIds.push(await createThread(fixture, index));
    }

    const defaultResponse = await fetch(`${fixture.broker.url}/api/v1/threads`, {
      headers: authHeaders(),
    });
    expect(defaultResponse.status).toBe(200);
    const defaultBody = threadListResponseFromJson((await defaultResponse.json()) as unknown);
    expect(defaultBody.threads).toHaveLength(MAX_ROUTE_THREAD_LIST_ITEMS);
    expect(defaultBody.nextCursor).toBeDefined();

    const highLimitResponse = await fetch(`${fixture.broker.url}/api/v1/threads?limit=9999`, {
      headers: authHeaders(),
    });
    expect(highLimitResponse.status).toBe(200);
    const highLimitBody = threadListResponseFromJson((await highLimitResponse.json()) as unknown);
    expect(highLimitBody.threads).toHaveLength(MAX_ROUTE_THREAD_LIST_ITEMS);
    expect(highLimitBody.threads.map((thread) => thread.id)).toEqual(
      defaultBody.threads.map((thread) => thread.id),
    );
    expect(highLimitBody.nextCursor).toBe(defaultBody.nextCursor);

    const cursor = defaultBody.nextCursor;
    if (cursor === undefined) throw new Error("missing default nextCursor");
    const defaultRemainderResponse = await fetch(
      `${fixture.broker.url}/api/v1/threads?cursor=${encodeURIComponent(cursor)}`,
      { headers: authHeaders() },
    );
    expect(defaultRemainderResponse.status).toBe(200);
    const defaultRemainderBody = threadListResponseFromJson(
      (await defaultRemainderResponse.json()) as unknown,
    );
    expect(defaultRemainderBody.threads).toHaveLength(2);
    expect(defaultRemainderBody.nextCursor).toBeUndefined();

    const highLimitCursor = highLimitBody.nextCursor;
    if (highLimitCursor === undefined) throw new Error("missing high-limit nextCursor");
    const highLimitRemainderResponse = await fetch(
      `${fixture.broker.url}/api/v1/threads?limit=9999&cursor=${encodeURIComponent(
        highLimitCursor,
      )}`,
      { headers: authHeaders() },
    );
    expect(highLimitRemainderResponse.status).toBe(200);
    const highLimitRemainderBody = threadListResponseFromJson(
      (await highLimitRemainderResponse.json()) as unknown,
    );
    expect(highLimitRemainderBody.threads.map((thread) => thread.id)).toEqual(
      defaultRemainderBody.threads.map((thread) => thread.id),
    );
    expect(highLimitRemainderBody.nextCursor).toBeUndefined();

    const listedIds = [
      ...defaultBody.threads.map((thread) => thread.id),
      ...defaultRemainderBody.threads.map((thread) => thread.id),
    ];
    expect(new Set(listedIds).size).toBe(MAX_ROUTE_THREAD_LIST_ITEMS + 2);
    expect(listedIds).toEqual([SYSTEM_INBOX_THREAD_ID, ...createdThreadIds]);
  });

  it.each([
    "error",
    "stalled",
    "ok",
  ] as const)("emits exactly one receipt-backed thread.updated invalidation for %s receipts", async (status) => {
    if (fixture === null) throw new Error("fixture missing");
    const controller = new AbortController();
    try {
      const reader = await openSse(fixture, controller);
      const threadId = await createThread(fixture, 30_000);
      await readUntil(reader, "event: thread.created");

      const receipt = minimalReceiptV2(
        indexedUlid(31_000),
        threadId,
        asTaskId(indexedUlid(32_000)),
        status,
      );
      const created = await postReceipt(fixture, receipt);
      expect(created.status).toBe(201);
      const committedLsn = latestReceiptPutLsn(fixture);
      const text = await readUntil(reader, "event: thread.updated");
      const event = parseThreadSse(text, "thread.updated");
      expect(validateThreadStreamEvent(event).ok).toBe(true);
      expect(event).toMatchObject({
        id: committedLsn,
        kind: "thread.updated",
        payload: { threadId, headLsn: committedLsn },
      });

      const duplicate = await postReceipt(fixture, receipt);
      expect(duplicate.status).toBe(409);
      expect(await duplicate.json()).toEqual({ error: "receipt_id_exists", id: receipt.id });
      await expectNoThreadUpdated(reader);
    } finally {
      controller.abort();
    }
  });

  it("does not emit thread.updated invalidations for unthreaded V1 receipts", async () => {
    if (fixture === null) throw new Error("fixture missing");
    const controller = new AbortController();
    try {
      const reader = await openSse(fixture, controller);
      const receipt = minimalReceiptV1(indexedUlid(40_000), asTaskId(indexedUlid(41_000)), "ok");

      const created = await postReceipt(fixture, receipt);
      expect(created.status).toBe(201);
      await expectNoThreadUpdated(reader);
    } finally {
      controller.abort();
    }
  });

  it("does not emit thread.updated invalidations for threaded V2 receipts without threads mounted", async () => {
    const broker = await createBroker({ port: 0, token: TOKEN });
    const controller = new AbortController();
    try {
      const reader = await openSseForBroker(broker, controller);
      const receipt = minimalReceiptV2(
        indexedUlid(50_000),
        asThreadId(indexedUlid(50_001)),
        asTaskId(indexedUlid(50_002)),
        "ok",
      );

      const created = await fetch(`${broker.url}/api/receipts`, {
        method: "POST",
        headers: jsonHeaders(),
        body: receiptToJson(receipt),
      });
      expect(created.status).toBe(201);
      await expectNoThreadUpdated(reader);
    } finally {
      controller.abort();
      await broker.stop();
    }
  });
});
