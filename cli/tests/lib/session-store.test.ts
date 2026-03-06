import { describe, it, beforeEach, afterEach } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { SessionStore } from "../../src/lib/session-store.js";

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

  it("get returns undefined for missing key", () => {
    assert.equal(store.get("nonexistent"), undefined);
  });

  it("set and get round-trip", () => {
    store.set("key1", "value1");
    assert.equal(store.get("key1"), "value1");
  });

  it("delete removes a key and returns true", () => {
    store.set("key1", "value1");
    assert.equal(store.delete("key1"), true);
    assert.equal(store.get("key1"), undefined);
  });

  it("delete returns false for missing key", () => {
    assert.equal(store.delete("nonexistent"), false);
  });

  it("list returns all entries", () => {
    store.set("a", "1");
    store.set("b", "2");
    const all = store.list();
    assert.equal(all.a, "1");
    assert.equal(all.b, "2");
  });

  it("clear removes all entries", () => {
    store.set("a", "1");
    store.set("b", "2");
    store.clear();
    const all = store.list();
    assert.deepEqual(all, {});
  });

  it("evicts oldest entries when max size exceeded", () => {
    const small = new SessionStore({ dataDir: tmpDir, maxSize: 2 });
    small.set("first", "1");
    small.set("second", "2");
    small.set("third", "3");
    const all = small.list();
    assert.equal(all.first, undefined); // evicted
    assert.equal(all.second, "2");
    assert.equal(all.third, "3");
  });
});
