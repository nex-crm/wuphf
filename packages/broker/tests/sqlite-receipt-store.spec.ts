import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import {
  asAgentSlug,
  asProviderKind,
  asReceiptId,
  asTaskId,
  asThreadId,
  type ReceiptSnapshot,
  SanitizedString,
  sha256Hex,
} from "@wuphf/protocol";
import type Database from "better-sqlite3";
import { afterEach, describe, expect, it } from "vitest";

import { openDatabase, runMigrations } from "../src/event-log/index.ts";
import { InvalidListCursorError, InvalidListLimitError } from "../src/receipt-store.ts";
import { SqliteReceiptStore } from "../src/sqlite-receipt-store.ts";

const TASK_ID = "01ARZ3NDEKTSV4RRFFQ69G5FAW";
const THREAD_A = "01ARZ3NDEKTSV4RRFFQ69G5FAZ";
const THREAD_B = "01ARZ3NDEKTSV4RRFFQ69G5FB0";
const ULID_TIME_PREFIX = "01ARZ3NDEK";
const ULID_ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";

const tempDirs: string[] = [];

afterEach(() => {
  for (const dir of tempDirs.splice(0)) {
    rmSync(dir, { recursive: true, force: true });
  }
});

function minimalReceiptV1(id: string): ReceiptSnapshot {
  return {
    id: asReceiptId(id),
    agentSlug: asAgentSlug("a"),
    taskId: asTaskId(TASK_ID),
    triggerKind: "human_message",
    triggerRef: "m",
    startedAt: new Date("2026-01-01T00:00:00.000Z"),
    finishedAt: new Date("2026-01-01T00:01:00.000Z"),
    status: "ok",
    providerKind: asProviderKind("anthropic"),
    model: "m",
    promptHash: sha256Hex("p"),
    toolManifest: sha256Hex("t"),
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

function minimalReceiptV2(id: string, threadIdStr: string): ReceiptSnapshot {
  return { ...minimalReceiptV1(id), threadId: asThreadId(threadIdStr), schemaVersion: 2 };
}

function openStore(): { readonly db: Database.Database; readonly store: SqliteReceiptStore } {
  const db = openDatabase({ path: ":memory:" });
  runMigrations(db);
  return { db, store: new SqliteReceiptStore(db) };
}

function tempDbPath(): string {
  const dir = mkdtempSync(join(tmpdir(), "wuphf-sqlite-store-"));
  tempDirs.push(dir);
  return join(dir, "event-log.sqlite");
}

function countRows(db: Database.Database, tableName: "event_log" | "receipts_projection"): number {
  const row = db
    .prepare<[], { readonly count: number }>(`SELECT COUNT(*) AS count FROM ${tableName}`)
    .get();
  if (row === undefined) {
    throw new Error(`count query returned no row for ${tableName}`);
  }
  return row.count;
}

function maxEventLogLsn(db: Database.Database): number {
  const row = db
    .prepare<[], { readonly lsn: number }>("SELECT COALESCE(MAX(lsn), 0) AS lsn FROM event_log")
    .get();
  if (row === undefined) {
    throw new Error("max lsn query returned no row");
  }
  return row.lsn;
}

function receiptIdAt(index: number): string {
  let value = index;
  let suffix = "";
  for (let i = 0; i < 16; i += 1) {
    suffix = ULID_ALPHABET[value % ULID_ALPHABET.length] + suffix;
    value = Math.floor(value / ULID_ALPHABET.length);
  }
  return `${ULID_TIME_PREFIX}${suffix}`;
}

describe("SqliteReceiptStore", () => {
  it("put returns existed:false, then get and list immediately include the receipt", async () => {
    const { store } = openStore();
    try {
      const receipt = minimalReceiptV2(receiptIdAt(1), THREAD_A);

      await expect(store.put(receipt)).resolves.toEqual({ existed: false });
      await expect(store.get(receipt.id)).resolves.toEqual(receipt);
      await expect(store.list({ threadId: asThreadId(THREAD_A) })).resolves.toMatchObject({
        items: [receipt],
        nextCursor: null,
      });
    } finally {
      store.close();
    }
  });

  it("duplicate put returns existed:true and leaves event_log at one row", async () => {
    const { db, store } = openStore();
    try {
      const first = minimalReceiptV1(receiptIdAt(1));
      const second = { ...first, model: "different" };

      expect(await store.put(first)).toEqual({ existed: false });
      expect(await store.put(second)).toEqual({ existed: true });

      expect(await store.get(first.id)).toEqual(first);
      expect(countRows(db, "event_log")).toBe(1);
      expect(countRows(db, "receipts_projection")).toBe(1);
    } finally {
      store.close();
    }
  });

  it("list({ threadId }) returns receipts for that thread only in LSN order", async () => {
    const { store } = openStore();
    try {
      const a1 = minimalReceiptV2(receiptIdAt(1), THREAD_A);
      const b1 = minimalReceiptV2(receiptIdAt(2), THREAD_B);
      const a2 = minimalReceiptV2(receiptIdAt(3), THREAD_A);
      const v1 = minimalReceiptV1(receiptIdAt(4));
      await store.put(a1);
      await store.put(b1);
      await store.put(a2);
      await store.put(v1);

      const page = await store.list({ threadId: asThreadId(THREAD_A) });

      expect(page.items.map((receipt) => receipt.id)).toEqual([a1.id, a2.id]);
      expect(page.nextCursor).toBeNull();
    } finally {
      store.close();
    }
  });

  it("list({ threadId, limit }) returns a bounded page and nextCursor when more rows exist", async () => {
    const { store } = openStore();
    try {
      const receipts = Array.from({ length: 6 }, (_, index) =>
        minimalReceiptV2(receiptIdAt(index + 1), THREAD_A),
      );
      for (const receipt of receipts) {
        await store.put(receipt);
      }

      const page = await store.list({ threadId: asThreadId(THREAD_A), limit: 5 });

      expect(page.items.map((receipt) => receipt.id)).toEqual(
        receipts.slice(0, 5).map((receipt) => receipt.id),
      );
      expect(page.nextCursor).not.toBeNull();
    } finally {
      store.close();
    }
  });

  it("list({ cursor }) skips already-seen items", async () => {
    const { store } = openStore();
    try {
      const receipts = Array.from({ length: 5 }, (_, index) =>
        minimalReceiptV2(receiptIdAt(index + 1), THREAD_A),
      );
      for (const receipt of receipts) {
        await store.put(receipt);
      }

      const firstPage = await store.list({ threadId: asThreadId(THREAD_A), limit: 2 });
      expect(firstPage.nextCursor).not.toBeNull();
      const secondPage = await store.list({
        threadId: asThreadId(THREAD_A),
        limit: 2,
        cursor: firstPage.nextCursor as string,
      });

      expect(secondPage.items.map((receipt) => receipt.id)).toEqual(
        receipts.slice(2, 4).map((receipt) => receipt.id),
      );
      expect(secondPage.nextCursor).not.toBeNull();
    } finally {
      store.close();
    }
  });

  it("list({ limit: 9999 }) clamps to 1000", async () => {
    const { store } = openStore();
    try {
      for (let i = 1; i <= 1_001; i += 1) {
        await store.put(minimalReceiptV1(receiptIdAt(i)));
      }

      const page = await store.list({ limit: 9_999 });

      expect(page.items).toHaveLength(1_000);
      expect(page.nextCursor).not.toBeNull();
    } finally {
      store.close();
    }
  });

  it("list({ limit: 0 }) rejects", async () => {
    const { store } = openStore();
    try {
      await expect(store.list({ limit: 0 })).rejects.toBeInstanceOf(InvalidListLimitError);
    } finally {
      store.close();
    }
  });

  it("list({ cursor: malformed }) rejects", async () => {
    const { store } = openStore();
    try {
      await expect(store.list({ cursor: "not-base64-!@#" })).rejects.toBeInstanceOf(
        InvalidListCursorError,
      );
    } finally {
      store.close();
    }
  });

  it("close is idempotent", () => {
    const { store } = openStore();

    store.close();
    expect(() => store.close()).not.toThrow();
  });

  it("persists receipts across restart and continues the LSN sequence", async () => {
    const path = tempDbPath();
    const firstDb = openDatabase({ path });
    runMigrations(firstDb);
    const firstStore = new SqliteReceiptStore(firstDb);
    const first = minimalReceiptV2(receiptIdAt(1), THREAD_A);
    await firstStore.put(first);
    firstStore.close();

    const secondDb = openDatabase({ path });
    runMigrations(secondDb);
    const secondStore = new SqliteReceiptStore(secondDb);
    try {
      const second = minimalReceiptV2(receiptIdAt(2), THREAD_A);

      expect(await secondStore.get(first.id)).toEqual(first);
      expect(await secondStore.put(second)).toEqual({ existed: false });
      expect(maxEventLogLsn(secondDb)).toBe(2);
      expect((await secondStore.list()).items.map((receipt) => receipt.id)).toEqual([
        first.id,
        second.id,
      ]);
    } finally {
      secondStore.close();
    }
  });

  it("rolls back event_log when projection insert fails in the same transaction", async () => {
    const { db, store } = openStore();
    try {
      db.exec(`
        CREATE TRIGGER fail_receipts_projection_insert
        BEFORE INSERT ON receipts_projection
        BEGIN
          SELECT RAISE(ABORT, 'forced_projection_failure');
        END;
      `);

      await expect(store.put(minimalReceiptV2(receiptIdAt(1), THREAD_A))).rejects.toThrow(
        /forced_projection_failure/,
      );
      expect(countRows(db, "event_log")).toBe(0);
      expect(countRows(db, "receipts_projection")).toBe(0);
    } finally {
      store.close();
    }
  });
});
