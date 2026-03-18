import { describe, it, beforeEach, afterEach, mock } from "node:test";
import assert from "node:assert/strict";
import { handleKey, parseKey } from "../../src/tui/keybindings.js";
import type { TuiState, Action } from "../../src/tui/store.js";

// ── Helpers ────────────────────────────────────────────────────────

function makeState(overrides: Partial<TuiState> = {}): TuiState {
  return {
    mode: "normal",
    viewStack: [{ name: "home" }],
    nav: {},
    pickerItems: null,
    pickerCursor: 0,
    content: "",
    loading: false,
    loadingHint: "",
    inputValue: "",
    inputHistory: [],
    historyIndex: -1,
    scrollOffset: 0,
    lastKey: "",
    lastKeyTime: 0,
    ...overrides,
  };
}

function collectDispatches(
  rawInput: string,
  state: TuiState,
): Action[] {
  const actions: Action[] = [];
  handleKey(rawInput, state, (action) => actions.push(action));
  return actions;
}

// ── parseKey tests ─────────────────────────────────────────────────

describe("parseKey", () => {
  it("parses printable lowercase", () => {
    const k = parseKey("a");
    assert.equal(k.name, "a");
    assert.equal(k.ctrl, false);
    assert.equal(k.shift, false);
  });

  it("parses uppercase as shift", () => {
    const k = parseKey("G");
    assert.equal(k.name, "G");
    assert.equal(k.shift, true);
  });

  it("parses Ctrl+d", () => {
    const k = parseKey("\x04"); // Ctrl+D = 0x04
    assert.equal(k.name, "d");
    assert.equal(k.ctrl, true);
  });

  it("parses Ctrl+u", () => {
    const k = parseKey("\x15"); // Ctrl+U = 0x15
    assert.equal(k.name, "u");
    assert.equal(k.ctrl, true);
  });

  it("parses escape", () => {
    const k = parseKey("\x1b");
    assert.equal(k.name, "escape");
  });

  it("parses return", () => {
    const k = parseKey("\r");
    assert.equal(k.name, "return");
  });

  it("parses tab", () => {
    const k = parseKey("\t");
    assert.equal(k.name, "tab");
  });

  it("parses up arrow", () => {
    const k = parseKey("\x1b[A");
    assert.equal(k.name, "up");
  });

  it("parses down arrow", () => {
    const k = parseKey("\x1b[B");
    assert.equal(k.name, "down");
  });
});

// ── Normal mode tests ──────────────────────────────────────────────

describe("keybindings: normal mode", () => {
  it("i switches to insert mode", () => {
    const actions = collectDispatches("i", makeState());
    assert.equal(actions.length, 1);
    assert.deepEqual(actions[0], { type: "SET_MODE", mode: "insert" });
  });

  it("/ switches to insert mode and sets input", () => {
    const actions = collectDispatches("/", makeState());
    assert.equal(actions.length, 2);
    assert.deepEqual(actions[0], { type: "SET_MODE", mode: "insert" });
    assert.deepEqual(actions[1], { type: "SET_INPUT", value: "/" });
  });

  it("? pushes help view", () => {
    const actions = collectDispatches("?", makeState());
    assert.equal(actions.length, 1);
    assert.equal(actions[0].type, "PUSH_VIEW");
    if (actions[0].type === "PUSH_VIEW") {
      assert.equal(actions[0].view.name, "help");
    }
  });

  it("j scrolls down when no picker", () => {
    const actions = collectDispatches("j", makeState({ scrollOffset: 5 }));
    assert.equal(actions.length, 1);
    assert.deepEqual(actions[0], { type: "SCROLL", offset: 6 });
  });

  it("k scrolls up when no picker", () => {
    const actions = collectDispatches("k", makeState({ scrollOffset: 5 }));
    assert.equal(actions.length, 1);
    assert.deepEqual(actions[0], { type: "SCROLL", offset: 4 });
  });

  it("j moves picker cursor down", () => {
    const items = [
      { command: "a", label: "A", detail: "a" },
      { command: "b", label: "B", detail: "b" },
    ];
    const actions = collectDispatches(
      "j",
      makeState({ pickerItems: items, pickerCursor: 0 }),
    );
    assert.equal(actions.length, 1);
    assert.deepEqual(actions[0], { type: "SET_PICKER_CURSOR", cursor: 1 });
  });

  it("k moves picker cursor up", () => {
    const items = [
      { command: "a", label: "A", detail: "a" },
      { command: "b", label: "B", detail: "b" },
    ];
    const actions = collectDispatches(
      "k",
      makeState({ pickerItems: items, pickerCursor: 1 }),
    );
    assert.equal(actions.length, 1);
    assert.deepEqual(actions[0], { type: "SET_PICKER_CURSOR", cursor: 0 });
  });

  it("G scrolls to bottom (Infinity)", () => {
    const actions = collectDispatches("G", makeState());
    assert.equal(actions.length, 1);
    assert.deepEqual(actions[0], { type: "SCROLL", offset: Infinity });
  });

  it("gg (double tap) scrolls to top", () => {
    const now = Date.now();
    const state = makeState({ lastKey: "g", lastKeyTime: now });
    const actions = collectDispatches("g", state);
    // Should produce SCROLL to 0 and clear lastKey
    const scrollAction = actions.find(
      (a) => a.type === "SCROLL" && "offset" in a && a.offset === 0,
    );
    assert.ok(scrollAction, "Expected SCROLL to offset 0");
  });

  it("single g just records lastKey", () => {
    const actions = collectDispatches("g", makeState());
    assert.equal(actions.length, 1);
    assert.equal(actions[0].type, "SET_LAST_KEY");
    if (actions[0].type === "SET_LAST_KEY") {
      assert.equal(actions[0].key, "g");
    }
  });

  it("Escape pops view", () => {
    const actions = collectDispatches("\x1b", makeState());
    assert.equal(actions.length, 1);
    assert.deepEqual(actions[0], { type: "POP_VIEW" });
  });

  it("Ctrl+d scrolls half page down", () => {
    const actions = collectDispatches("\x04", makeState({ scrollOffset: 0 }));
    assert.equal(actions.length, 1);
    assert.deepEqual(actions[0], { type: "SCROLL", offset: 15 });
  });

  it("Ctrl+u scrolls half page up", () => {
    const actions = collectDispatches("\x15", makeState({ scrollOffset: 20 }));
    assert.equal(actions.length, 1);
    assert.deepEqual(actions[0], { type: "SCROLL", offset: 5 });
  });

  it("number keys quick-select picker item", () => {
    const items = [
      { command: "cmd1", label: "1", detail: "d1" },
      { command: "cmd2", label: "2", detail: "d2" },
      { command: "cmd3", label: "3", detail: "d3" },
    ];
    const actions = collectDispatches(
      "2",
      makeState({ pickerItems: items }),
    );
    // Should set cursor to 1 and switch to insert with cmd2
    const cursorAction = actions.find(
      (a) => a.type === "SET_PICKER_CURSOR",
    );
    assert.ok(cursorAction);
    if (cursorAction && cursorAction.type === "SET_PICKER_CURSOR") {
      assert.equal(cursorAction.cursor, 1);
    }
    const modeAction = actions.find((a) => a.type === "SET_MODE");
    assert.ok(modeAction);
  });

  it("q calls process.exit", () => {
    const originalExit = process.exit;
    let exitCalled = false;
    process.exit = ((_code?: number) => {
      exitCalled = true;
    }) as typeof process.exit;

    try {
      collectDispatches("q", makeState());
      assert.ok(exitCalled, "process.exit should have been called");
    } finally {
      process.exit = originalExit;
    }
  });
});

