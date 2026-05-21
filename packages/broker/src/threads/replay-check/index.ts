import type { DatabaseSync } from "node:sqlite";
import {
  asThreadId,
  canonicalJSON,
  type EventLsn,
  type JsonValue,
  lsnFromV1Number,
  type ReceiptStatus,
  receiptFromJson,
  THREAD_STATUS_VALUES,
  type ThreadCreatedAuditPayload,
  type ThreadReplayCheckDiscrepancy,
  type ThreadReplayCheckReport,
  type ThreadSpecEditedAuditPayload,
  type ThreadStatus,
  type ThreadStatusChangedAuditPayload,
  threadAuditPayloadFromJsonValue,
  threadExternalRefsToJsonValue,
} from "@wuphf/protocol";

import {
  type ApprovalProjectionSnapshotRow,
  type ApprovalReplayEventRow,
  replayApprovalsProjectionSnapshot,
  snapshotApprovalsProjection,
} from "../../approvals/index.ts";
import { createTransaction } from "../../internal/sqlite-transaction.ts";
import { deriveThreadEffectiveStatus } from "../effective-status.ts";
import { SYSTEM_INBOX_THREAD_ID } from "../subsystem.ts";

const BATCH_SIZE = 1_000;
const THREAD_EVENT_TYPES = [
  "thread.created",
  "thread.spec_edited",
  "thread.status_changed",
  "approval.requested",
  "approval.decided",
  "receipt.put",
] as const;
const THREAD_EVENT_TYPE_SQL = THREAD_EVENT_TYPES.map((type) => `'${type}'`).join(", ");
const THREAD_STATUS_SET: ReadonlySet<string> = new Set<string>(THREAD_STATUS_VALUES);

export interface ThreadProjectionSnapshotRow {
  readonly threadId: string;
  readonly title: string;
  readonly status: string;
  readonly headLsn: number;
  readonly createdBy: string;
  readonly createdAtMs: number;
  readonly updatedAtMs: number;
  readonly closedAtMs: number | null;
  readonly specRevisionId: string | null;
  readonly specBaseRevisionId: string | null;
  readonly specContent: string | null;
  readonly specContentHash: string | null;
  readonly specAuthoredBy: string | null;
  readonly specAuthoredAtMs: number | null;
  readonly externalRefs: string;
}

interface ReplayEventRow {
  readonly lsn: number;
  readonly tsMs: number;
  readonly type: string;
  readonly payload: Uint8Array;
}

interface HighestLsnRow {
  readonly lsn: number;
}

interface ThreadReceiptProjectionRow {
  readonly threadId: string;
  readonly receiptId: string;
  readonly taskId: string;
  readonly lsn: number;
  readonly payload: Uint8Array;
}

interface ReplayedThreadState {
  threadId: string;
  title: string;
  status: string;
  headLsn: number;
  createdBy: string;
  createdAtMs: number;
  updatedAtMs: number;
  closedAtMs: number | null;
  specRevisionId: string | null;
  specBaseRevisionId: string | null;
  specContent: string | null;
  specContentHash: string | null;
  specAuthoredBy: string | null;
  specAuthoredAtMs: number | null;
  externalRefs: string;
  receiptIds: string[];
  taskIds: string[];
  latestReceiptStatus: ReceiptStatus | null;
}

interface ThreadReceiptRefs {
  readonly receiptIds: readonly string[];
  readonly taskIds: readonly string[];
  readonly latestReceiptStatus: ReceiptStatus | null;
}

interface PinnedApprovalProjection {
  readonly approvalIds: readonly string[];
  readonly headLsn: EventLsn | null;
}

interface EffectiveStatusProjection {
  readonly effectiveStatus: string;
  readonly attentionReason: string | null;
  readonly boardColumn: string;
  readonly currentSeat: string;
  readonly pendingApprovalCount: number;
}

