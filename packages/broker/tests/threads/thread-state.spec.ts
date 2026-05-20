import {
  asAgentSlug,
  asIdempotencyKey,
  asProviderKind,
  asReceiptId,
  asSignerIdentity,
  asTaskId,
  asThreadId,
  asThreadSpecRevisionId,
  canonicalJSON,
  type JsonValue,
  type ReceiptSnapshot,
  SanitizedString,
  sha256Hex,
  type ThreadCreateCommand,
  type ThreadCreatedAuditPayload,
  type ThreadSpecEditCommand,
  type ThreadSpecEditedAuditPayload,
  type ThreadStatusChangeCommand,
  type ThreadStatusChangedAuditPayload,
  threadAuditPayloadFromJsonValue,
  threadAuditPayloadToBytes,
  threadSpecContentHash,
} from "@wuphf/protocol";
import { afterEach, describe, expect, it } from "vitest";

import { createEventLog, openDatabase, runMigrations } from "../../src/event-log/index.ts";
import { constructSqliteReceiptStoreForTesting } from "../../src/internal/sqlite-receipt-store-testing.ts";
import type { SqliteReceiptStore } from "../../src/sqlite-receipt-store.ts";
import {
  createThreadSubsystem,
  createThreadViewStore,
  parseThreadIdempotencyKey,
  SYSTEM_INBOX_THREAD_ID,
  snapshotThreadProjection,
  type ThreadAppender,
  type ThreadApprovalQuery,
  type ThreadCommand,
  ThreadConflictError,
  ThreadIdempotencyConflictError,
  ThreadNotFoundError,
  type ThreadReceiptIndexStore,
  type ThreadStateStore,
  type ThreadSubsystem,
  ThreadTerminalTransitionError,
} from "../../src/threads/index.ts";

const THREAD_ID = "01ARZ3NDEKTSV4RRFFQ69G5FAZ";
const OTHER_THREAD_ID = "01BRZ3NDEKTSV4RRFFQ69G5FB0";
const CREATE_KEY = "cmd_thread.create_01CRZ3NDEKTSV4RRFFQ69G5FC0";
const EDIT_KEY = "cmd_thread.spec.edit_01DRZ3NDEKTSV4RRFFQ69G5FD0";
const STATUS_KEY = "cmd_thread.status.change_01FRZ3NDEKTSV4RRFFQ69G5FF0";
const STATUS_KEY_2 = "cmd_thread.status.change_01GRZ3NDEKTSV4RRFFQ69G5FG0";
const STATUS_KEY_3 = "cmd_thread.status.change_01HRZ3NDEKTSV4RRFFQ69G5FH0";
const SIGNER = asSignerIdentity("operator@example.com");
const INITIAL_CONTENT: JsonValue = { goal: "ship threads", version: 1 };

interface Fixture {
  readonly db: ReturnType<typeof openDatabase>;
  readonly eventLog: ReturnType<typeof createEventLog>;
  readonly subsystem: ThreadSubsystem;
  readonly state: ThreadStateStore;
  readonly appender: ThreadAppender;
  readonly receiptStore: SqliteReceiptStore;
}

let fixture: Fixture | null = null;

afterEach(() => {
  fixture?.receiptStore.close();
  fixture = null;
});

function setup(): Fixture {
  const db = openDatabase({ path: ":memory:" });
  runMigrations(db);
  const eventLog = createEventLog(db);
  const receiptStore = constructSqliteReceiptStoreForTesting(db, eventLog);
  const subsystem = createThreadSubsystem(db, eventLog, receiptStore);
  const { state, appender } = subsystem;
  fixture = { db, eventLog, subsystem, state, appender, receiptStore };
  return fixture;
}

function parsedIdempotency(raw: string, command: ThreadCommand) {
  const parsed = parseThreadIdempotencyKey(raw, command);
  if (!parsed.ok) throw new Error(`bad idempotency key: ${raw}`);
  return parsed.key;
}

function commandIdempotencyKey(raw: string, command: ThreadCommand) {
  return asIdempotencyKey(parsedIdempotency(raw, command).ulid);
}

