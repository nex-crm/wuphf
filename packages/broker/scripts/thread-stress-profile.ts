import { mkdtempSync, rmSync } from "node:fs";
import { request as httpRequest } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";

import {
  type ApprovalRequestCreateRequest,
  type ApprovalRequestId,
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
  type JsonValue,
  type ReceiptSnapshot,
  SanitizedString,
  sha256Hex,
  type ThreadId,
  type ThreadMutationResponse,
  type ThreadStatus,
  threadGetResponseFromJson,
  threadMutationResponseFromJson,
  threadPinnedApprovalsResponseFromJson,
  threadReplayCheckReportToJsonValue,
  threadSpecContentHash,
} from "@wuphf/protocol";

import { createApprovalSubsystem } from "../src/approvals/index.ts";
import { createEventLog, openDatabase, runMigrations } from "../src/event-log/index.ts";
import { type BrokerHandle, createBroker } from "../src/index.ts";
import { SqliteReceiptStore } from "../src/sqlite-receipt-store.ts";
import {
  createThreadSubsystem,
  runThreadReplayCheck,
  SYSTEM_INBOX_THREAD_ID,
} from "../src/threads/index.ts";

const THREAD_COUNT = 50;
const SPEC_EDITS_PER_THREAD = 4;
const RECEIPT_COUNT = 200;
const CONCURRENT_SPEC_ATTEMPTS = 10;
const TOKEN = asApiToken("thread-stress-token-with-enough-entropy");
const AGENT_ID = asAgentId("agent_thread_stress");
const ULID_PREFIX = "01ARZ3NDEK";
const ULID_ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";
const BASE_CLOCK_MS = 1_800_000_000_000;
const TAMPER_DEFERRED_REF = "TODO(thread-audit-verify)";

interface StressThread {
  readonly id: ThreadId;
  revisionId: string;
  content: JsonValue;
  contentHash: string;
}

interface StressApproval {
  readonly id: ApprovalRequestId;
  readonly threadId: ThreadId;
}

interface JsonResponse {
  readonly status: number;
  readonly value: unknown;
}

interface SlowSseSession {
  readonly dropped: Promise<boolean>;
  close(): void;
}

let ulidCounter = 1_000;
let clockTick = 0;

async function main(): Promise<void> {
  const workspace = mkdtempSync(join(tmpdir(), "wuphf-thread-stress-"));
  let broker: BrokerHandle | null = null;
  let receiptStore: SqliteReceiptStore | null = null;

  try {
    const db = openDatabase({ path: join(workspace, "broker.sqlite") });
    runMigrations(db);
    const eventLog = createEventLog(db);
    receiptStore = SqliteReceiptStore.fromDatabase(db, eventLog);
    const threads = createThreadSubsystem(db, eventLog, receiptStore);
    const approvals = createApprovalSubsystem(db, eventLog);
    const tokenAgentIds = new Map([[TOKEN, AGENT_ID]]);
    const clock = { now: () => BASE_CLOCK_MS + clockTick++ };

    broker = await createBroker({
      port: 0,
      token: TOKEN,
      clock,
      threads,
      approvals: {
        appender: approvals.appender,
        projection: approvals.projection,
        tokenAgentIds,
      },
    });

    const createdThreads = await createInitialThreads(broker);
    const approvalsCreated = await addReceiptsAndApprovals(broker, receiptStore, createdThreads);
    await assertConcurrentSpecContention(broker, db, createdThreads[0] ?? fail("missing thread"));
    await decideHalfTheApprovals(broker, approvalsCreated);
    await assertPinnedApprovalCounts(broker, createdThreads, approvalsCreated);
    await pumpStatusesToTerminal(broker, createdThreads);

    await broker.stop();
    broker = null;
    assertReplayCheckOk(db, "post-stop replay-check");

    deferAuditChainTamperCheck();

    broker = await createBroker({
      port: 0,
      token: TOKEN,
      clock,
      threads,
      approvals: {
        appender: approvals.appender,
        projection: approvals.projection,
        tokenAgentIds,
      },
      sse: { maxWritableLength: 128 },
    });
    await assertSseInvalidationConvergence(broker);
    assertReplayCheckOk(db, "post-sse replay-check");

    console.log(
      `thread stress profile passed: ${createdThreads.length} threads, ` +
        `${RECEIPT_COUNT} receipts, ${approvalsCreated.length} approvals; ` +
        `tamper step deferred (${TAMPER_DEFERRED_REF})`,
    );
  } finally {
    if (broker !== null) {
      await broker.stop();
    }
    receiptStore?.close();
    rmSync(workspace, { recursive: true, force: true });
  }
}

