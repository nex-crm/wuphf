import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { DatabaseSync } from "node:sqlite";

import { afterEach, describe, expect, it } from "vitest";

import {
  CURRENT_SCHEMA_VERSION,
  createEventLog,
  openDatabase,
  runMigrations,
} from "../src/event-log/index.ts";

const tempDirs: string[] = [];

function userVersion(db: DatabaseSync): number {
  return (db.prepare("PRAGMA user_version").get() as { readonly user_version: number })
    .user_version;
}

function sqliteMasterName(db: DatabaseSync, type: string, name: string): string | undefined {
  return (
    db.prepare("SELECT name FROM sqlite_master WHERE type = ? AND name = ?").get(type, name) as
      | { readonly name: string }
      | undefined
  )?.name;
}

afterEach(() => {
  for (const dir of tempDirs.splice(0)) {
    rmSync(dir, { recursive: true, force: true });
  }
});

function tempDbPath(): string {
  const dir = mkdtempSync(join(tmpdir(), "wuphf-event-log-"));
  tempDirs.push(dir);
  return join(dir, "event-log.sqlite");
}

describe("event log", () => {
  it("append assigns monotonically increasing LSNs", () => {
    const db = openDatabase({ path: ":memory:" });
    try {
      runMigrations(db);
      const eventLog = createEventLog(db);

      const first = eventLog.append({ type: "receipt.put", payload: Buffer.from("one") });
      const second = eventLog.append({ type: "receipt.put", payload: Buffer.from("two") });
      const third = eventLog.append({ type: "receipt.put", payload: Buffer.from("three") });

      expect([first, second, third]).toEqual([1, 2, 3]);
    } finally {
      db.close();
    }
  });

  it("readFromLsn skips rows at or before fromLsn and honors limit", () => {
    const db = openDatabase({ path: ":memory:" });
    try {
      runMigrations(db);
      const eventLog = createEventLog(db);
      for (let i = 1; i <= 12; i += 1) {
        eventLog.append({ type: "receipt.put", payload: Buffer.from(`payload-${i}`) });
      }

      expect(eventLog.readFromLsn(0, 10).map((record) => record.lsn)).toEqual([
        1, 2, 3, 4, 5, 6, 7, 8, 9, 10,
      ]);
      expect(eventLog.readFromLsn(5, 10).map((record) => record.lsn)).toEqual([
        6, 7, 8, 9, 10, 11, 12,
      ]);
      expect(eventLog.readFromLsn(9_999, 10)).toEqual([]);
    } finally {
      db.close();
    }
  });

  it("highestLsn returns zero for an empty log and then tracks the last append", () => {
    const db = openDatabase({ path: ":memory:" });
    try {
      runMigrations(db);
      const eventLog = createEventLog(db);

      expect(eventLog.highestLsn()).toBe(0);
      eventLog.append({ type: "receipt.put", payload: Buffer.from("one") });
      const last = eventLog.append({ type: "receipt.put", payload: Buffer.from("two") });

      expect(eventLog.highestLsn()).toBe(last);
    } finally {
      db.close();
    }
  });

  it("migrations are idempotent across repeated opens", () => {
    const path = tempDbPath();
    const first = openDatabase({ path });
    try {
      runMigrations(first);
      expect(userVersion(first)).toBe(CURRENT_SCHEMA_VERSION);
      expect(sqliteMasterName(first, "table", "event_log")).toBe("event_log");
    } finally {
      first.close();
    }

    const second = openDatabase({ path });
    try {
      runMigrations(second);
      expect(userVersion(second)).toBe(CURRENT_SCHEMA_VERSION);
      expect(sqliteMasterName(second, "table", "receipts_projection")).toBe("receipts_projection");
      expect(sqliteMasterName(second, "table", "webauthn_registered_credentials")).toBe(
        "webauthn_registered_credentials",
      );
      expect(sqliteMasterName(second, "table", "webauthn_challenges")).toBe("webauthn_challenges");
      expect(sqliteMasterName(second, "table", "webauthn_consumed_tokens")).toBe(
        "webauthn_consumed_tokens",
      );
      expect(sqliteMasterName(second, "index", "webauthn_challenges_expires_at_ms_idx")).toBe(
        "webauthn_challenges_expires_at_ms_idx",
      );
      expect(sqliteMasterName(second, "index", "webauthn_consumed_tokens_expires_at_ms_idx")).toBe(
        "webauthn_consumed_tokens_expires_at_ms_idx",
      );
      expect(
        second
          .prepare("PRAGMA foreign_key_list('pending_approvals')")
          .all()
          .map(
            (row) => row as { readonly table: string; readonly from: string; readonly to: string },
          )
          .map((row) => ({ table: row.table, from: row.from, to: row.to })),
      ).toContainEqual({ table: "threads", from: "thread_id", to: "thread_id" });
    } finally {
      second.close();
    }
  });
});
