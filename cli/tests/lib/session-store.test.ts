import { describe, test, expect, beforeEach, afterEach } from "bun:test";
import { mkdtempSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { SessionStore } from "../../src/lib/session-store.ts";

describe("SessionStore", () => {
  let tmpDir: string;
  let store: SessionStore;

  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), "nex-session-test-"));
    store = new SessionStore({ dataDir: tmpDir });
  });

  afterEach(() => {
    rmSync(tmpDir, { recursive: true, force: true });
  });

  test("get returns undefined for missing key", () => {
    expect(store.get("nonexistent")).toBe(undefined);
  });

  test("set and get round-trip", () => {
    store.set("key1", "value1");
    expect(store.get("key1")).toBe("value1");
  });

  test("delete removes a key and returns true", () => {
    store.set("key1", "value1");
    expect(store.delete("key1")).toBe(true);
    expect(store.get("key1")).toBe(undefined);
  });

  test("delete returns false for missing key", () => {
    expect(store.delete("nonexistent")).toBe(false);
  });

  test("list returns all entries", () => {
    store.set("a", "1");
    store.set("b", "2");
    const all = store.list();
    expect(all.a).toBe("1");
    expect(all.b).toBe("2");
  });

  test("clear removes all entries", () => {
    store.set("a", "1");
    store.set("b", "2");
    store.clear();
    const all = store.list();
    expect(all).toEqual({});
  });

  test("evicts oldest entries when max size exceeded", () => {
    const small = new SessionStore({ dataDir: tmpDir, maxSize: 2 });
    small.set("first", "1");
    small.set("second", "2");
    small.set("third", "3");
    const all = small.list();
    expect(all.first).toBe(undefined); // evicted
    expect(all.second).toBe("2");
    expect(all.third).toBe("3");
  });
});