function render(applied: { readonly threadId: string; readonly headLsn: string }) {
  return {
    statusCode: 200,
    payload: Buffer.from(JSON.stringify({ threadId: applied.threadId, headLsn: applied.headLsn })),
  };
}

function idempotencyFingerprint(command: ThreadCommand, raw: string): string {
  return canonicalJSON({ command, raw });
}

function createCommand(key = CREATE_KEY, threadId = THREAD_ID): ThreadCreateCommand {
  return {
    kind: "thread.create",
    idempotencyKey: commandIdempotencyKey(key, "thread.create"),
    threadId: asThreadId(threadId),
    title: "Thread foundation",
    createdBy: SIGNER,
    createdAt: new Date("2026-05-18T10:00:00.000Z"),
    externalRefs: { sourceUrls: ["https://example.com/spec"], entityIds: ["entity:thread"] },
    content: INITIAL_CONTENT,
  };
}

function appendCreate(fix: Fixture, key = CREATE_KEY, threadId = THREAD_ID) {
  return fix.appender.appendCreateIdempotent({
    command: createCommand(key, threadId),
    idempotency: parsedIdempotency(key, "thread.create"),
    requestFingerprint: idempotencyFingerprint("thread.create", key),
    nowMs: 1_700_000_000_000,
    render,
  });
}

function specEditCommand(args: {
  readonly key?: string;
  readonly threadId?: string;
  readonly revisionId?: string;
  readonly baseRevisionId: string;
  readonly content: JsonValue;
  readonly authoredAt?: string;
}): ThreadSpecEditCommand {
  const key = args.key ?? EDIT_KEY;
  return {
    kind: "thread.spec.edit",
    idempotencyKey: commandIdempotencyKey(key, "thread.spec.edit"),
    threadId: asThreadId(args.threadId ?? THREAD_ID),
    revisionId: asThreadSpecRevisionId(args.revisionId ?? "01JRZ3NDEKTSV4RRFFQ69G5FJ0"),
    baseRevisionId: asThreadSpecRevisionId(args.baseRevisionId),
    content: args.content,
    contentHash: threadSpecContentHash(args.content),
    authoredBy: SIGNER,
    authoredAt: new Date(args.authoredAt ?? "2026-05-18T10:05:00.000Z"),
  };
}

function appendSpecEdit(
  fix: Fixture,
  command: ThreadSpecEditCommand,
  key: string,
  baseContent: JsonValue,
) {
  return fix.appender.appendSpecEditIdempotent({
    command,
    baseContentHash: threadSpecContentHash(baseContent),
    idempotency: parsedIdempotency(key, "thread.spec.edit"),
    requestFingerprint: idempotencyFingerprint("thread.spec.edit", key),
    nowMs: 1_700_000_000_100,
    render,
  });
}

function statusCommand(args: {
  readonly key?: string;
  readonly fromStatus: "open" | "in_progress" | "needs_review" | "merged" | "closed";
  readonly toStatus: "open" | "in_progress" | "needs_review" | "merged" | "closed";
  readonly changedAt?: string;
}): ThreadStatusChangeCommand {
  const key = args.key ?? STATUS_KEY;
  return {
    kind: "thread.status.change",
    idempotencyKey: commandIdempotencyKey(key, "thread.status.change"),
    threadId: asThreadId(THREAD_ID),
    fromStatus: args.fromStatus,
    toStatus: args.toStatus,
    changedBy: SIGNER,
    changedAt: new Date(args.changedAt ?? "2026-05-18T10:10:00.000Z"),
  };
}

function appendStatus(fix: Fixture, command: ThreadStatusChangeCommand, key: string) {
  return fix.appender.appendStatusChangeIdempotent({
    command,
    idempotency: parsedIdempotency(key, "thread.status.change"),
    requestFingerprint: idempotencyFingerprint("thread.status.change", key),
    nowMs: 1_700_000_000_200,
    render,
  });
}

function countEvents(fix: Fixture, type: string): number {
  return (
    fix.db
      .prepare<[string], { readonly count: number }>(
        "SELECT COUNT(*) AS count FROM event_log WHERE type = ?",
      )
      .get(type)?.count ?? 0
  );
}