async function createInitialThreads(broker: BrokerHandle): Promise<StressThread[]> {
  const threads: StressThread[] = [];
  for (let index = 0; index < THREAD_COUNT; index += 1) {
    const idempotencyKey = nextUlid();
    const initialContent: JsonValue = { thread: index, edit: 0, body: `thread-${index}` };
    const created = await postThreadCreate(broker, idempotencyKey, initialContent);
    const thread: StressThread = {
      id: asThreadId(created.threadId),
      revisionId: created.revisionId,
      content: initialContent,
      contentHash: created.contentHash,
    };
    for (let edit = 1; edit <= SPEC_EDITS_PER_THREAD; edit += 1) {
      const content: JsonValue = { thread: index, edit, body: `thread-${index}-edit-${edit}` };
      const edited = await patchThreadSpec(broker, thread, content, nextUlid(), 200);
      thread.revisionId = edited.revisionId;
      thread.content = content;
      thread.contentHash = edited.contentHash;
    }
    threads.push(thread);
  }
  return threads;
}

async function addReceiptsAndApprovals(
  broker: BrokerHandle,
  receiptStore: SqliteReceiptStore,
  threads: readonly StressThread[],
): Promise<StressApproval[]> {
  const approvals: StressApproval[] = [];
  for (let index = 0; index < RECEIPT_COUNT; index += 1) {
    const taskId = asTaskId(nextUlid());
    const receiptId = asReceiptId(nextUlid());
    const thread = threads[index % threads.length] ?? fail("thread list unexpectedly empty");
    const threadId = index % 17 === 0 ? undefined : thread.id;
    await receiptStore.put(minimalReceipt(receiptId, taskId, threadId));

    if (index % 10 < 3) {
      const approval = await postApprovalRequest(broker, {
        requestId: asApprovalRequestId(nextUlid()),
        receiptId,
        taskId,
        threadId,
      });
      approvals.push({
        id: approval.id,
        threadId: approval.threadId ?? SYSTEM_INBOX_THREAD_ID,
      });
    }
  }
  return approvals;
}

async function assertConcurrentSpecContention(
  broker: BrokerHandle,
  db: ReturnType<typeof openDatabase>,
  thread: StressThread,
): Promise<void> {
  const before = countEvents(db, "thread.spec_edited");
  const baseRevisionId = thread.revisionId;
  const baseContentHash = thread.contentHash;
  const responses = await Promise.all(
    Array.from({ length: CONCURRENT_SPEC_ATTEMPTS }, async (_, index) => {
      const content: JsonValue = { thread: "contention", attempt: index };
      return patchThreadSpecRaw(broker, thread.id, {
        baseRevisionId,
        baseContentHash,
        content,
        idempotencyKey: nextUlid(),
      });
    }),
  );
  const statuses = responses.map((response) => response.status);
  assert(
    statuses.filter((status) => status === 200).length === 1,
    `expected one accepted spec edit, got statuses ${statuses.join(",")}`,
  );
  assert(
    statuses.filter((status) => status === 409).length === 9,
    `expected nine rejected spec edits, got statuses ${statuses.join(",")}`,
  );
  assert(
    countEvents(db, "thread.spec_edited") === before + 1,
    "contention must append exactly one thread.spec_edited event",
  );
  const accepted = responses.find((response) => response.status === 200);
  if (accepted !== undefined) {
    const body = threadMutationResponseFromJson(accepted.value);
    thread.revisionId = body.revisionId;
    thread.content = { thread: "contention", accepted: true };
    thread.contentHash = body.contentHash;
  }
}