export function snapshotThreadProjection(db: DatabaseSync): readonly ThreadProjectionSnapshotRow[] {
  return db
    .prepare(
      `SELECT thread_id AS threadId,
              title,
              status,
              head_lsn AS headLsn,
              created_by AS createdBy,
              created_at_ms AS createdAtMs,
              updated_at_ms AS updatedAtMs,
              closed_at_ms AS closedAtMs,
              spec_revision_id AS specRevisionId,
              spec_base_revision_id AS specBaseRevisionId,
              spec_content AS specContent,
              spec_content_hash AS specContentHash,
              spec_authored_by AS specAuthoredBy,
              spec_authored_at_ms AS specAuthoredAtMs,
              external_refs AS externalRefs
       FROM threads
       ORDER BY thread_id ASC`,
    )
    .all() as unknown as ThreadProjectionSnapshotRow[];
}

export function runThreadReplayCheck(db: DatabaseSync): ThreadReplayCheckReport {
  const readBatchStmt = db.prepare(
    `SELECT lsn, ts_ms AS tsMs, type, payload
     FROM event_log
     WHERE lsn > ? AND type IN (${THREAD_EVENT_TYPE_SQL})
     ORDER BY lsn ASC
     LIMIT ?`,
  );
  const highestLsnStmt = db.prepare("SELECT COALESCE(MAX(lsn), 0) AS lsn FROM event_log");
  const listThreadReceiptsStmt = db.prepare(
    `SELECT tr.thread_id AS threadId, tr.receipt_id AS receiptId, tr.task_id AS taskId,
            tr.lsn, rp.payload
     FROM thread_receipts AS tr
     INNER JOIN receipts_projection AS rp ON rp.receipt_id = tr.receipt_id
     ORDER BY tr.thread_id ASC, tr.lsn ASC`,
  );

  const txn = createTransaction(db, (): ThreadReplayCheckReport => {
    const replayedThreads = new Map<string, ReplayedThreadState>();
    const acceptedRevisionIds = new Map<string, number>();
    const taskThreadIds = new Map<string, string>();
    const approvalReplayRows: ApprovalReplayEventRow[] = [];
    const discrepancies: ThreadReplayCheckDiscrepancy[] = [];
    let cursor = 0;
    let scanned = 0;

    for (;;) {
      const rows = readBatchStmt.all(cursor, BATCH_SIZE) as unknown as ReplayEventRow[];
      if (rows.length === 0) break;
      for (const row of rows) {
        scanned += 1;
        if (
          row.type === "thread.created" ||
          row.type === "thread.spec_edited" ||
          row.type === "thread.status_changed" ||
          row.type === "approval.requested" ||
          row.type === "approval.decided"
        ) {
          approvalReplayRows.push(row);
        }
        applyReplayEvent(row, replayedThreads, acceptedRevisionIds, taskThreadIds, discrepancies);
      }
      cursor = rows[rows.length - 1]?.lsn ?? cursor;
      if (rows.length < BATCH_SIZE) break;
    }

    let replayedApprovalRows: readonly ApprovalProjectionSnapshotRow[] = [];
    try {
      replayedApprovalRows = replayApprovalsProjectionSnapshot(approvalReplayRows);
    } catch (err) {
      discrepancies.push({
        kind: "approval_replay_failed",
        reason: boundedReason(err),
      });
    }

    const liveThreads = new Map(snapshotThreadProjection(db).map((row) => [row.threadId, row]));
    const replayedThreadRows = new Map<string, ThreadProjectionSnapshotRow>();
    for (const row of replayedThreads.values()) {
      replayedThreadRows.set(row.threadId, threadProjectionRow(row));
    }
    compareThreadRows(replayedThreadRows, liveThreads, discrepancies);

    const liveReceiptRefs = liveThreadReceiptRefs(
      listThreadReceiptsStmt.all() as unknown as ThreadReceiptProjectionRow[],
    );
    const replayedReceiptRefs = new Map<string, ThreadReceiptRefs>();
    for (const row of replayedThreads.values()) {
      replayedReceiptRefs.set(row.threadId, {
        receiptIds: row.receiptIds,
        taskIds: row.taskIds,
        latestReceiptStatus: row.latestReceiptStatus,
      });
    }
    compareReceiptRefs(replayedReceiptRefs, liveReceiptRefs, discrepancies);

    const replayedPinned = pinnedApprovalProjection(replayedApprovalRows, discrepancies);
    const livePinned = pinnedApprovalProjection(snapshotApprovalsProjection(db), discrepancies);
    comparePinnedApprovals(replayedPinned, livePinned, replayedThreads, liveThreads, discrepancies);

    const replayedEffective = effectiveStatusProjection(replayedThreads, replayedPinned);
    const liveEffective = effectiveStatusProjectionFromLive(
      liveThreads,
      liveReceiptRefs,
      livePinned,
    );
    compareEffectiveStatus(replayedEffective, liveEffective, discrepancies);

    return {
      ok: discrepancies.length === 0,
      highestLsn: lsnFromV1Number((highestLsnStmt.get() as HighestLsnRow | undefined)?.lsn ?? 0),
      eventsScanned: scanned,
      discrepancies,
    };
  });
  return txn.deferred();
}

