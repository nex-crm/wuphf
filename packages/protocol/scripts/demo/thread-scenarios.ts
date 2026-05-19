import {
  asSignerIdentity,
  asThreadId,
  asThreadSpecRevisionId,
  isStreamEventKind,
  lsnFromV1Number,
  type ReceiptSnapshot,
  receiptFromJson,
  receiptToJson,
  sha256Hex,
  type ThreadSpecEditedAuditPayload,
  type ThreadStreamEvent,
  threadAuditPayloadToBytes,
  threadFromJson,
  threadSpecContentHash,
  threadToJson,
  validateThreadSpecEditedAuditPayload,
  validateThreadSpecRevisionChain,
  validateThreadStatusFold,
  validateThreadStreamEvent,
} from "../../src/index.ts";
import { buildValidReceipt } from "./fixtures.ts";
import { expectEqual, expectThrows, header, textDecoder } from "./harness.ts";

export function runThreadScenarios(): void {
  const validReceipt = buildValidReceipt();
  const threadId = asThreadId("01ARZ3NDEKTSV4RRFFQ69G5FAY");
  const revision1 = asThreadSpecRevisionId("01BRZ3NDEKTSV4RRFFQ69G5FA0");
  const revision2 = asThreadSpecRevisionId("01BRZ3NDEKTSV4RRFFQ69G5FA1");
  const staleRevision = asThreadSpecRevisionId("01BRZ3NDEKTSV4RRFFQ69G5FA2");
  const threadSigner = asSignerIdentity("fran@example.com");
  const specContent = {
    body: "Implement the thread protocol slice",
    checklist: ["receipt v2", "audit events", "stream invalidation"],
  };
  const thread = {
    id: threadId,
    title: "Thread protocol",
    status: "open" as const,
    spec: {
      revisionId: revision1,
      threadId,
      content: specContent,
      contentHash: threadSpecContentHash(specContent),
      authoredBy: threadSigner,
      authoredAt: new Date("2026-05-08T18:00:00.000Z"),
    },
    externalRefs: {
      sourceUrls: ["https://example.test/wuphf/743"],
      entityIds: ["issue:743"],
    },
    taskIds: [validReceipt.taskId],
    createdBy: threadSigner,
    createdAt: new Date("2026-05-08T18:00:00.000Z"),
    updatedAt: new Date("2026-05-08T18:01:00.000Z"),
  };

  // ────────────────────────────────────────────────────────────────────────
  header(18, "Thread create + spec edit + status change round-trip through canonical JSON");
  // ────────────────────────────────────────────────────────────────────────
  expectEqual("threadFromJson(threadToJson(thread))", threadFromJson(threadToJson(thread)), thread);
  expectEqual(
    "thread_created audit body is canonical JSON bytes",
    textDecoder
      .decode(
        threadAuditPayloadToBytes("thread_created", {
          threadId,
          title: "Thread protocol",
          createdBy: threadSigner,
          createdAt: thread.createdAt,
          externalRefs: thread.externalRefs,
        }),
      )
      .includes('"threadId"'),
    true,
  );

  // ────────────────────────────────────────────────────────────────────────
  header(19, "Thread spec OCC accepts the prior revision and rejects stale-base 409 shape");
  // ────────────────────────────────────────────────────────────────────────
  const firstEdit: ThreadSpecEditedAuditPayload = {
    threadId,
    revisionId: revision1,
    content: specContent,
    contentHash: threadSpecContentHash(specContent),
    authoredBy: threadSigner,
    authoredAt: new Date("2026-05-08T18:00:00.000Z"),
  };
  const secondEdit: ThreadSpecEditedAuditPayload = {
    ...firstEdit,
    revisionId: revision2,
    baseRevisionId: revision1,
  };
  const staleEdit: ThreadSpecEditedAuditPayload = {
    ...firstEdit,
    revisionId: revision2,
    baseRevisionId: staleRevision,
  };
  expectEqual("OCC happy path", validateThreadSpecRevisionChain([firstEdit, secondEdit]), {
    ok: true,
  });
  expectEqual(
    "OCC stale-base rejected",
    validateThreadSpecRevisionChain([firstEdit, staleEdit]).ok,
    false,
  );

  // ────────────────────────────────────────────────────────────────────────
  header(20, "Thread status fold and terminal-write-once invariant");
  // ────────────────────────────────────────────────────────────────────────
  expectEqual(
    "status fold happy path",
    validateThreadStatusFold([
      { kind: "thread_created", threadId },
      {
        kind: "thread_status_changed",
        threadId,
        fromStatus: "open",
        toStatus: "in_progress",
        changedBy: threadSigner,
        changedAt: new Date("2026-05-08T18:02:00.000Z"),
      },
      {
        kind: "thread_status_changed",
        threadId,
        fromStatus: "in_progress",
        toStatus: "closed",
        changedBy: threadSigner,
        changedAt: new Date("2026-05-08T18:03:00.000Z"),
      },
    ]),
    { ok: true },
  );
  expectEqual(
    "terminal transition rejected",
    validateThreadStatusFold([
      { kind: "thread_created", threadId },
      {
        kind: "thread_status_changed",
        threadId,
        fromStatus: "open",
        toStatus: "closed",
        changedBy: threadSigner,
        changedAt: new Date("2026-05-08T18:02:00.000Z"),
      },
      {
        kind: "thread_status_changed",
        threadId,
        fromStatus: "closed",
        toStatus: "in_progress",
        changedBy: threadSigner,
        changedAt: new Date("2026-05-08T18:03:00.000Z"),
      },
    ]).ok,
    false,
  );

  // ────────────────────────────────────────────────────────────────────────
  header(21, "ReceiptSnapshotV1 rejects threadId presence with path /threadId");
  // ────────────────────────────────────────────────────────────────────────
  const v1WithThreadId = JSON.parse(receiptToJson(validReceipt)) as Record<string, unknown>;
  v1WithThreadId.threadId = threadId;
  expectThrows(() => receiptFromJson(JSON.stringify(v1WithThreadId)), /\/threadId/);

  // ────────────────────────────────────────────────────────────────────────
  header(22, "ReceiptSnapshotV2 round-trips with and without threadId");
  // ────────────────────────────────────────────────────────────────────────
  const v2WithThread: ReceiptSnapshot = { ...validReceipt, schemaVersion: 2, threadId };
  const v2WithoutThread: ReceiptSnapshot = { ...validReceipt, schemaVersion: 2 };
  expectEqual(
    "v2 with threadId canonical bytes",
    receiptToJson(receiptFromJson(receiptToJson(v2WithThread))),
    receiptToJson(v2WithThread),
  );
  expectEqual(
    "v2 without threadId canonical bytes",
    receiptToJson(receiptFromJson(receiptToJson(v2WithoutThread))),
    receiptToJson(v2WithoutThread),
  );

  // ────────────────────────────────────────────────────────────────────────
  header(23, "thread_spec_edited validator catches forged contentHash");
  // ────────────────────────────────────────────────────────────────────────
  expectEqual(
    "forged contentHash rejected",
    validateThreadSpecEditedAuditPayload({ ...firstEdit, contentHash: sha256Hex("forged") }).ok,
    false,
  );

  // ────────────────────────────────────────────────────────────────────────
  header(24, "Thread stream event tuple guards stay invalidation-only");
  // ────────────────────────────────────────────────────────────────────────
  expectEqual("thread.created accepted", isStreamEventKind("thread.created"), true);
  expectEqual("thread.updated accepted", isStreamEventKind("thread.updated"), true);
  expectEqual(
    "thread.pinned_approvals.changed accepted",
    isStreamEventKind("thread.pinned_approvals.changed"),
    true,
  );
  const threadStreamEvent: ThreadStreamEvent = {
    id: "evt-thread-updated-demo",
    kind: "thread.updated",
    emittedAt: "2026-05-08T18:02:00.000Z",
    payload: {
      threadId,
      headLsn: lsnFromV1Number(3),
    },
  };
  expectEqual(
    "thread stream event validator accepts invalidation",
    validateThreadStreamEvent(threadStreamEvent),
    {
      ok: true,
    },
  );
  expectEqual(
    "thread stream event validator rejects payload data",
    validateThreadStreamEvent({
      ...threadStreamEvent,
      payload: {
        ...threadStreamEvent.payload,
        content: "secret",
      },
    }).ok,
    false,
  );
}
