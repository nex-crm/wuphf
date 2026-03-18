import { describe, it, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { createStore } from "../../src/tui/store.js";
import type { Store } from "../../src/tui/store.js";

describe("TUI Store", () => {
  let store: Store;

  beforeEach(() => {
    store = createStore();
  });

  // ── Initial state ──────────────────────────────────────────────

  it("initial mode is normal", () => {
    assert.equal(store.getState().mode, "normal");
  });

  it("initial viewStack contains home", () => {
    const stack = store.getState().viewStack;
    assert.equal(stack.length, 1);
    assert.equal(stack[0].name, "home");
  });

  it("initial pickerItems is null", () => {
    assert.equal(store.getState().pickerItems, null);
  });

  it("initial inputValue is empty", () => {
    assert.equal(store.getState().inputValue, "");
  });

  it("initial scrollOffset is 0", () => {
    assert.equal(store.getState().scrollOffset, 0);
  });

  // ── PUSH_VIEW / POP_VIEW ──────────────────────────────────────

  it("PUSH_VIEW adds to viewStack", () => {
    store.dispatch({ type: "PUSH_VIEW", view: { name: "help" } });
    const stack = store.getState().viewStack;
    assert.equal(stack.length, 2);
    assert.equal(stack[1].name, "help");
  });

  it("PUSH_VIEW preserves props", () => {
    store.dispatch({
      type: "PUSH_VIEW",
      view: { name: "record-detail", props: { id: "abc" } },
    });
    const top = store.getState().viewStack[1];
    assert.deepEqual(top.props, { id: "abc" });
  });

  it("POP_VIEW removes top entry", () => {
    store.dispatch({ type: "PUSH_VIEW", view: { name: "help" } });
    store.dispatch({ type: "PUSH_VIEW", view: { name: "record-list" } });
    assert.equal(store.getState().viewStack.length, 3);

    store.dispatch({ type: "POP_VIEW" });
    const stack = store.getState().viewStack;
    assert.equal(stack.length, 2);
    assert.equal(stack[stack.length - 1].name, "help");
  });

  it("POP_VIEW never pops below home", () => {
    store.dispatch({ type: "POP_VIEW" });
    store.dispatch({ type: "POP_VIEW" });
    assert.equal(store.getState().viewStack.length, 1);
    assert.equal(store.getState().viewStack[0].name, "home");
  });

  it("PUSH_VIEW resets scrollOffset", () => {
    store.dispatch({ type: "SCROLL", offset: 42 });
    assert.equal(store.getState().scrollOffset, 42);

    store.dispatch({ type: "PUSH_VIEW", view: { name: "help" } });
    assert.equal(store.getState().scrollOffset, 0);
  });

  it("viewStack max depth is 20", () => {
    for (let i = 0; i < 25; i++) {
      store.dispatch({ type: "PUSH_VIEW", view: { name: `view-${i}` } });
    }
    const stack = store.getState().viewStack;
    assert.ok(stack.length <= 20, `Stack length ${stack.length} exceeds 20`);
    // Home should still be at index 0
    assert.equal(stack[0].name, "home");
    // Last pushed view should be at the end
    assert.equal(stack[stack.length - 1].name, "view-24");
  });

  // ── SET_MODE ──────────────────────────────────────────────────

  it("SET_MODE toggles to insert", () => {
    store.dispatch({ type: "SET_MODE", mode: "insert" });
    assert.equal(store.getState().mode, "insert");
  });

  it("SET_MODE toggles back to normal", () => {
    store.dispatch({ type: "SET_MODE", mode: "insert" });
    store.dispatch({ type: "SET_MODE", mode: "normal" });
    assert.equal(store.getState().mode, "normal");
  });

  // ── SET_CONTENT ───────────────────────────────────────────────

  it("SET_CONTENT updates content", () => {
    store.dispatch({ type: "SET_CONTENT", content: "hello world" });
    assert.equal(store.getState().content, "hello world");
  });

  // ── SET_LOADING ───────────────────────────────────────────────

  it("SET_LOADING sets loading and hint", () => {
    store.dispatch({ type: "SET_LOADING", loading: true, hint: "fetching..." });
    assert.equal(store.getState().loading, true);
    assert.equal(store.getState().loadingHint, "fetching...");
  });

  it("SET_LOADING clears hint when not provided", () => {
    store.dispatch({ type: "SET_LOADING", loading: true, hint: "x" });
    store.dispatch({ type: "SET_LOADING", loading: false });
    assert.equal(store.getState().loading, false);
    assert.equal(store.getState().loadingHint, "");
  });

  // ── SET_INPUT ─────────────────────────────────────────────────

  it("SET_INPUT updates inputValue", () => {
    store.dispatch({ type: "SET_INPUT", value: "record list" });
    assert.equal(store.getState().inputValue, "record list");
  });

  // ── SET_PICKER ────────────────────────────────────────────────

  it("SET_PICKER sets items and resets cursor", () => {
    const items = [
      { command: "cmd1", label: "Label 1", detail: "Detail 1" },
      { command: "cmd2", label: "Label 2", detail: "Detail 2" },
    ];
    store.dispatch({ type: "SET_PICKER", items });
    assert.deepEqual(store.getState().pickerItems, items);
    assert.equal(store.getState().pickerCursor, 0);
  });

  it("SET_PICKER with cursor overrides default", () => {
    const items = [
      { command: "a", label: "A", detail: "a" },
      { command: "b", label: "B", detail: "b" },
    ];
    store.dispatch({ type: "SET_PICKER", items, cursor: 1 });
    assert.equal(store.getState().pickerCursor, 1);
  });

  it("SET_PICKER clears items", () => {
    store.dispatch({
      type: "SET_PICKER",
      items: [{ command: "x", label: "X", detail: "x" }],
    });
    store.dispatch({ type: "SET_PICKER", items: null });
    assert.equal(store.getState().pickerItems, null);
  });

  // ── NAVIGATE ──────────────────────────────────────────────────

  it("NAVIGATE merges nav state", () => {
    store.dispatch({ type: "NAVIGATE", nav: { objectSlug: "person" } });
    assert.equal(store.getState().nav.objectSlug, "person");

    store.dispatch({ type: "NAVIGATE", nav: { recordId: "123" } });
    assert.equal(store.getState().nav.objectSlug, "person");
    assert.equal(store.getState().nav.recordId, "123");
  });

  // ── SCROLL ────────────────────────────────────────────────────

  it("SCROLL updates scrollOffset", () => {
    store.dispatch({ type: "SCROLL", offset: 10 });
    assert.equal(store.getState().scrollOffset, 10);
  });

  it("SCROLL clamps to 0", () => {
    store.dispatch({ type: "SCROLL", offset: -5 });
    assert.equal(store.getState().scrollOffset, 0);
  });

  // ── PUSH_HISTORY ──────────────────────────────────────────────

  it("PUSH_HISTORY adds to inputHistory", () => {
    store.dispatch({ type: "PUSH_HISTORY", command: "record list" });
    assert.deepEqual(store.getState().inputHistory, ["record list"]);
  });

  it("PUSH_HISTORY resets historyIndex", () => {
    store.dispatch({ type: "SET_HISTORY_INDEX", index: 2 });
    store.dispatch({ type: "PUSH_HISTORY", command: "search foo" });
    assert.equal(store.getState().historyIndex, -1);
  });

  // ── SET_PICKER_CURSOR ─────────────────────────────────────────

  it("SET_PICKER_CURSOR updates cursor", () => {
    store.dispatch({ type: "SET_PICKER_CURSOR", cursor: 5 });
    assert.equal(store.getState().pickerCursor, 5);
  });

  // ── subscribe / unsubscribe ───────────────────────────────────

  it("subscribe fires on dispatch", () => {
    let callCount = 0;
    store.subscribe(() => {
      callCount++;
    });

    store.dispatch({ type: "SET_MODE", mode: "insert" });
    assert.equal(callCount, 1);

    store.dispatch({ type: "SET_MODE", mode: "normal" });
    assert.equal(callCount, 2);
  });

  it("unsubscribe stops notifications", () => {
    let callCount = 0;
    const unsub = store.subscribe(() => {
      callCount++;
    });

    store.dispatch({ type: "SET_MODE", mode: "insert" });
    assert.equal(callCount, 1);

    unsub();
    store.dispatch({ type: "SET_MODE", mode: "normal" });
    assert.equal(callCount, 1); // no increase
  });

  it("setState merges and notifies", () => {
    let called = false;
    store.subscribe(() => {
      called = true;
    });

    store.setState({ content: "direct" });
    assert.equal(store.getState().content, "direct");
    assert.ok(called);
  });

  // ── SET_LAST_KEY ──────────────────────────────────────────────

  it("SET_LAST_KEY records key and time", () => {
    store.dispatch({ type: "SET_LAST_KEY", key: "g", time: 12345 });
    assert.equal(store.getState().lastKey, "g");
    assert.equal(store.getState().lastKeyTime, 12345);
  });
});