function applyReplayEvent(
  row: ReplayEventRow,
  threads: Map<string, ReplayedThreadState>,
  acceptedRevisionIds: Map<string, number>,
  taskThreadIds: Map<string, string>,
  discrepancies: ThreadReplayCheckDiscrepancy[],
): void {
  try {
    if (row.type === "thread.created") {
      const payload = threadAuditPayloadFromJsonValue(
        "thread_created",
        JSON.parse(new TextDecoder().decode(row.payload)) as unknown,
      ) as ThreadCreatedAuditPayload;
      threads.set(payload.threadId, {
        threadId: payload.threadId,
        title: payload.title,
        status: "open",
        headLsn: row.lsn,
        createdBy: payload.createdBy,
        createdAtMs: payload.createdAt.getTime(),
        updatedAtMs: payload.createdAt.getTime(),
        closedAtMs: null,
        specRevisionId: null,
        specBaseRevisionId: null,
        specContent: null,
        specContentHash: null,
        specAuthoredBy: null,
        specAuthoredAtMs: null,
        externalRefs: canonicalJSON(threadExternalRefsToJsonValue(payload.externalRefs)),
        receiptIds: [],
        taskIds: [],
        latestReceiptStatus: null,
      });
      return;
    }
    if (row.type === "thread.spec_edited") {
      const payload = threadAuditPayloadFromJsonValue(
        "thread_spec_edited",
        JSON.parse(new TextDecoder().decode(row.payload)) as unknown,
      ) as ThreadSpecEditedAuditPayload;
      applySpecEdit(row, payload, threads, acceptedRevisionIds, discrepancies);
      return;
    }
    if (row.type === "thread.status_changed") {
      const payload = threadAuditPayloadFromJsonValue(
        "thread_status_changed",
        JSON.parse(new TextDecoder().decode(row.payload)) as unknown,
      ) as ThreadStatusChangedAuditPayload;
      applyStatusChange(row, payload, threads, discrepancies);
      return;
    }
    if (row.type === "receipt.put") {
      applyReceipt(row, threads, taskThreadIds, discrepancies);
    }
  } catch (err) {
    discrepancies.push({
      kind: "event_payload_unparseable",
      lsn: lsnFromV1Number(row.lsn),
      eventType: row.type,
      reason: boundedReason(err),
    });
  }
}

