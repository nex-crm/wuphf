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

import { InMemoryReceiptStore } from "../src/receipt-store.ts";

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

  it("list() returns receipts in insertion order", async () => {
    const store = new InMemoryReceiptStore();
    const a = minimalReceiptV1("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    const b = minimalReceiptV1("01ARZ3NDEKTSV4RRFFQ69G5FAY");
    const c = minimalReceiptV1("01ARZ3NDEKTSV4RRFFQ69G5FB1");
    await store.put(a);
    await store.put(b);
    await store.put(c);
    const all = await store.list();
    expect(all.map((r) => r.id)).toEqual([a.id, b.id, c.id]);
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
    expect(inA.map((r) => r.id).sort()).toEqual([a.id, c.id].sort());
  });

  it("list({threadId}) excludes v1 receipts (no threadId)", async () => {
    const store = new InMemoryReceiptStore();
    await store.put(minimalReceiptV1("01ARZ3NDEKTSV4RRFFQ69G5FAV"));
    await store.put(minimalReceiptV2("01ARZ3NDEKTSV4RRFFQ69G5FAY", THREAD_A));
    const inA = await store.list({ threadId: asThreadId(THREAD_A) });
    expect(inA.map((r) => r.id)).toEqual(["01ARZ3NDEKTSV4RRFFQ69G5FAY"]);
  });

  it("list({threadId}) returns [] for unknown thread", async () => {
    const store = new InMemoryReceiptStore();
    await store.put(minimalReceiptV2("01ARZ3NDEKTSV4RRFFQ69G5FAV", THREAD_A));
    const inB = await store.list({ threadId: asThreadId(THREAD_B) });
    expect(inB).toEqual([]);
  });

  it("size reflects byId count, not thread-index count", async () => {
    const store = new InMemoryReceiptStore();
    expect(store.size()).toBe(0);
    await store.put(minimalReceiptV1("01ARZ3NDEKTSV4RRFFQ69G5FAV"));
    await store.put(minimalReceiptV2("01ARZ3NDEKTSV4RRFFQ69G5FAY", THREAD_A));
    expect(store.size()).toBe(2);
  });
});
