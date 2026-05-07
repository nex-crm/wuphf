/**
 * Tests for pushRecentObject / readRecentObjects.
 *
 * Phase 5 PR 2 — app navigation refresh.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  RECENT_OBJECTS_MAX,
  pushRecentObject,
  readRecentObjects,
} from "./useRecentObjects";

// Fake localStorage using a plain Map.
let store: Map<string, string>;
beforeEach(() => {
  store = new Map();
  vi.stubGlobal("localStorage", {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => { store.set(k, v); },
    removeItem: (k: string) => { store.delete(k); },
    clear: () => { store.clear(); },
  });
});
afterEach(() => {
  vi.unstubAllGlobals();
});

describe("pushRecentObject", () => {
  it("adds an entry and returns the updated list", () => {
    const list = pushRecentObject({ kind: "agent", slug: "gaia" });
    expect(list).toHaveLength(1);
    expect(list[0].ref.kind).toBe("agent");
    expect(list[0].label).toContain("gaia");
  });

  it("deduplicates by object key (newest wins)", () => {
    pushRecentObject({ kind: "agent", slug: "gaia" });
    const list = pushRecentObject({ kind: "agent", slug: "gaia" });
    expect(list).toHaveLength(1);
  });

  it("prepends new entries so the most recent is first", () => {
    pushRecentObject({ kind: "agent", slug: "gaia" });
    const list = pushRecentObject({ kind: "task", id: "t-1" });
    expect(list[0].ref.kind).toBe("task");
    expect(list[1].ref.kind).toBe("agent");
  });

  it("caps at RECENT_OBJECTS_MAX entries", () => {
    for (let i = 0; i < RECENT_OBJECTS_MAX + 5; i++) {
      pushRecentObject({ kind: "task", id: `task-${i}` });
    }
    const list = readRecentObjects();
    expect(list).toHaveLength(RECENT_OBJECTS_MAX);
  });

  it("does not record fallback (missing-id) objects", () => {
    // pushRecentObject with a missing field resolves to a fallback and skips.
    // Cast through unknown to test the runtime guard without TS complaining.
    const before = readRecentObjects().length;
    pushRecentObject({ kind: "agent", slug: "" } as unknown as Parameters<typeof pushRecentObject>[0]);
    expect(readRecentObjects().length).toBe(before);
  });
});

describe("readRecentObjects", () => {
  it("returns empty array on cold start", () => {
    expect(readRecentObjects()).toEqual([]);
  });

  it("persists across reads", () => {
    pushRecentObject({ kind: "wiki-page", path: "people/nazz" });
    const list = readRecentObjects();
    expect(list).toHaveLength(1);
    expect(list[0].ref.kind).toBe("wiki-page");
  });

  it("survives corrupted localStorage gracefully", () => {
    store.set("wuphf-recent-objects", "NOT_JSON{{{{");
    expect(readRecentObjects()).toEqual([]);
  });

  it("drops parseable entries with invalid shape", () => {
    store.set(
      "wuphf-recent-objects",
      JSON.stringify([
        {
          ref: { kind: "agent", slug: "gaia" },
          href: 123,
          label: null,
          visitedAtMs: Date.now(),
        },
      ]),
    );
    expect(readRecentObjects()).toEqual([]);
  });
});