function applySpecEdit(
  row: ReplayEventRow,
  payload: ThreadSpecEditedAuditPayload,
  threads: Map<string, ReplayedThreadState>,
  acceptedRevisionIds: Map<string, number>,
  discrepancies: ThreadReplayCheckDiscrepancy[],
): void {
  const thread = threads.get(payload.threadId);
  if (thread === undefined) {
    logInvariant(discrepancies, row, payload.threadId, "spec edit references a missing thread");
    return;
  }
  if (acceptedRevisionIds.has(payload.revisionId)) {
    logInvariant(discrepancies, row, payload.threadId, "duplicate spec revision id", {
      actual: payload.revisionId,
    });
    return;
  }
  const expectedBase = thread.specRevisionId;
  const actualBase = payload.baseRevisionId ?? null;
  if (actualBase !== expectedBase) {
    logInvariant(discrepancies, row, payload.threadId, "spec base revision does not match head", {
      expected: expectedBase,
      actual: actualBase,
    });
  }
  acceptedRevisionIds.set(payload.revisionId, row.lsn);
  thread.headLsn = row.lsn;
  thread.updatedAtMs = payload.authoredAt.getTime();
  thread.specRevisionId = payload.revisionId;
  thread.specBaseRevisionId = payload.baseRevisionId ?? null;
  thread.specContent = canonicalJSON(payload.content);
  thread.specContentHash = payload.contentHash;
  thread.specAuthoredBy = payload.authoredBy;
  thread.specAuthoredAtMs = payload.authoredAt.getTime();
}

function applyStatusChange(
  row: ReplayEventRow,
  payload: ThreadStatusChangedAuditPayload,
  threads: Map<string, ReplayedThreadState>,
  discrepancies: ThreadReplayCheckDiscrepancy[],
): void {
  const thread = threads.get(payload.threadId);
  if (thread === undefined) {
    logInvariant(discrepancies, row, payload.threadId, "status change references a missing thread");
    return;
  }
  if (thread.status !== payload.fromStatus) {
    logInvariant(discrepancies, row, payload.threadId, "status fromStatus does not match fold", {
      expected: thread.status,
      actual: payload.fromStatus,
    });
  }
  if (thread.status === "merged" || thread.status === "closed") {
    logInvariant(discrepancies, row, payload.threadId, "status transition out of terminal state", {
      expected: thread.status,
      actual: payload.toStatus,
    });
  }
  thread.status = payload.toStatus;
  thread.headLsn = row.lsn;
  thread.updatedAtMs = payload.changedAt.getTime();
  thread.closedAtMs =
    payload.toStatus === "merged" || payload.toStatus === "closed"
      ? payload.changedAt.getTime()
      : null;
}

function applyReceipt(
  row: ReplayEventRow,
  threads: Map<string, ReplayedThreadState>,
  taskThreadIds: Map<string, string>,
  discrepancies: ThreadReplayCheckDiscrepancy[],
): void {
  const receipt = receiptFromJson(new TextDecoder().decode(row.payload));
  if (receipt.schemaVersion !== 2) return;
  const threadId = receipt.threadId ?? SYSTEM_INBOX_THREAD_ID;
  const thread = threads.get(threadId);
  if (thread === undefined) {
    logInvariant(discrepancies, row, threadId, "receipt references a missing thread", {
      actual: receipt.id,
    });
    return;
  }
  const existingTaskThreadId = taskThreadIds.get(receipt.taskId);
  if (existingTaskThreadId !== undefined && existingTaskThreadId !== threadId) {
    logInvariant(discrepancies, row, threadId, "task id assigned to multiple threads", {
      expected: existingTaskThreadId,
      actual: threadId,
    });
  }
  taskThreadIds.set(receipt.taskId, threadId);
  thread.receiptIds.push(receipt.id);
  if (!thread.taskIds.includes(receipt.taskId)) {
    thread.taskIds.push(receipt.taskId);
  }
  thread.latestReceiptStatus = receipt.status;
}

