import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import React from "react";
import { render, cleanup } from "ink-testing-library";
import {
  SlashAutocomplete,
  filterCommands,
  computeAutocompleteState,
  createAutocompleteState,
  createAutocompleteActions,
} from "../../../src/tui/components/slash-autocomplete.js";
import type {
  SlashCommandEntry,
  AutocompleteState,
} from "../../../src/tui/components/slash-autocomplete.js";

// Strip ANSI escape sequences for assertion matching
function strip(s: string): string {
  return s.replace(/\x1b\[[0-9;]*m/g, "");
}

afterEach(() => {
  cleanup();
});

// ── Test data ───────────────────────────────────────────────────────

const COMMANDS: SlashCommandEntry[] = [
  { name: "help", description: "Show all commands" },
  { name: "search", description: "Search records" },
  { name: "ask", description: "Query context graph" },
  { name: "agents", description: "Open agent list" },
  { name: "agent", description: "Manage agents" },
  { name: "calendar", description: "Open calendar" },
  { name: "chat", description: "Open chat view" },
  { name: "clear", description: "Clear history" },
  { name: "objects", description: "List object types" },
  { name: "records", description: "List records" },
  { name: "remember", description: "Store content" },
  { name: "quit", description: "Exit" },
];

// ── filterCommands ──────────────────────────────────────────────────

describe("filterCommands", () => {
  it("returns all commands when query is empty", () => {
    const result = filterCommands("", COMMANDS);
    assert.equal(result.length, COMMANDS.length);
  });

  it("filters by prefix", () => {
    const result = filterCommands("a", COMMANDS);
    assert.equal(result.length, 3); // ask, agents, agent
    assert.ok(result.every((r) => r.name.startsWith("a")));
  });

  it("is case insensitive", () => {
    const result = filterCommands("A", COMMANDS);
    assert.equal(result.length, 3);
  });

  it("returns empty array when no match", () => {
    const result = filterCommands("xyz", COMMANDS);
    assert.equal(result.length, 0);
  });

  it("matches exact prefix", () => {
    const result = filterCommands("he", COMMANDS);
    assert.equal(result.length, 1);
    assert.equal(result[0].name, "help");
  });

  it("matches multiple commands with same prefix", () => {
    const result = filterCommands("c", COMMANDS);
    // calendar, chat, clear
    assert.equal(result.length, 3);
  });
});

// ── computeAutocompleteState ────────────────────────────────────────

describe("computeAutocompleteState", () => {
  it("returns not visible for empty input", () => {
    const state = computeAutocompleteState("", COMMANDS);
    assert.equal(state.visible, false);
  });

  it("returns not visible for non-slash input", () => {
    const state = computeAutocompleteState("hello", COMMANDS);
    assert.equal(state.visible, false);
  });

  it("returns visible with all commands for bare /", () => {
    const state = computeAutocompleteState("/", COMMANDS);
    assert.equal(state.visible, true);
    assert.equal(state.matches.length, COMMANDS.length);
    assert.equal(state.query, "");
  });

  it("returns visible with filtered commands for /a", () => {
    const state = computeAutocompleteState("/a", COMMANDS);
    assert.equal(state.visible, true);
    assert.equal(state.matches.length, 3); // ask, agents, agent
    assert.equal(state.query, "a");
  });

  it("returns not visible when input has a space (args started)", () => {
    const state = computeAutocompleteState("/search foo", COMMANDS);
    assert.equal(state.visible, false);
  });

  it("returns not visible when exact match only", () => {
    const state = computeAutocompleteState("/help", COMMANDS);
    assert.equal(state.visible, false); // exact match = dismiss
  });

  it("returns not visible for no matches", () => {
    const state = computeAutocompleteState("/zzz", COMMANDS);
    assert.equal(state.visible, false);
  });

  it("starts with selectedIndex 0", () => {
    const state = computeAutocompleteState("/a", COMMANDS);
    assert.equal(state.selectedIndex, 0);
  });
});

// ── createAutocompleteActions ───────────────────────────────────────

describe("createAutocompleteActions", () => {
  function makeState(query: string): AutocompleteState {
    return computeAutocompleteState(`/${query}`, COMMANDS);
  }

  it("onTab cycles through matches", () => {
    let state = makeState("a"); // 3 matches: ask, agents, agent
    let lastState = state;
    const setState = (s: AutocompleteState) => { lastState = s; };

    const actions = createAutocompleteActions(state, setState, COMMANDS);
    const result = actions.onTab();
    assert.equal(result, null); // no accept, just cycle
    assert.equal(lastState.selectedIndex, 1);
  });

  it("onTab wraps around", () => {
    const state: AutocompleteState = {
      visible: true,
      matches: COMMANDS.slice(0, 3),
      selectedIndex: 2,
      query: "",
    };
    let lastState = state;
    const setState = (s: AutocompleteState) => { lastState = s; };

    const actions = createAutocompleteActions(state, setState, COMMANDS);
    actions.onTab();
    assert.equal(lastState.selectedIndex, 0); // wrapped
  });

  it("onTab accepts immediately when only one match", () => {
    const state: AutocompleteState = {
      visible: true,
      matches: [{ name: "help", description: "Show help" }],
      selectedIndex: 0,
      query: "he",
    };
    let lastState = state;
    const setState = (s: AutocompleteState) => { lastState = s; };

    const actions = createAutocompleteActions(state, setState, COMMANDS);
    const result = actions.onTab();
    assert.ok(result, "should return result for single match");
    assert.equal(result!.text, "/help ");
    assert.equal(result!.command, "help");
    assert.equal(lastState.visible, false); // dismissed
  });

  it("onShiftTab cycles backwards", () => {
    const state: AutocompleteState = {
      visible: true,
      matches: COMMANDS.slice(0, 3),
      selectedIndex: 0,
      query: "",
    };
    let lastState = state;
    const setState = (s: AutocompleteState) => { lastState = s; };

    const actions = createAutocompleteActions(state, setState, COMMANDS);
    actions.onShiftTab();
    assert.equal(lastState.selectedIndex, 2); // wrapped to end
  });

  it("onAccept returns selected command with trailing space", () => {
    const state: AutocompleteState = {
      visible: true,
      matches: [
        { name: "agents", description: "Open agent list" },
        { name: "agent", description: "Manage agents" },
      ],
      selectedIndex: 1,
      query: "ag",
    };
    let lastState = state;
    const setState = (s: AutocompleteState) => { lastState = s; };

    const actions = createAutocompleteActions(state, setState, COMMANDS);
    const result = actions.onAccept();
    assert.ok(result);
    assert.equal(result!.text, "/agent ");
    assert.equal(result!.command, "agent");
    assert.equal(lastState.visible, false);
  });

  it("onAccept returns null when not visible", () => {
    const state = createAutocompleteState();
    const setState = () => {};

    const actions = createAutocompleteActions(state, setState, COMMANDS);
    const result = actions.onAccept();
    assert.equal(result, null);
  });

  it("onDismiss hides the overlay", () => {
    const state: AutocompleteState = {
      visible: true,
      matches: COMMANDS.slice(0, 3),
      selectedIndex: 1,
      query: "a",
    };
    let lastState = state;
    const setState = (s: AutocompleteState) => { lastState = s; };

    const actions = createAutocompleteActions(state, setState, COMMANDS);
    actions.onDismiss();
    assert.equal(lastState.visible, false);
    assert.equal(lastState.matches.length, 0);
  });

  it("onTab returns null when not visible", () => {
    const state = createAutocompleteState();
    const setState = () => {};

    const actions = createAutocompleteActions(state, setState, COMMANDS);
    assert.equal(actions.onTab(), null);
  });
});

// ── SlashAutocomplete component ─────────────────────────────────────

describe("SlashAutocomplete", () => {
  it("renders nothing when not visible", () => {
    const state = createAutocompleteState();
    const { lastFrame } = render(<SlashAutocomplete state={state} />);
    const frame = lastFrame() ?? "";
    assert.equal(frame.trim(), "");
  });

  it("renders matching commands with / prefix", () => {
    const state: AutocompleteState = {
      visible: true,
      matches: [
        { name: "agents", description: "Open agent list" },
        { name: "agent", description: "Manage agents" },
        { name: "ask", description: "Query context graph" },
      ],
      selectedIndex: 0,
      query: "a",
    };
    const { lastFrame } = render(<SlashAutocomplete state={state} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("/agents"), "should show /agents");
    assert.ok(frame.includes("/agent"), "should show /agent");
    assert.ok(frame.includes("/ask"), "should show /ask");
    assert.ok(frame.includes("Open agent list"), "should show description");
  });

  it("highlights the selected item with >", () => {
    const state: AutocompleteState = {
      visible: true,
      matches: [
        { name: "agents", description: "Open agent list" },
        { name: "ask", description: "Query context graph" },
      ],
      selectedIndex: 1,
      query: "a",
    };
    const { lastFrame } = render(<SlashAutocomplete state={state} />);
    const frame = strip(lastFrame() ?? "");
    const lines = frame.split("\n");
    const askLine = lines.find((l) => l.includes("/ask"));
    assert.ok(askLine, "should find /ask line");
    assert.ok(askLine.includes(">"), "selected line should have > indicator");
  });

  it("respects maxVisible", () => {
    const matches: SlashCommandEntry[] = Array.from({ length: 10 }, (_, i) => ({
      name: `cmd${i}`,
      description: `Command ${i}`,
    }));
    const state: AutocompleteState = {
      visible: true,
      matches,
      selectedIndex: 0,
      query: "",
    };
    const { lastFrame } = render(
      <SlashAutocomplete state={state} maxVisible={3} />,
    );
    const frame = strip(lastFrame() ?? "");
    // Should show scroll indicator
    assert.ok(frame.includes("1/10"), "should show scroll position");
    // Should show only 3 command items
    const cmdLines = frame.split("\n").filter((l) => l.includes("/cmd"));
    assert.ok(cmdLines.length <= 3, `should show at most 3 items, got ${cmdLines.length}`);
  });

  it("shows count indicator when items overflow", () => {
    const matches: SlashCommandEntry[] = Array.from({ length: 15 }, (_, i) => ({
      name: `cmd${i}`,
      description: `Command ${i}`,
    }));
    const state: AutocompleteState = {
      visible: true,
      matches,
      selectedIndex: 5,
      query: "",
    };
    const { lastFrame } = render(
      <SlashAutocomplete state={state} maxVisible={5} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("6/15"), "should show current/total");
  });
});