async function decideHalfTheApprovals(
  broker: BrokerHandle,
  approvals: readonly StressApproval[],
): Promise<void> {
  const decisions = approvals.slice(0, Math.floor(approvals.length / 2));
  await Promise.all(
    decisions.map(async (approval) => {
      const body = approvalDecisionRequestToJsonValue({
        schemaVersion: 1,
        decision: "reject",
        idempotencyKey: asIdempotencyKey(nextUlid()),
      });
      const response = await requestJson(
        broker,
        `/api/v1/approvals/${approval.id}/decision`,
        {
          method: "POST",
          headers: jsonHeaders(),
          body: JSON.stringify(body),
        },
        201,
      );
      approvalDecisionResponseFromJson(response.value);
    }),
  );
}

async function assertPinnedApprovalCounts(
  broker: BrokerHandle,
  threads: readonly StressThread[],
  approvals: readonly StressApproval[],
): Promise<void> {
  const expected = new Map<string, number>();
  for (const thread of threads) {
    expected.set(thread.id, 0);
  }
  expected.set(SYSTEM_INBOX_THREAD_ID, 0);
  for (const approval of approvals.slice(Math.floor(approvals.length / 2))) {
    expected.set(approval.threadId, (expected.get(approval.threadId) ?? 0) + 1);
  }
  for (const [threadId, count] of expected) {
    const response = await requestJson(
      broker,
      `/api/v1/threads/${threadId}/pinned-approvals`,
      { headers: authHeaders() },
      200,
    );
    const pinned = threadPinnedApprovalsResponseFromJson(response.value);
    assert(
      pinned.approvals.length === count,
      `thread ${threadId} pinned approvals: expected ${count}, got ${pinned.approvals.length}`,
    );
  }
}

async function pumpStatusesToTerminal(
  broker: BrokerHandle,
  threads: readonly StressThread[],
): Promise<void> {
  for (let index = 0; index < threads.length; index += 1) {
    const thread = threads[index] ?? fail("missing thread during status pump");
    await patchThreadStatus(broker, thread.id, "open", "in_progress", 200);
    await patchThreadStatus(broker, thread.id, "in_progress", "needs_review", 200);
    const terminal: ThreadStatus = index % 2 === 0 ? "merged" : "closed";
    await patchThreadStatus(broker, thread.id, "needs_review", terminal, 200);
    const rejected = await patchThreadStatus(
      broker,
      thread.id,
      terminal,
      terminal === "merged" ? "closed" : "merged",
      422,
    );
    assertErrorCode(rejected.value, "terminal_status_transition");
  }
}

async function assertSseInvalidationConvergence(broker: BrokerHandle): Promise<void> {
  const sse = await openSlowSseSession(broker);

  const created = await postThreadCreate(broker, nextUlid(), {
    thread: "sse-convergence",
    edit: 0,
  });
  const dropped = await withTimeout(sse.dropped, 1_000, false);
  sse.close();
  assert(dropped, "SSE stream did not close under configured backpressure");

  const response = await requestJson(
    broker,
    `/api/v1/threads/${created.threadId}`,
    { headers: authHeaders() },
    200,
  );
  const thread = threadGetResponseFromJson(response.value).thread;
  assert(
    thread.id === created.threadId && thread.spec.contentHash === created.contentHash,
    "thread projection did not converge after dropped SSE invalidation",
  );

  const pinnedResponse = await requestJson(
    broker,
    `/api/v1/threads/${created.threadId}/pinned-approvals`,
    { headers: authHeaders() },
    200,
  );
  const pinned = threadPinnedApprovalsResponseFromJson(pinnedResponse.value);
  assert(
    pinned.threadId === created.threadId && pinned.approvals.length === 0,
    "pinned approval projection did not converge after dropped SSE invalidation",
  );
}

function openSlowSseSession(broker: BrokerHandle): Promise<SlowSseSession> {
  const url = new URL(`${broker.url}/api/events`);
  return new Promise((resolve, reject) => {
    let settled = false;
    const req = httpRequest(
      {
        hostname: url.hostname,
        port: url.port,
        path: url.pathname,
        method: "GET",
        headers: {
          ...authHeaders(),
          Accept: "text/event-stream",
        },
      },
      (res) => {
        if (res.statusCode !== 200) {
          res.resume();
          reject(new Error(`/api/events returned ${res.statusCode ?? 0}`));
          return;
        }
        res.pause();
        const dropped = new Promise<boolean>((resolveDropped) => {
          const done = (): void => resolveDropped(true);
          res.once("aborted", done);
          res.once("close", done);
          res.once("end", done);
          res.once("error", done);
        });
        settled = true;
        resolve({
          dropped,
          close(): void {
            req.destroy();
          },
        });
      },
    );
    req.once("error", () => {
      if (settled) return;
      settled = true;
      resolve({
        dropped: Promise.resolve(true),
        close: () => undefined,
      });
    });
    req.end();
  });
}