function compareThreadRows(
  replayed: Map<string, ThreadProjectionSnapshotRow>,
  stored: Map<string, ThreadProjectionSnapshotRow>,
  discrepancies: ThreadReplayCheckDiscrepancy[],
): void {
  for (const threadId of sortedUnion(replayed, stored)) {
    const replayedRow = replayed.get(threadId);
    const storedRow = stored.get(threadId);
    if (replayedRow === undefined && storedRow !== undefined) {
      discrepancies.push({ kind: "thread_state_row_ghost", threadId: asThreadId(threadId) });
      continue;
    }
    if (replayedRow !== undefined && storedRow === undefined) {
      discrepancies.push({ kind: "thread_state_row_missing", threadId: asThreadId(threadId) });
      continue;
    }
    if (replayedRow === undefined || storedRow === undefined) continue;
    for (const field of THREAD_STATE_COMPARE_FIELDS) {
      if (replayedRow[field] !== storedRow[field]) {
        discrepancies.push({
          kind: "thread_state_field_mismatch",
          threadId: asThreadId(threadId),
          field,
          replayed: primitiveToJson(replayedRow[field]),
          stored: primitiveToJson(storedRow[field]),
        });
      }
    }
  }
}

const THREAD_STATE_COMPARE_FIELDS = [
  "title",
  "status",
  "headLsn",
  "createdBy",
  "createdAtMs",
  "updatedAtMs",
  "closedAtMs",
  "specRevisionId",
  "specBaseRevisionId",
  "specContent",
  "specContentHash",
  "specAuthoredBy",
  "specAuthoredAtMs",
  "externalRefs",
] as const satisfies readonly (keyof ThreadProjectionSnapshotRow)[];

function liveThreadReceiptRefs(
  rows: readonly ThreadReceiptProjectionRow[],
): Map<string, ThreadReceiptRefs> {
  const out = new Map<
    string,
    { receiptIds: string[]; taskIds: string[]; latestReceiptStatus: ReceiptStatus | null }
  >();
  for (const row of rows) {
    const entry =
      out.get(row.threadId) ??
      (() => {
        const created = { receiptIds: [], taskIds: [], latestReceiptStatus: null };
        out.set(row.threadId, created);
        return created;
      })();
    entry.receiptIds.push(row.receiptId);
    if (!entry.taskIds.includes(row.taskId)) {
      entry.taskIds.push(row.taskId);
    }
    entry.latestReceiptStatus = receiptFromJson(new TextDecoder().decode(row.payload)).status;
  }
  return out;
}

function compareReceiptRefs(
  replayed: Map<string, ThreadReceiptRefs>,
  stored: Map<string, ThreadReceiptRefs>,
  discrepancies: ThreadReplayCheckDiscrepancy[],
): void {
  for (const threadId of sortedUnion(replayed, stored)) {
    const replayedRefs = replayed.get(threadId) ?? EMPTY_RECEIPT_REFS;
    const storedRefs = stored.get(threadId) ?? EMPTY_RECEIPT_REFS;
    compareJsonField(
      discrepancies,
      "thread_receipt_index_mismatch",
      threadId,
      "receiptIds",
      replayedRefs.receiptIds,
      storedRefs.receiptIds,
    );
    compareJsonField(
      discrepancies,
      "thread_receipt_index_mismatch",
      threadId,
      "taskIds",
      replayedRefs.taskIds,
      storedRefs.taskIds,
    );
    compareJsonField(
      discrepancies,
      "thread_receipt_index_mismatch",
      threadId,
      "latestReceiptStatus",
      replayedRefs.latestReceiptStatus,
      storedRefs.latestReceiptStatus,
    );
  }
}

const EMPTY_RECEIPT_REFS: ThreadReceiptRefs = Object.freeze({
  receiptIds: Object.freeze([]),
  taskIds: Object.freeze([]),
  latestReceiptStatus: null,
});