function eventPayloadBytes(fix: Fixture, type: string): readonly Buffer[] {
  return fix.db
    .prepare<[string], { readonly payload: Buffer }>(
      "SELECT payload FROM event_log WHERE type = ? ORDER BY lsn ASC",
    )
    .all(type)
    .map((row) => row.payload);
}

function expectThreadPayloadBytes(
  actual: Buffer,
  kind: "thread_created",
  payload: ThreadCreatedAuditPayload,
): void;
function expectThreadPayloadBytes(
  actual: Buffer,
  kind: "thread_spec_edited",
  payload: ThreadSpecEditedAuditPayload,
): void;
function expectThreadPayloadBytes(
  actual: Buffer,
  kind: "thread_status_changed",
  payload: ThreadStatusChangedAuditPayload,
): void;
function expectThreadPayloadBytes(
  actual: Buffer,
  kind: "thread_created" | "thread_spec_edited" | "thread_status_changed",
  payload:
    | ThreadCreatedAuditPayload
    | ThreadSpecEditedAuditPayload
    | ThreadStatusChangedAuditPayload,
): void {
  expect(actual).toEqual(Buffer.from(threadAuditPayloadToBytes(kind, payload)));
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

function minimalReceiptV2(id: string, taskId: string, threadId = THREAD_ID): ReceiptSnapshot {
  return {
    ...minimalReceiptV1(id, taskId),
    schemaVersion: 2,
    threadId: asThreadId(threadId),
  };
}

describe("thread appender and projection", () => {
  it("derives thread list views inside one read transaction", () => {
    const fix = setup();
    const observed: { readonly name: string; readonly inTransaction: boolean }[] = [];
    const record = (name: string): void => {
      observed.push({ name, inTransaction: fix.db.inTransaction });
    };
    const state: ThreadStateStore = {
      applyEvent(recordEvent) {
        fix.state.applyEvent(recordEvent);
      },
      clear() {
        fix.state.clear();
      },
      rebuildFromLog(eventLog, fromLsn) {
        fix.state.rebuildFromLog(eventLog, fromLsn);
      },
      getById(threadId) {
        return fix.state.getById(threadId);
      },
      hasSpecRevision(revisionId) {
        return fix.state.hasSpecRevision(revisionId);
      },
      list(filter) {
        record("state.list");
        return fix.state.list(filter);
      },
      listPage(page) {
        return fix.state.listPage(page);
      },
    };
    const receiptIndex: ThreadReceiptIndexStore = {
      applyEvent(recordEvent) {
        fix.subsystem.receiptIndex.applyEvent(recordEvent);
      },
      clear() {
        fix.subsystem.receiptIndex.clear();
      },
      rebuildFromLog(eventLog, fromLsn) {
        fix.subsystem.receiptIndex.rebuildFromLog(eventLog, fromLsn);
      },
      list(threadId, filter) {
        return fix.subsystem.receiptIndex.list(threadId, filter);
      },
      refsForThread(threadId) {
        record("receiptIndex.refsForThread");
        return fix.subsystem.receiptIndex.refsForThread(threadId);
      },
      latestForThread(threadId) {
        record("receiptIndex.latestForThread");
        return fix.subsystem.receiptIndex.latestForThread(threadId);
      },
    };
    const approvals: ThreadApprovalQuery = {
      countPendingByThread() {
        record("approvals.countPendingByThread");
        return 0;
      },
      listPendingByThread() {
        return [];
      },
      latestHeadLsnByThread() {
        record("approvals.latestHeadLsnByThread");
        return null;
      },
      pendingByThreadSnapshot() {
        return { rows: [], headLsn: null };
      },
    };
    const views = createThreadViewStore(fix.db, state, receiptIndex);

    const page = views.listThreadViews({ limit: 10, approvals });

    expect(page.threads.map((thread) => thread.id)).toContain(SYSTEM_INBOX_THREAD_ID);
    expect(observed.map((entry) => entry.name)).toEqual(
      expect.arrayContaining([
        "state.list",
        "receiptIndex.refsForThread",
        "receiptIndex.latestForThread",
        "approvals.countPendingByThread",
        "approvals.latestHeadLsnByThread",
      ]),
    );
    expect(observed.every((entry) => entry.inTransaction)).toBe(true);
  });

  it("stores spec content hash from canonical content and replays latest spec payload bytes", () => {
    const fix = setup();
    appendCreate(fix);
    const nextContent: JsonValue = { goal: "ship threads", version: 2, nested: ["a", "b"] };
    appendSpecEdit(
      fix,
      specEditCommand({
        baseRevisionId: "01CRZ3NDEKTSV4RRFFQ69G5FC0",
        content: nextContent,
      }),
      EDIT_KEY,
      INITIAL_CONTENT,
    );

    const row = fix.state.getById(asThreadId(THREAD_ID));
    expect(row?.spec.contentHash).toBe(threadSpecContentHash(nextContent));
    expect(row?.spec.content).toEqual(nextContent);

    const latestSpec = fix.db
      .prepare<[], { readonly payload: Buffer }>(
        "SELECT payload FROM event_log WHERE type = 'thread.spec_edited' ORDER BY lsn DESC LIMIT 1",
      )
      .get();
    expect(latestSpec).toBeDefined();
    if (latestSpec === undefined) return;
    const payload = threadAuditPayloadFromJsonValue(
      "thread_spec_edited",
      JSON.parse(latestSpec.payload.toString("utf8")) as unknown,
    ) as ThreadSpecEditedAuditPayload;
    expect(canonicalJSON(row?.spec.content)).toBe(canonicalJSON(payload.content));
  });

  it("enforces spec OCC and accepts only one same-base edit", async () => {
    const fix = setup();
    appendCreate(fix);
    const baseRevisionId = "01CRZ3NDEKTSV4RRFFQ69G5FC0";
    const attempts = await Promise.all(
      Array.from({ length: 8 }, async (_, index) => {
        const key = `cmd_thread.spec.edit_01${String(index + 10).padStart(24, "0")}`;
        try {
          appendSpecEdit(
            fix,
            specEditCommand({
              key,
              revisionId: `01${String(index + 30).padStart(24, "0")}`,
              baseRevisionId,
              content: { attempt: index },
            }),
            key,
            INITIAL_CONTENT,
          );
          return "accepted" as const;
        } catch (err) {
          if (err instanceof ThreadConflictError && err.code === "stale_spec_base") {
            return "conflict" as const;
          }
          throw err;
        }
      }),
    );
    expect(attempts.filter((r) => r === "accepted").length).toBe(1);
    expect(attempts.filter((r) => r === "conflict").length).toBe(7);
    expect(countEvents(fix, "thread.spec_edited")).toBe(3);

    const specRows = fix.db
      .prepare<[], { readonly payload: Buffer }>(
        "SELECT payload FROM event_log WHERE type = 'thread.spec_edited' ORDER BY lsn ASC",
      )
      .all();
    const baseRevisionIds = specRows.map((row) => {
      const payload = threadAuditPayloadFromJsonValue(
        "thread_spec_edited",
        JSON.parse(row.payload.toString("utf8")) as unknown,
      ) as ThreadSpecEditedAuditPayload;
      return payload.baseRevisionId ?? null;
    });
    expect(baseRevisionIds).toEqual([null, null, baseRevisionId]);
  });

  it("rejects spec revision id reuse across threads", () => {
    const fix = setup();
    appendCreate(fix);
    appendCreate(fix, "cmd_thread.create_01SRZ3NDEKTSV4RRFFQ69G5FS0", OTHER_THREAD_ID);
    const duplicateRevisionId = "01JRZ3NDEKTSV4RRFFQ69G5FJ0";
    appendSpecEdit(
      fix,
      specEditCommand({
        revisionId: duplicateRevisionId,
        baseRevisionId: "01CRZ3NDEKTSV4RRFFQ69G5FC0",
        content: { thread: "one" },
      }),
      EDIT_KEY,
      INITIAL_CONTENT,
    );

    expect(() =>
      appendSpecEdit(
        fix,
        specEditCommand({
          key: "cmd_thread.spec.edit_01MRZ3NDEKTSV4RRFFQ69G5FM0",
          threadId: OTHER_THREAD_ID,
          revisionId: duplicateRevisionId,
          baseRevisionId: "01SRZ3NDEKTSV4RRFFQ69G5FS0",
          content: { thread: "two" },
        }),
        "cmd_thread.spec.edit_01MRZ3NDEKTSV4RRFFQ69G5FM0",
        INITIAL_CONTENT,
      ),
    ).toThrow(ThreadConflictError);
  });

  it("folds status by LSN, rejects bad fromStatus, and blocks terminal exits", () => {
    const fix = setup();
    appendCreate(fix);
    appendStatus(fix, statusCommand({ fromStatus: "open", toStatus: "in_progress" }), STATUS_KEY);
    expect(fix.state.getById(asThreadId(THREAD_ID))?.status).toBe("in_progress");
    expect(fix.state.getById(asThreadId(THREAD_ID))?.closedAt).toBeUndefined();

    expect(() =>
      appendStatus(
        fix,
        statusCommand({
          key: STATUS_KEY_2,
          fromStatus: "open",
          toStatus: "closed",
        }),
        STATUS_KEY_2,
      ),
    ).toThrow(ThreadConflictError);

    appendStatus(
      fix,
      statusCommand({
        key: STATUS_KEY_2,
        fromStatus: "in_progress",
        toStatus: "needs_review",
        changedAt: "2026-05-18T10:11:00.000Z",
      }),
      STATUS_KEY_2,
    );
    appendStatus(
      fix,
      statusCommand({
        key: STATUS_KEY_3,
        fromStatus: "needs_review",
        toStatus: "merged",
        changedAt: "2026-05-18T10:12:00.000Z",
      }),
      STATUS_KEY_3,
    );
    const merged = fix.state.getById(asThreadId(THREAD_ID));
    expect(merged?.status).toBe("merged");
    expect(merged?.closedAt?.toISOString()).toBe("2026-05-18T10:12:00.000Z");

    expect(() =>
      appendStatus(
        fix,
        statusCommand({
          key: "cmd_thread.status.change_01KRZ3NDEKTSV4RRFFQ69G5FK0",
          fromStatus: "merged",
          toStatus: "closed",
        }),
        "cmd_thread.status.change_01KRZ3NDEKTSV4RRFFQ69G5FK0",
      ),
    ).toThrow(ThreadTerminalTransitionError);
  });

  it("rejects spec and status commands that reference a missing thread", () => {
    const fix = setup();
    expect(() =>
      appendSpecEdit(
        fix,
        specEditCommand({
          baseRevisionId: "01CRZ3NDEKTSV4RRFFQ69G5FC0",
          content: { missing: true },
        }),
        EDIT_KEY,
        INITIAL_CONTENT,
      ),
    ).toThrow(ThreadNotFoundError);
    expect(() =>
      appendStatus(fix, statusCommand({ fromStatus: "open", toStatus: "closed" }), STATUS_KEY),
    ).toThrow(ThreadNotFoundError);
  });

  it("duplicates idempotency keys without duplicating events or projection rows", () => {
    const fix = setup();
    const first = appendCreate(fix);
    const second = appendCreate(fix);
    expect(first.replayed).toBe(false);
    expect(second.replayed).toBe(true);
    expect(second.payload.toString("utf8")).toBe(first.payload.toString("utf8"));
    expect(countEvents(fix, "thread.created")).toBe(2);
    expect(countEvents(fix, "thread.spec_edited")).toBe(2);
    expect(fix.state.list()).toHaveLength(2);
    expect(fix.state.getById(SYSTEM_INBOX_THREAD_ID)?.title).toBe("Inbox");

    const edit = specEditCommand({
      baseRevisionId: "01CRZ3NDEKTSV4RRFFQ69G5FC0",
      content: { edit: 1 },
    });
    const editFirst = appendSpecEdit(fix, edit, EDIT_KEY, INITIAL_CONTENT);
    const editSecond = appendSpecEdit(fix, edit, EDIT_KEY, INITIAL_CONTENT);
    expect(editFirst.replayed).toBe(false);
    expect(editSecond.replayed).toBe(true);
    expect(countEvents(fix, "thread.spec_edited")).toBe(3);
  });

  it("rejects idempotency key reuse with a different request fingerprint", () => {
    const fix = setup();
    appendCreate(fix);

    expect(() =>
      fix.appender.appendCreateIdempotent({
        command: createCommand(),
        idempotency: parsedIdempotency(CREATE_KEY, "thread.create"),
        requestFingerprint: idempotencyFingerprint("thread.create", "different-body"),
        nowMs: 1_700_000_000_000,
        render,
      }),
    ).toThrow(ThreadIdempotencyConflictError);
    expect(countEvents(fix, "thread.created")).toBe(2);
    expect(countEvents(fix, "thread.spec_edited")).toBe(2);
  });

  it("rebuilds thread projection from the event log byte-equal to live rows", () => {
    const fix = setup();
    const create = createCommand();
    appendCreate(fix);
    const edit = specEditCommand({
      baseRevisionId: "01CRZ3NDEKTSV4RRFFQ69G5FC0",
      content: { edit: "replay" },
    });
    appendSpecEdit(fix, edit, EDIT_KEY, INITIAL_CONTENT);
    const status = statusCommand({ fromStatus: "open", toStatus: "in_progress" });
    appendStatus(fix, status, STATUS_KEY);
    appendCreate(fix, "cmd_thread.create_01SRZ3NDEKTSV4RRFFQ69G5FS0", OTHER_THREAD_ID);

    const createdRows = eventPayloadBytes(fix, "thread.created");
    const specRows = eventPayloadBytes(fix, "thread.spec_edited");
    const statusRows = eventPayloadBytes(fix, "thread.status_changed");
    expectThreadPayloadBytes(createdRows[1] ?? Buffer.alloc(0), "thread_created", {
      threadId: create.threadId,
      title: create.title,
      createdBy: create.createdBy,
      createdAt: create.createdAt,
      externalRefs: create.externalRefs,
    });
    expectThreadPayloadBytes(specRows[1] ?? Buffer.alloc(0), "thread_spec_edited", {
      threadId: create.threadId,
      revisionId: asThreadSpecRevisionId("01CRZ3NDEKTSV4RRFFQ69G5FC0"),
      content: create.content,
      contentHash: threadSpecContentHash(create.content),
      authoredBy: create.createdBy,
      authoredAt: create.createdAt,
    });
    expectThreadPayloadBytes(specRows[2] ?? Buffer.alloc(0), "thread_spec_edited", {
      threadId: edit.threadId,
      revisionId: edit.revisionId,
      baseRevisionId: edit.baseRevisionId,
      content: edit.content,
      contentHash: edit.contentHash,
      authoredBy: edit.authoredBy,
      authoredAt: edit.authoredAt,
    });
    expectThreadPayloadBytes(statusRows[0] ?? Buffer.alloc(0), "thread_status_changed", {
      threadId: status.threadId,
      fromStatus: status.fromStatus,
      toStatus: status.toStatus,
      changedBy: status.changedBy,
      changedAt: status.changedAt,
    });

    const live = snapshotThreadProjection(fix.db);
    fix.state.rebuildFromLog(fix.eventLog);
    expect(snapshotThreadProjection(fix.db)).toEqual(live);
    fix.subsystem.rebuildFromLog(0);
    expect(snapshotThreadProjection(fix.db)).toEqual(live);
  });

  it("paginates and rebuilds the direct thread receipt index", async () => {
    const fix = setup();
    appendCreate(fix);
    const taskA = "01MRZ3NDEKTSV4RRFFQ69G5FM0";
    const taskB = "01NRZ3NDEKTSV4RRFFQ69G5FN0";
    await fix.receiptStore.put(minimalReceiptV2("01PRZ3NDEKTSV4RRFFQ69G5FP0", taskA));
    await fix.receiptStore.put(minimalReceiptV2("01QRZ3NDEKTSV4RRFFQ69G5FQ0", taskB));
    await fix.receiptStore.put(minimalReceiptV2("01RRZ3NDEKTSV4RRFFQ69G5FR0", taskA));
    const page = await fix.receiptStore.list({ threadId: asThreadId(THREAD_ID) });
    expect(page.items.map((receipt) => receipt.id)).toEqual([
      "01PRZ3NDEKTSV4RRFFQ69G5FP0",
      "01QRZ3NDEKTSV4RRFFQ69G5FQ0",
      "01RRZ3NDEKTSV4RRFFQ69G5FR0",
    ]);
    expect([...new Set(page.items.map((receipt) => receipt.taskId))]).toEqual([taskA, taskB]);
    const liveRefs = {
      receiptIds: [
        "01PRZ3NDEKTSV4RRFFQ69G5FP0",
        "01QRZ3NDEKTSV4RRFFQ69G5FQ0",
        "01RRZ3NDEKTSV4RRFFQ69G5FR0",
      ],
      taskIds: [taskA, taskB],
    };
    expect(fix.subsystem.receiptIndex.refsForThread(asThreadId(THREAD_ID))).toEqual(liveRefs);
    const first = fix.subsystem.receiptIndex.list(asThreadId(THREAD_ID), { limit: 2 });
    expect(first.items.map((item) => item.receiptId)).toEqual([
      "01PRZ3NDEKTSV4RRFFQ69G5FP0",
      "01QRZ3NDEKTSV4RRFFQ69G5FQ0",
    ]);
    expect(first.nextCursor).not.toBeNull();
    if (first.nextCursor === null) throw new Error("missing receipt index cursor");
    const second = fix.subsystem.receiptIndex.list(asThreadId(THREAD_ID), {
      cursor: first.nextCursor,
      limit: 2,
    });
    expect(second.items.map((item) => item.receiptId)).toEqual(["01RRZ3NDEKTSV4RRFFQ69G5FR0"]);
    expect(fix.subsystem.receiptIndex.latestForThread(asThreadId(THREAD_ID))?.receiptId).toBe(
      "01RRZ3NDEKTSV4RRFFQ69G5FR0",
    );
    fix.subsystem.receiptIndex.rebuildFromLog(fix.eventLog, 0);
    expect(fix.subsystem.receiptIndex.refsForThread(asThreadId(THREAD_ID))).toEqual(liveRefs);
    fix.subsystem.rebuildFromLog(0);
    expect(fix.subsystem.receiptIndex.refsForThread(asThreadId(THREAD_ID))).toEqual(liveRefs);
  });

  it("keeps thread state and receipt index intact when full rebuild replay fails", async () => {
    const fix = setup();
    appendCreate(fix);
    await fix.receiptStore.put(
      minimalReceiptV2("01PRZ3NDEKTSV4RRFFQ69G5FP0", "01MRZ3NDEKTSV4RRFFQ69G5FM0"),
    );
    const live = snapshotThreadProjection(fix.db);
    const liveReceiptRows = fix.db
      .prepare<[], { readonly count: number }>("SELECT COUNT(*) AS count FROM thread_receipts")
      .get()?.count;

    fix.eventLog.append({
      type: "thread.status_changed",
      payload: Buffer.from(
        threadAuditPayloadToBytes("thread_status_changed", {
          threadId: asThreadId(OTHER_THREAD_ID),
          fromStatus: "open",
          toStatus: "closed",
          changedBy: SIGNER,
          changedAt: new Date("2026-05-18T10:30:00.000Z"),
        }),
      ),
    });

    expect(() => fix.subsystem.rebuildFromLog(0)).toThrow(
      /thread projection status change referenced a missing thread/,
    );
    expect(snapshotThreadProjection(fix.db)).toEqual(live);
    expect(
      fix.db
        .prepare<[], { readonly count: number }>("SELECT COUNT(*) AS count FROM thread_receipts")
        .get()?.count,
    ).toBe(liveReceiptRows);
  });
});