function deferAuditChainTamperCheck(): void {
  // TODO(thread-audit-verify): broker event_log currently stores
  // lsn/type/payload but not prevHash/eventHash, and there is no wuphf audit
  // verify CLI path for this SQLite log. Wire this step to the real verifier
  // once that surface lands.
  console.warn(
    `DEFERRED stress step 6 (${TAMPER_DEFERRED_REF}): no broker event_log hash-chain verifier exists yet`,
  );
}

async function postThreadCreate(
  broker: BrokerHandle,
  idempotencyKey: string,
  content: JsonValue,
): Promise<ThreadMutationResponse> {
  const response = await requestJson(
    broker,
    "/api/v1/threads",
    {
      method: "POST",
      headers: jsonHeaders(),
      body: JSON.stringify({
        title: `Stress ${idempotencyKey}`,
        specContent: content,
        externalRefs: { source_urls: [], entity_ids: [] },
        idempotencyKey,
      }),
    },
    201,
  );
  const body = threadMutationResponseFromJson(response.value);
  assert(body.threadId === idempotencyKey, `created thread id mismatch for ${idempotencyKey}`);
  assert(
    body.contentHash === threadSpecContentHash(content),
    `created thread content hash mismatch for ${idempotencyKey}`,
  );
  return body;
}

async function patchThreadSpec(
  broker: BrokerHandle,
  thread: StressThread,
  content: JsonValue,
  idempotencyKey: string,
  expectedStatus: number,
): Promise<ThreadMutationResponse> {
  const response = await patchThreadSpecRaw(
    broker,
    thread.id,
    {
      baseRevisionId: thread.revisionId,
      baseContentHash: thread.contentHash,
      content,
      idempotencyKey,
    },
    expectedStatus,
  );
  return threadMutationResponseFromJson(response.value);
}

async function patchThreadSpecRaw(
  broker: BrokerHandle,
  threadId: ThreadId,
  body: {
    readonly baseRevisionId: string;
    readonly baseContentHash: string;
    readonly content: JsonValue;
    readonly idempotencyKey: string;
  },
  expectedStatus?: number,
): Promise<JsonResponse> {
  return requestJson(
    broker,
    `/api/v1/threads/${threadId}/spec`,
    {
      method: "PATCH",
      headers: jsonHeaders(),
      body: JSON.stringify(body),
    },
    expectedStatus,
  );
}

async function patchThreadStatus(
  broker: BrokerHandle,
  threadId: ThreadId,
  fromStatus: ThreadStatus,
  toStatus: ThreadStatus,
  expectedStatus: number,
): Promise<JsonResponse> {
  return requestJson(
    broker,
    `/api/v1/threads/${threadId}/status`,
    {
      method: "PATCH",
      headers: jsonHeaders(),
      body: JSON.stringify({
        fromStatus,
        toStatus,
        idempotencyKey: nextUlid(),
      }),
    },
    expectedStatus,
  );
}

async function postApprovalRequest(
  broker: BrokerHandle,
  args: {
    readonly requestId: ApprovalRequestId;
    readonly receiptId: ReturnType<typeof asReceiptId>;
    readonly taskId: ReturnType<typeof asTaskId>;
    readonly threadId?: ThreadId | undefined;
  },
): Promise<StressApproval> {
  const claimId = asApprovalClaimId(`claim_${nextUlid()}`);
  const frozenArgsHash = sha256Hex(`frozen-${args.requestId}`);
  const request: ApprovalRequestCreateRequest = {
    schemaVersion: 1,
    claim: {
      schemaVersion: 1,
      claimId,
      kind: "receipt_co_sign",
      receiptId: args.receiptId,
      frozenArgsHash,
      riskClass: "critical",
    },
    scope: {
      mode: "single_use",
      claimId,
      claimKind: "receipt_co_sign",
      role: asApprovalRole("approver"),
      maxUses: 1,
      receiptId: args.receiptId,
      frozenArgsHash,
    },
    riskClass: "critical",
    ...(args.threadId === undefined ? {} : { threadId: args.threadId }),
    taskId: args.taskId,
    receiptId: args.receiptId,
    idempotencyKey: asIdempotencyKey(args.requestId),
  };
  const response = await requestJson(
    broker,
    "/api/v1/approvals",
    {
      method: "POST",
      headers: jsonHeaders(),
      body: JSON.stringify(approvalRequestCreateRequestToJsonValue(request)),
    },
    201,
  );
  const created = approvalRequestCreateResponseFromJson(response.value).approvalRequest;
  return {
    id: created.id,
    threadId: created.threadId ?? SYSTEM_INBOX_THREAD_ID,
  };
}