function pinnedApprovalProjection(
  rows: readonly ApprovalProjectionSnapshotRow[],
  discrepancies: ThreadReplayCheckDiscrepancy[],
): Map<string, PinnedApprovalProjection> {
  const sorted = [...rows].sort((left, right) => left.headLsn - right.headLsn);
  const mutable = new Map<string, { approvalIds: string[]; headLsn: number }>();
  for (const row of sorted) {
    if (row.threadId === null) {
      discrepancies.push({
        kind: "thread_log_invariant_violation",
        lsn: lsnFromV1Number(row.headLsn),
        eventType: "approval.requested",
        reason: "approval has no thread id",
        actual: row.approvalId,
      });
      continue;
    }
    if (row.status !== "pending") continue;
    const entry =
      mutable.get(row.threadId) ??
      (() => {
        const created = { approvalIds: [], headLsn: 0 };
        mutable.set(row.threadId, created);
        return created;
      })();
    entry.approvalIds.push(row.approvalId);
    entry.headLsn = Math.max(entry.headLsn, row.headLsn);
  }
  const out = new Map<string, PinnedApprovalProjection>();
  for (const [threadId, entry] of mutable) {
    out.set(threadId, {
      approvalIds: entry.approvalIds,
      headLsn: entry.headLsn === 0 ? null : lsnFromV1Number(entry.headLsn),
    });
  }
  return out;
}

function comparePinnedApprovals(
  replayed: Map<string, PinnedApprovalProjection>,
  stored: Map<string, PinnedApprovalProjection>,
  replayedThreads: Map<string, ReplayedThreadState>,
  storedThreads: Map<string, ThreadProjectionSnapshotRow>,
  discrepancies: ThreadReplayCheckDiscrepancy[],
): void {
  const threadIds = new Set<string>([...replayedThreads.keys(), ...storedThreads.keys()]);
  for (const threadId of sortedUnion(replayed, stored, threadIds)) {
    const replayedPins = replayed.get(threadId) ?? EMPTY_PINNED_APPROVALS;
    const storedPins = stored.get(threadId) ?? EMPTY_PINNED_APPROVALS;
    if (canonicalJSON(replayedPins) !== canonicalJSON(storedPins)) {
      discrepancies.push({
        kind: "thread_pinned_approvals_mismatch",
        threadId: asThreadId(threadId),
        replayed: pinnedToJson(replayedPins),
        stored: pinnedToJson(storedPins),
      });
    }
  }
}

const EMPTY_PINNED_APPROVALS: PinnedApprovalProjection = Object.freeze({
  approvalIds: Object.freeze([]),
  headLsn: null,
});

function effectiveStatusProjection(
  threads: Map<string, ReplayedThreadState>,
  pinned: Map<string, PinnedApprovalProjection>,
): Map<string, EffectiveStatusProjection> {
  const out = new Map<string, EffectiveStatusProjection>();
  for (const [threadId, row] of threads) {
    out.set(
      threadId,
      deriveEffectiveStatus(
        row.status,
        pinned.get(threadId)?.approvalIds.length ?? 0,
        row.latestReceiptStatus,
      ),
    );
  }
  return out;
}

function effectiveStatusProjectionFromLive(
  threads: Map<string, ThreadProjectionSnapshotRow>,
  receiptRefs: Map<string, ThreadReceiptRefs>,
  pinned: Map<string, PinnedApprovalProjection>,
): Map<string, EffectiveStatusProjection> {
  const out = new Map<string, EffectiveStatusProjection>();
  for (const [threadId, row] of threads) {
    out.set(
      threadId,
      deriveEffectiveStatus(
        row.status,
        pinned.get(threadId)?.approvalIds.length ?? 0,
        receiptRefs.get(threadId)?.latestReceiptStatus ?? null,
      ),
    );
  }
  return out;
}

function deriveEffectiveStatus(
  storedStatus: string,
  pendingApprovalCount: number,
  latestReceiptStatus: ReceiptStatus | null,
): EffectiveStatusProjection {
  if (!THREAD_STATUS_SET.has(storedStatus)) {
    return {
      effectiveStatus: "invalid_status",
      attentionReason: null,
      boardColumn: "invalid_status",
      currentSeat: "invalid_status",
      pendingApprovalCount,
    };
  }
  const derived = deriveThreadEffectiveStatus({
    storedStatus: storedStatus as ThreadStatus,
    pendingApprovalCount,
    ...(latestReceiptStatus === null ? {} : { latestReceiptStatus }),
  });
  return {
    effectiveStatus: derived.effectiveStatus,
    attentionReason: derived.attentionReason ?? null,
    boardColumn: derived.boardColumn,
    currentSeat: derived.currentSeat,
    pendingApprovalCount,
  };
}

