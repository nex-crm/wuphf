import {
  asIdempotencyKey,
  asSignerIdentity,
  asThreadId,
  canonicalJSON,
  type ThreadExternalRefs,
  type ThreadId,
  threadMutationResponseToJsonValue,
} from "@wuphf/protocol";
import type Database from "better-sqlite3";

import type { ApprovalAppender, ApprovalProjection } from "../approvals/index.ts";
import type { EventLog } from "../event-log/index.ts";
import type { SqliteReceiptStore } from "../sqlite-receipt-store.ts";
import { createThreadAppender, type ThreadAppender } from "./appender.ts";
import type { ParsedIdempotencyKey } from "./idempotency.ts";
import { createThreadStateStore, type ThreadStateStore } from "./projections.ts";
import { createThreadReceiptIndexStore, type ThreadReceiptIndexStore } from "./receipt-index.ts";

export const SYSTEM_INBOX_THREAD_ID: ThreadId = asThreadId("00000000000000000000000001");

const SYSTEM_INBOX_IDEMPOTENCY: ParsedIdempotencyKey = Object.freeze({
  raw: `system_thread_inbox_${SYSTEM_INBOX_THREAD_ID}`,
  command: "thread.create",
  ulid: SYSTEM_INBOX_THREAD_ID,
});
const SYSTEM_SIGNER = asSignerIdentity("system");
const SYSTEM_INBOX_EXTERNAL_REFS: ThreadExternalRefs = Object.freeze({
  sourceUrls: Object.freeze([]),
  entityIds: Object.freeze([]),
});

export interface ThreadSubsystem {
  readonly appender: ThreadAppender;
  readonly state: ThreadStateStore;
  readonly receiptIndex: ThreadReceiptIndexStore;
  readonly receiptStore: SqliteReceiptStore;
  readonly inboxThreadId: ThreadId;
  sharesApprovalProvenance(appender: ApprovalAppender, projection: ApprovalProjection): boolean;
  rebuildFromLog(fromLsn?: number): void;
}

export function createThreadSubsystem(
  db: Database.Database,
  eventLog: EventLog,
  receiptStore: SqliteReceiptStore,
): ThreadSubsystem {
  if (!receiptStore.sharesProvenance(db, eventLog)) {
    throw new Error("createThreadSubsystem: receiptStore must share db and eventLog provenance");
  }
  const state = createThreadStateStore(db);
  const receiptIndex = createThreadReceiptIndexStore(db);
  const appender = createThreadAppender(db, eventLog, state);
  ensureSystemInboxThread(appender, state);
  return {
    appender,
    state,
    receiptIndex,
    receiptStore,
    inboxThreadId: SYSTEM_INBOX_THREAD_ID,
    sharesApprovalProvenance(appender: ApprovalAppender, projection: ApprovalProjection): boolean {
      return appender.sharesProvenance(db, eventLog) && projection.sharesProvenance(db, eventLog);
    },
    rebuildFromLog(fromLsn = 0): void {
      if (fromLsn === 0) {
        receiptIndex.clear();
      }
      state.rebuildFromLog(eventLog, fromLsn);
      receiptIndex.rebuildFromLog(eventLog, fromLsn);
      ensureSystemInboxThread(appender, state);
    },
  };
}

function ensureSystemInboxThread(appender: ThreadAppender, state: ThreadStateStore): void {
  if (state.getById(SYSTEM_INBOX_THREAD_ID) !== null) return;
  appender.appendCreateIdempotent({
    command: {
      kind: "thread.create",
      idempotencyKey: asIdempotencyKey(SYSTEM_INBOX_THREAD_ID),
      threadId: SYSTEM_INBOX_THREAD_ID,
      title: "Inbox",
      createdBy: SYSTEM_SIGNER,
      createdAt: new Date(0),
      externalRefs: SYSTEM_INBOX_EXTERNAL_REFS,
      content: { purpose: "system_inbox" },
    },
    idempotency: SYSTEM_INBOX_IDEMPOTENCY,
    requestFingerprint: "system:thread-inbox",
    nowMs: 0,
    render: (applied) => ({
      statusCode: 201,
      payload: Buffer.from(
        canonicalJSON(
          threadMutationResponseToJsonValue({
            threadId: applied.threadId,
            headLsn: applied.headLsn,
            revisionId: applied.revisionId,
            contentHash: applied.contentHash,
          }),
        ),
        "utf8",
      ),
    }),
  });
}
