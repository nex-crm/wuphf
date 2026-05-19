import type Database from "better-sqlite3";

import type { EventLog } from "../event-log/index.ts";
import type { SqliteReceiptStore } from "../sqlite-receipt-store.ts";
import { createThreadAppender, type ThreadAppender } from "./appender.ts";
import { createThreadStateStore, type ThreadStateStore } from "./projections.ts";
import { createThreadReceiptIndexStore, type ThreadReceiptIndexStore } from "./receipt-index.ts";

export interface ThreadSubsystem {
  readonly appender: ThreadAppender;
  readonly state: ThreadStateStore;
  readonly receiptIndex: ThreadReceiptIndexStore;
  readonly receiptStore: SqliteReceiptStore;
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
  return {
    appender,
    state,
    receiptIndex,
    receiptStore,
    rebuildFromLog(fromLsn = 0): void {
      if (fromLsn === 0) {
        receiptIndex.clear();
      }
      state.rebuildFromLog(eventLog, fromLsn);
      receiptIndex.rebuildFromLog(eventLog, fromLsn);
    },
  };
}