function compareEffectiveStatus(
  replayed: Map<string, EffectiveStatusProjection>,
  stored: Map<string, EffectiveStatusProjection>,
  discrepancies: ThreadReplayCheckDiscrepancy[],
): void {
  for (const threadId of sortedUnion(replayed, stored)) {
    const replayedEffective = replayed.get(threadId);
    const storedEffective = stored.get(threadId);
    if (replayedEffective === undefined || storedEffective === undefined) continue;
    for (const field of EFFECTIVE_STATUS_COMPARE_FIELDS) {
      if (replayedEffective[field] !== storedEffective[field]) {
        discrepancies.push({
          kind: "thread_effective_status_mismatch",
          threadId: asThreadId(threadId),
          field,
          replayed: primitiveToJson(replayedEffective[field]),
          stored: primitiveToJson(storedEffective[field]),
        });
      }
    }
  }
}

const EFFECTIVE_STATUS_COMPARE_FIELDS = [
  "effectiveStatus",
  "attentionReason",
  "boardColumn",
  "currentSeat",
  "pendingApprovalCount",
] as const satisfies readonly (keyof EffectiveStatusProjection)[];

function threadProjectionRow(row: ReplayedThreadState): ThreadProjectionSnapshotRow {
  return {
    threadId: row.threadId,
    title: row.title,
    status: row.status,
    headLsn: row.headLsn,
    createdBy: row.createdBy,
    createdAtMs: row.createdAtMs,
    updatedAtMs: row.updatedAtMs,
    closedAtMs: row.closedAtMs,
    specRevisionId: row.specRevisionId,
    specBaseRevisionId: row.specBaseRevisionId,
    specContent: row.specContent,
    specContentHash: row.specContentHash,
    specAuthoredBy: row.specAuthoredBy,
    specAuthoredAtMs: row.specAuthoredAtMs,
    externalRefs: row.externalRefs,
  };
}

function compareJsonField(
  discrepancies: ThreadReplayCheckDiscrepancy[],
  kind: "thread_receipt_index_mismatch",
  threadId: string,
  field: "receiptIds" | "taskIds" | "latestReceiptStatus",
  replayed: JsonValue,
  stored: JsonValue,
): void {
  if (canonicalJSON(replayed) === canonicalJSON(stored)) return;
  discrepancies.push({
    kind,
    threadId: asThreadId(threadId),
    field,
    replayed,
    stored,
  });
}

function pinnedToJson(value: PinnedApprovalProjection): JsonValue {
  return {
    approvalIds: value.approvalIds,
    headLsn: value.headLsn,
  };
}

function primitiveToJson(value: string | number | null): JsonValue {
  return value;
}

function sortedUnion<T>(
  left: Map<string, T>,
  right: Map<string, T>,
  extra: ReadonlySet<string> = new Set<string>(),
): string[] {
  return [...new Set<string>([...left.keys(), ...right.keys(), ...extra])].sort();
}

function logInvariant(
  discrepancies: ThreadReplayCheckDiscrepancy[],
  row: ReplayEventRow,
  threadId: string,
  reason: string,
  values: { readonly expected?: JsonValue; readonly actual?: JsonValue } = {},
): void {
  discrepancies.push({
    kind: "thread_log_invariant_violation",
    lsn: lsnFromV1Number(row.lsn),
    eventType: row.type,
    reason,
    threadId: asThreadId(threadId),
    ...values,
  });
}

function boundedReason(err: unknown): string {
  const message = err instanceof Error ? err.message : String(err);
  return message.length > 8_000 ? `${message.slice(0, 8_000)}...` : message;
}
