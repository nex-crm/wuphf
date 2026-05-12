// Test-only construction helper for `SqliteReceiptStore`. NOT exposed
// via the `@wuphf/broker/sqlite` subpath — production callers cannot
// reach this file. Tests inside the broker package import via the
// relative path.
//
// The helper exists so spec files can construct a store against a
// caller-supplied `Database` handle (pre-migrated `:memory:` DB, stubbed
// `EventLog`) without going through `SqliteReceiptStore.open()` and its
// `openDatabase` + `runMigrations` chain. Outside test code, the only
// supported construction path is `SqliteReceiptStore.open(config)` so
// migrations always run before any read or write.

import type Database from "better-sqlite3";

import type { EventLog } from "../event-log/index.ts";
import { SqliteReceiptStore } from "../sqlite-receipt-store.ts";

// Constructor shape captured here so the cast below is `unknown` →
// typed-constructor rather than `unknown` → `any`. The private modifier
// on `SqliteReceiptStore`'s constructor is a TypeScript-level fence;
// the call below bypasses it deliberately for test-only construction.
type SqliteReceiptStoreCtor = new (
  db: Database.Database,
  eventLog?: EventLog,
  maxReceipts?: number,
) => SqliteReceiptStore;

export function constructSqliteReceiptStoreForTesting(
  db: Database.Database,
  eventLog?: EventLog,
  maxReceipts?: number,
): SqliteReceiptStore {
  return new (SqliteReceiptStore as unknown as SqliteReceiptStoreCtor)(db, eventLog, maxReceipts);
}
