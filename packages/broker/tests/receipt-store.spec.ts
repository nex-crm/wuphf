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
  InvalidListCursorError,
  InvalidListLimitError,
  MAX_LIST_LIMIT,
  ReceiptStoreFullError,
} from "../src/receipt-store.ts";

const TASK_ID = "01ARZ3NDEKTSV4RRFFQ69G5FAW";
const THREAD_A = "01ARZ3NDEKTSV4RRFFQ69G5FAZ";
const THREAD_B = "01ARZ3NDEKTSV4RRFFQ69G5FB0";

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

describe("InMemoryReceiptStore", () => {
  it("put + get round-trips identity-stable", async () => {
    const store = new InMemoryReceiptStore();
    const r = minimalReceiptV1("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    const result = await store.put(r);
    expect(result).toEqual({ existed: false });
    expect(await store.get(r.id)).toBe(r);
  });

  it("put returns existed:true on collision and does NOT overwrite", async () => {
    const store = new InMemoryReceiptStore();
    const r1 = minimalReceiptV1("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    const r2 = { ...r1, model: "different" };
    await store.put(r1);
    const second = await store.put(r2);
    expect(second).toEqual({ existed: true });
    expect(await store.get(r1.id)).toBe(r1);
  });

  it("get returns null for unknown id", async () => {
    const store = new InMemoryReceiptStore();
    expect(await store.get(asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV"))).toBeNull();
  });

  it("list() returns receipts in insertion (LSN) order", async () => {
    const store = new InMemoryReceiptStore();
    const a = minimalReceiptV1("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    const b = minimalReceiptV1("01ARZ3NDEKTSV4RRFFQ69G5FAY");
    const c = minimalReceiptV1("01ARZ3NDEKTSV4RRFFQ69G5FB1");
    await store.put(a);
    await store.put(b);
    await store.put(c);
    const all = await store.list();
    expect(all.items.map((r) => r.id)).toEqual([a.id, b.id, c.id]);
    expect(all.nextCursor).toBeNull();
  });

  it("list({threadId}) filters to v2 receipts of that thread", async () => {
    const store = new InMemoryReceiptStore();
    const a = minimalReceiptV2("01ARZ3NDEKTSV4RRFFQ69G5FAV", THREAD_A);
    const b = minimalReceiptV2("01ARZ3NDEKTSV4RRFFQ69G5FAY", THREAD_B);
    const c = minimalReceiptV2("01ARZ3NDEKTSV4RRFFQ69G5FB1", THREAD_A);
    await store.put(a);
    await store.put(b);
    await store.put(c);
    const inA = await store.list({ threadId: asThreadId(THREAD_A) });
    expect(inA.items.map((r) => r.id)).toEqual([a.id, c.id]);
    expect(inA.nextCursor).toBeNull();
  });

  it("list({threadId}) excludes v1 receipts (no threadId)", async () => {
    const store = new InMemoryReceiptStore();
    await store.put(minimalReceiptV1("01ARZ3NDEKTSV4RRFFQ69G5FAV"));
    await store.put(minimalReceiptV2("01ARZ3NDEKTSV4RRFFQ69G5FAY", THREAD_A));
    const inA = await store.list({ threadId: asThreadId(THREAD_A) });
    expect(inA.items.map((r) => r.id)).toEqual(["01ARZ3NDEKTSV4RRFFQ69G5FAY"]);
    expect(inA.nextCursor).toBeNull();
  });

  it("list({threadId}) returns empty page for unknown thread", async () => {
    const store = new InMemoryReceiptStore();
    await store.put(minimalReceiptV2("01ARZ3NDEKTSV4RRFFQ69G5FAV", THREAD_A));
    const inB = await store.list({ threadId: asThreadId(THREAD_B) });
    expect(inB.items).toEqual([]);
    expect(inB.nextCursor).toBeNull();
  });

  it("list paginates with cursor + limit and exposes a nextCursor for more pages", async () => {
    const store = new InMemoryReceiptStore();
    const ids = [
      "01ARZ3NDEKTSV4RRFFQ69G5FAV",
      "01ARZ3NDEKTSV4RRFFQ69G5FAY",
      "01ARZ3NDEKTSV4RRFFQ69G5FB1",
      "01ARZ3NDEKTSV4RRFFQ69G5FB2",
      "01ARZ3NDEKTSV4RRFFQ69G5FB3",
    ];
    for (const id of ids) {
      await store.put(minimalReceiptV2(id, THREAD_A));
    }
    const page1 = await store.list({ threadId: asThreadId(THREAD_A), limit: 2 });
    expect(page1.items.map((r) => r.id)).toEqual([ids[0], ids[1]]);
    expect(page1.nextCursor).not.toBeNull();
    const page2 = await store.list({
      threadId: asThreadId(THREAD_A),
      limit: 2,
      cursor: page1.nextCursor as string,
    });
    expect(page2.items.map((r) => r.id)).toEqual([ids[2], ids[3]]);
    expect(page2.nextCursor).not.toBeNull();
    const page3 = await store.list({
      threadId: asThreadId(THREAD_A),
      limit: 2,
      cursor: page2.nextCursor as string,
    });
    expect(page3.items.map((r) => r.id)).toEqual([ids[4]]);
    expect(page3.nextCursor).toBeNull();
  });

  it("list clamps limit above MAX_LIST_LIMIT instead of throwing", async () => {
    const store = new InMemoryReceiptStore();
    const r = minimalReceiptV1("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    await store.put(r);
    const page = await store.list({ limit: MAX_LIST_LIMIT + 5_000 });
    expect(page.items).toHaveLength(1);
    expect(page.nextCursor).toBeNull();
  });

  it("list throws InvalidListLimitError for non-positive or non-integer limits", async () => {
    const store = new InMemoryReceiptStore();
    await expect(store.list({ limit: 0 })).rejects.toBeInstanceOf(InvalidListLimitError);
    await expect(store.list({ limit: -1 })).rejects.toBeInstanceOf(InvalidListLimitError);
    await expect(store.list({ limit: 1.5 })).rejects.toBeInstanceOf(InvalidListLimitError);
  });

  it("list throws InvalidListCursorError for malformed cursors", async () => {
    const store = new InMemoryReceiptStore();
    await expect(store.list({ cursor: "" })).rejects.toBeInstanceOf(InvalidListCursorError);
    await expect(store.list({ cursor: "not-base64-!@#" })).rejects.toBeInstanceOf(
      InvalidListCursorError,
    );
    // Base64url of "foo:1" — wrong prefix.
    await expect(
      store.list({ cursor: Buffer.from("foo:1", "utf8").toString("base64url") }),
    ).rejects.toBeInstanceOf(InvalidListCursorError);
    // Non-canonical base64url: `bHNuOjE!` decodes to `lsn:1` via Node's
    // permissive `Buffer.from(_, "base64url")`. Strict alphabet validation
    // rejects this (triangulation T5).
    await expect(store.list({ cursor: "bHNuOjE!" })).rejects.toBeInstanceOf(InvalidListCursorError);
    // Trailing padding (`=`) is not part of canonical unpadded base64url.
    const padded = `${Buffer.from("lsn:1", "utf8").toString("base64url")}=`;
    await expect(store.list({ cursor: padded })).rejects.toBeInstanceOf(InvalidListCursorError);
    // LSN with leading zeros — not canonical decimal.
    await expect(
      store.list({ cursor: Buffer.from("lsn:01", "utf8").toString("base64url") }),
    ).rejects.toBeInstanceOf(InvalidListCursorError);
    // LSN zero — cursors are issued only after at least one item is returned,
    // so the smallest legal LSN inside a cursor is 1.
    await expect(
      store.list({ cursor: Buffer.from("lsn:0", "utf8").toString("base64url") }),
    ).rejects.toBeInstanceOf(InvalidListCursorError);
    // LSN above MAX_SAFE_INTEGER — JS rounding makes comparisons unstable.
    const unsafe = `${String(Number.MAX_SAFE_INTEGER)}0`; // 9_007_199_254_740_9910
    await expect(
      store.list({ cursor: Buffer.from(`lsn:${unsafe}`, "utf8").toString("base64url") }),
    ).rejects.toBeInstanceOf(InvalidListCursorError);
    // Non-canonical base64url aliases that decode to `lsn:1` via Node's
    // permissive decoder but fail strict round-trip (R2-A1). The
    // canonical cursor for LSN 1 is `bHNuOjE`; these are aliases that
    // a strict Go/Rust raw_url decoder would reject.
    for (const alias of ["bHNuOjF", "bHNuOjG", "bHNuOjH"]) {
      await expect(store.list({ cursor: alias })).rejects.toBeInstanceOf(InvalidListCursorError);
    }
  });

  it("InvalidListCursorError carries a constant message (no echoed input)", async () => {
    const store = new InMemoryReceiptStore();
    const hostile = "<script>alert('xss')</script>";
    try {
      await store.list({ cursor: hostile });
      throw new Error("expected InvalidListCursorError");
    } catch (err) {
      expect(err).toBeInstanceOf(InvalidListCursorError);
      // Security T6: the raw cursor MUST NOT appear in the error message
      // — it is hostile input and could land in log payloads.
      expect((err as Error).message).toBe("invalid_list_cursor");
      expect((err as Error).message).not.toContain(hostile);
    }
  });

  it("encodeListCursor rejects non-positive or unsafe LSNs", () => {
    expect(() => encodeListCursor(0)).toThrow(/positive safe integer/);
    expect(() => encodeListCursor(-1)).toThrow(/positive safe integer/);
    expect(() => encodeListCursor(1.5)).toThrow(/positive safe integer/);
    expect(() => encodeListCursor(Number.MAX_SAFE_INTEGER + 1)).toThrow(/positive safe integer/);
  });

  it("encodeListCursor round-trip skips items at or before the encoded LSN", async () => {
    const store = new InMemoryReceiptStore();
    await store.put(minimalReceiptV1("01ARZ3NDEKTSV4RRFFQ69G5FAV"));
    await store.put(minimalReceiptV1("01ARZ3NDEKTSV4RRFFQ69G5FAY"));
    await store.put(minimalReceiptV1("01ARZ3NDEKTSV4RRFFQ69G5FB1"));
    // LSN 1 means "skip the first item, return everything after".
    const after1 = await store.list({ cursor: encodeListCursor(1) });
    expect(after1.items.map((r) => r.id)).toEqual([
      "01ARZ3NDEKTSV4RRFFQ69G5FAY",
      "01ARZ3NDEKTSV4RRFFQ69G5FB1",
    ]);
    expect(after1.nextCursor).toBeNull();
  });

  it("size reflects byId count, not thread-index count", async () => {
    const store = new InMemoryReceiptStore();
    expect(store.size()).toBe(0);
    await store.put(minimalReceiptV1("01ARZ3NDEKTSV4RRFFQ69G5FAV"));
    await store.put(minimalReceiptV2("01ARZ3NDEKTSV4RRFFQ69G5FAY", THREAD_A));
    expect(store.size()).toBe(2);
  });

  it("throws ReceiptStoreFullError when maxReceipts is exceeded", async () => {
    const store = new InMemoryReceiptStore({ maxReceipts: 2 });
    await store.put(minimalReceiptV1("01ARZ3NDEKTSV4RRFFQ69G5FAV"));
    await store.put(minimalReceiptV1("01ARZ3NDEKTSV4RRFFQ69G5FAY"));
    await expect(store.put(minimalReceiptV1("01ARZ3NDEKTSV4RRFFQ69G5FB1"))).rejects.toBeInstanceOf(
      ReceiptStoreFullError,
    );
    expect(store.size()).toBe(2);
  });

  it("returns existed:true (not 507) when a duplicate hits at capacity", async () => {
    // Cap check runs AFTER the has-check: a duplicate at the cap is a
    // collision, not a capacity rejection. The correct status for a
    // bearer-holder retrying a previously-stored receipt is 409, not 507.
    const store = new InMemoryReceiptStore({ maxReceipts: 1 });
    const r = minimalReceiptV1("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    await store.put(r);
    expect(await store.put(r)).toEqual({ existed: true });
  });

  it("rejects non-positive or non-integer maxReceipts at construction", () => {
    expect(() => new InMemoryReceiptStore({ maxReceipts: 0 })).toThrow(/positive integer/);
    expect(() => new InMemoryReceiptStore({ maxReceipts: -1 })).toThrow(/positive integer/);
    expect(() => new InMemoryReceiptStore({ maxReceipts: 1.5 })).toThrow(/positive integer/);
  });
});