function minimalReceipt(
  id: ReturnType<typeof asReceiptId>,
  taskId: ReturnType<typeof asTaskId>,
  threadId: ThreadId | undefined,
): ReceiptSnapshot {
  return {
    id,
    agentSlug: asAgentSlug("stress_agent"),
    taskId,
    triggerKind: "human_message",
    triggerRef: `tool:${taskId}`,
    startedAt: new Date("2026-05-18T09:00:00.000Z"),
    finishedAt: new Date("2026-05-18T09:01:00.000Z"),
    status: "ok",
    providerKind: asProviderKind("anthropic"),
    model: "claude-opus-4-7",
    promptHash: sha256Hex(`prompt-${id}`),
    toolManifest: sha256Hex(`tools-${id}`),
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
    schemaVersion: 2,
    ...(threadId === undefined ? {} : { threadId }),
  };
}

async function requestJson(
  broker: BrokerHandle,
  path: string,
  init: RequestInit,
  expectedStatus?: number,
): Promise<JsonResponse> {
  const response = await fetch(`${broker.url}${path}`, init);
  const text = await response.text();
  if (expectedStatus !== undefined && response.status !== expectedStatus) {
    throw new Error(
      `${path} returned ${response.status}, expected ${expectedStatus}: ${text.slice(0, 500)}`,
    );
  }
  return {
    status: response.status,
    value: text.length === 0 ? null : (JSON.parse(text) as unknown),
  };
}

async function withTimeout<T>(promise: Promise<T>, timeoutMs: number, fallback: T): Promise<T> {
  let timeout: ReturnType<typeof setTimeout> | null = null;
  try {
    return await Promise.race([
      promise,
      new Promise<T>((resolve) => {
        timeout = setTimeout(() => resolve(fallback), timeoutMs);
      }),
    ]);
  } finally {
    if (timeout !== null) clearTimeout(timeout);
  }
}

function assertReplayCheckOk(db: ReturnType<typeof openDatabase>, label: string): void {
  const report = runThreadReplayCheck(db);
  assert(
    report.ok,
    `${label} failed: ${JSON.stringify(threadReplayCheckReportToJsonValue(report))}`,
  );
}

function countEvents(db: ReturnType<typeof openDatabase>, type: string): number {
  return (
    db
      .prepare<[string], { readonly count: number }>(
        "SELECT COUNT(*) AS count FROM event_log WHERE type = ?",
      )
      .get(type)?.count ?? 0
  );
}

function authHeaders(): Record<string, string> {
  return { Authorization: `Bearer ${TOKEN}` };
}

function jsonHeaders(): Record<string, string> {
  return { ...authHeaders(), "Content-Type": "application/json" };
}

function assertErrorCode(value: unknown, expected: string): void {
  assert(isRecord(value), `expected error body object for ${expected}`);
  assert(value.error === expected, `expected error ${expected}, got ${String(value.error)}`);
}

function nextUlid(): string {
  const value = ulidCounter;
  ulidCounter += 1;
  return ulidFromCounter(value);
}

function ulidFromCounter(input: number): string {
  let value = BigInt(input);
  let suffix = "";
  for (let index = 0; index < 16; index += 1) {
    suffix = ULID_ALPHABET[Number(value % 32n)] + suffix;
    value /= 32n;
  }
  return `${ULID_PREFIX}${suffix}`;
}

function isRecord(value: unknown): value is Readonly<Record<string, unknown>> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function fail(message: string): never {
  throw new Error(message);
}

function assert(condition: unknown, message: string): asserts condition {
  if (!condition) throw new Error(message);
}

await main();
