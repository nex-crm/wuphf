import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { createEventLog, openDatabase, runMigrations } from "../src/event-log/index.ts";

const tempDirs: string[] = [];

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
      expect(first.pragma("user_version", { simple: true })).toBe(1);
      expect(
        first
          .prepare<[], { readonly name: string }>(
            "SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'event_log'",
          )
          .get()?.name,
      ).toBe("event_log");
    } finally {
      first.close();
    }

    const second = openDatabase({ path });
    try {
      runMigrations(second);
      expect(second.pragma("user_version", { simple: true })).toBe(1);
      expect(
        second
          .prepare<[], { readonly name: string }>(
            "SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'receipts_projection'",
          )
          .get()?.name,
      ).toBe("receipts_projection");
    } finally {
      second.close();
    }
  });
});
