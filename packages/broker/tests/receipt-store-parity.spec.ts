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
import { describe, expect, it } from "vitest";

import {
  encodeListCursor,
  InMemoryReceiptStore,
  MAX_LIST_LIMIT,
  type ReceiptStore,
} from "../src/receipt-store.ts";
import { SqliteReceiptStore } from "../src/sqlite-receipt-store.ts";

const TASK_ID = "01ARZ3NDEKTSV4RRFFQ69G5FAW";
const THREAD_A = "01ARZ3NDEKTSV4RRFFQ69G5FAZ";
const THREAD_B = "01ARZ3NDEKTSV4RRFFQ69G5FB0";
const ULID_TIME_PREFIX = "01ARZ3NDEK";
const ULID_ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";

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

function receiptIdAt(index: number): string {
  let value = index;
  let suffix = "";
  for (let i = 0; i < 16; i += 1) {
    suffix = ULID_ALPHABET[value % ULID_ALPHABET.length] + suffix;
    value = Math.floor(value / ULID_ALPHABET.length);
  }
  return `${ULID_TIME_PREFIX}${suffix}`;
}

function closeStore(store: ReceiptStore): void {
  if (hasClose(store)) {
    store.close();
  }
}

function hasClose(store: ReceiptStore): store is ReceiptStore & { readonly close: () => void } {
  const candidate = store as { readonly close?: unknown };
  return typeof candidate.close === "function";
}

function runReceiptStoreContractTests(factory: () => Promise<ReceiptStore>): void {
  it("put + get round-trips a receipt", async () => {
    const store = await factory();
    try {
      const receipt = minimalReceiptV1(receiptIdAt(1));

      expect(await store.put(receipt)).toEqual({ existed: false });
      expect(await store.get(receipt.id)).toEqual(receipt);
    } finally {
      closeStore(store);
    }
  });

  it("duplicate put returns existed:true and does not overwrite", async () => {
    const store = await factory();
    try {
      const first = minimalReceiptV1(receiptIdAt(1));
      const second = { ...first, model: "different" };

      await store.put(first);
      expect(await store.put(second)).toEqual({ existed: true });
      expect(await store.get(first.id)).toEqual(first);
    } finally {
      closeStore(store);
    }
  });

  it("list returns receipts in insertion/LSN order", async () => {
    const store = await factory();
    try {
      const receipts = [
        minimalReceiptV1(receiptIdAt(1)),
        minimalReceiptV1(receiptIdAt(2)),
        minimalReceiptV1(receiptIdAt(3)),
      ];
      for (const receipt of receipts) {
        await store.put(receipt);
      }

      const page = await store.list();

      expect(page.items.map((receipt) => receipt.id)).toEqual(
        receipts.map((receipt) => receipt.id),
      );
      expect(page.nextCursor).toBeNull();
    } finally {
      closeStore(store);
    }
  });

  it("cursor pagination returns byte-identical cursors for the same logical LSN", async () => {
    const store = await factory();
    try {
      const receipts = [
        minimalReceiptV1(receiptIdAt(1)),
        minimalReceiptV1(receiptIdAt(2)),
        minimalReceiptV1(receiptIdAt(3)),
        minimalReceiptV1(receiptIdAt(4)),
        minimalReceiptV1(receiptIdAt(5)),
      ];
      for (const receipt of receipts) {
        await store.put(receipt);
      }

      const firstPage = await store.list({ limit: 2 });
      expect(firstPage.items.map((receipt) => receipt.id)).toEqual(
        receipts.slice(0, 2).map((receipt) => receipt.id),
      );
      expect(firstPage.nextCursor).toBe(encodeListCursor(2));
      if (firstPage.nextCursor === null) {
        throw new Error("expected first page cursor");
      }

      const secondPage = await store.list({ limit: 2, cursor: firstPage.nextCursor });
      expect(secondPage.items.map((receipt) => receipt.id)).toEqual(
        receipts.slice(2, 4).map((receipt) => receipt.id),
      );
      expect(secondPage.nextCursor).toBe(encodeListCursor(4));
      if (secondPage.nextCursor === null) {
        throw new Error("expected second page cursor");
      }

      const thirdPage = await store.list({ limit: 2, cursor: secondPage.nextCursor });
      expect(thirdPage.items.map((receipt) => receipt.id)).toEqual(
        receipts.slice(4).map((receipt) => receipt.id),
      );
      expect(thirdPage.nextCursor).toBeNull();
    } finally {
      closeStore(store);
    }
  });

  it("threadId filter returns only that thread in LSN order", async () => {
    const store = await factory();
    try {
      const a1 = minimalReceiptV2(receiptIdAt(1), THREAD_A);
      const b1 = minimalReceiptV2(receiptIdAt(2), THREAD_B);
      const a2 = minimalReceiptV2(receiptIdAt(3), THREAD_A);
      await store.put(a1);
      await store.put(b1);
      await store.put(a2);

      const page = await store.list({ threadId: asThreadId(THREAD_A) });

      expect(page.items.map((receipt) => receipt.id)).toEqual([a1.id, a2.id]);
      expect(page.nextCursor).toBeNull();
    } finally {
      closeStore(store);
    }
  });

  it("threadId filter excludes V1 receipts", async () => {
    const store = await factory();
    try {
      const v1 = minimalReceiptV1(receiptIdAt(1));
      const v2 = minimalReceiptV2(receiptIdAt(2), THREAD_A);
      await store.put(v1);
      await store.put(v2);

      const page = await store.list({ threadId: asThreadId(THREAD_A) });

      expect(page.items.map((receipt) => receipt.id)).toEqual([v2.id]);
      expect(page.nextCursor).toBeNull();
    } finally {
      closeStore(store);
    }
  });

  it("limit clamping returns at most MAX_LIST_LIMIT rows and a cursor when more exist", async () => {
    const store = await factory();
    try {
      for (let i = 1; i <= MAX_LIST_LIMIT + 1; i += 1) {
        await store.put(minimalReceiptV1(receiptIdAt(i)));
      }

      const page = await store.list({ limit: MAX_LIST_LIMIT + 5_000 });

      expect(page.items).toHaveLength(MAX_LIST_LIMIT);
      expect(page.nextCursor).toBe(encodeListCursor(MAX_LIST_LIMIT));
    } finally {
      closeStore(store);
    }
  });
}

describe("ReceiptStore parity", () => {
  describe("InMemoryReceiptStore", () => {
    runReceiptStoreContractTests(async () => new InMemoryReceiptStore());
  });

  describe("SqliteReceiptStore", () => {
    runReceiptStoreContractTests(async () => SqliteReceiptStore.open({ path: ":memory:" }));
  });
});