// ── Insert mode tests ──────────────────────────────────────────────

describe("keybindings: insert mode", () => {
  it("Escape switches to normal mode", () => {
    const actions = collectDispatches("\x1b", makeState({ mode: "insert" }));
    assert.equal(actions.length, 1);
    assert.deepEqual(actions[0], { type: "SET_MODE", mode: "normal" });
  });

  it("Enter pushes history when inputValue is non-empty", () => {
    const actions = collectDispatches(
      "\r",
      makeState({ mode: "insert", inputValue: "record list" }),
    );
    assert.equal(actions.length, 1);
    assert.deepEqual(actions[0], {
      type: "PUSH_HISTORY",
      command: "record list",
    });
  });

  it("Enter does not push empty input to history", () => {
    const actions = collectDispatches(
      "\r",
      makeState({ mode: "insert", inputValue: "" }),
    );
    assert.equal(actions.length, 0);
  });

  it("Up arrow navigates history", () => {
    const history = ["first", "second", "third"];
    const actions = collectDispatches(
      "\x1b[A",
      makeState({ mode: "insert", inputHistory: history }),
    );
    // Should set historyIndex to last and set input
    const idxAction = actions.find((a) => a.type === "SET_HISTORY_INDEX");
    assert.ok(idxAction);
    if (idxAction && idxAction.type === "SET_HISTORY_INDEX") {
      assert.equal(idxAction.index, 2); // last item
    }
  });

  it("Down arrow navigates history forward", () => {
    const history = ["first", "second"];
    const actions = collectDispatches(
      "\x1b[B",
      makeState({ mode: "insert", inputHistory: history, historyIndex: 0 }),
    );
    const idxAction = actions.find((a) => a.type === "SET_HISTORY_INDEX");
    assert.ok(idxAction);
    if (idxAction && idxAction.type === "SET_HISTORY_INDEX") {
      assert.equal(idxAction.index, 1);
    }
  });

  it("Tab autocompletes matching command", () => {
    const actions = collectDispatches(
      "\t",
      makeState({ mode: "insert", inputValue: "record l" }),
    );
    const inputAction = actions.find((a) => a.type === "SET_INPUT");
    assert.ok(inputAction);
    if (inputAction && inputAction.type === "SET_INPUT") {
      assert.equal(inputAction.value, "record list");
    }
  });

  it("Tab does nothing when no match", () => {
    const actions = collectDispatches(
      "\t",
      makeState({ mode: "insert", inputValue: "zzzz" }),
    );
    const inputAction = actions.find((a) => a.type === "SET_INPUT");
    assert.equal(inputAction, undefined);
  });

  it("regular keys in insert mode produce no dispatch", () => {
    const actions = collectDispatches(
      "a",
      makeState({ mode: "insert" }),
    );
    assert.equal(actions.length, 0);
  });
});
