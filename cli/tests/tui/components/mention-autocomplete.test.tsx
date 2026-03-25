import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import React from "react";
import { render, cleanup } from "ink-testing-library";
import {
  MentionAutocomplete,
  filterAgents,
  computeMentionState,
  createMentionState,
  createMentionActions,
} from "../../../src/tui/components/mention-autocomplete.js";
import type {
  AgentEntry,
  MentionState,
} from "../../../src/tui/components/mention-autocomplete.js";

function strip(s: string): string {
  return s.replace(/\x1b\[[0-9;]*m/g, "");
}

afterEach(() => {
  cleanup();
});

// ── Test data ───────────────────────────────────────────────────────

const AGENTS: AgentEntry[] = [
  { slug: "seo-analyst", name: "SEO Analyst" },
  { slug: "lead-gen", name: "Lead Generator" },
  { slug: "researcher", name: "Researcher" },
  { slug: "scheduler", name: "Scheduler" },
  { slug: "sales-rep", name: "Sales Rep" },
];

// ── filterAgents ────────────────────────────────────────────────────

describe("filterAgents", () => {
  it("returns all agents when query is empty", () => {
    const result = filterAgents("", AGENTS);
    assert.equal(result.length, AGENTS.length);
  });

  it("filters by slug prefix", () => {
    const result = filterAgents("s", AGENTS);
    // seo-analyst, scheduler, sales-rep
    assert.equal(result.length, 3);
    assert.ok(result.every((a) => a.slug.startsWith("s") || a.name.toLowerCase().startsWith("s")));
  });

  it("filters by name prefix", () => {
    const result = filterAgents("Lead", AGENTS);
    assert.equal(result.length, 1);
    assert.equal(result[0].slug, "lead-gen");
  });

  it("is case insensitive", () => {
    const result = filterAgents("SEO", AGENTS);
    assert.equal(result.length, 1);
    assert.equal(result[0].slug, "seo-analyst");
  });

  it("returns empty array when no match", () => {
    const result = filterAgents("xyz", AGENTS);
    assert.equal(result.length, 0);
  });

  it("matches exact slug prefix", () => {
    const result = filterAgents("re", AGENTS);
    assert.equal(result.length, 1);
    assert.equal(result[0].slug, "researcher");
  });
});

// ── computeMentionState ─────────────────────────────────────────────

describe("computeMentionState", () => {
  it("returns not visible for empty input", () => {
    const state = computeMentionState("", AGENTS);
    assert.equal(state.visible, false);
  });

  it("returns not visible for input without @", () => {
    const state = computeMentionState("hello world", AGENTS);
    assert.equal(state.visible, false);
  });

  it("returns visible with all agents for bare @", () => {
    const state = computeMentionState("@", AGENTS);
    assert.equal(state.visible, true);
    assert.equal(state.matches.length, AGENTS.length);
    assert.equal(state.query, "");
  });

  it("returns visible with filtered agents for @s", () => {
    const state = computeMentionState("@s", AGENTS);
    assert.equal(state.visible, true);
    assert.equal(state.matches.length, 3); // seo-analyst, scheduler, sales-rep
    assert.equal(state.query, "s");
  });

  it("returns not visible when @ is followed by a space", () => {
    const state = computeMentionState("@seo-analyst hello", AGENTS);
    assert.equal(state.visible, false);
  });

  it("returns not visible when exact slug match only", () => {
    const state = computeMentionState("@researcher", AGENTS);
    assert.equal(state.visible, false); // exact match = dismiss
  });

  it("returns not visible when no matches", () => {
    const state = computeMentionState("@zzz", AGENTS);
    assert.equal(state.visible, false);
  });

  it("works when @ is mid-sentence", () => {
    const state = computeMentionState("hey @s", AGENTS);
    assert.equal(state.visible, true);
    assert.equal(state.query, "s");
    assert.equal(state.atIndex, 4);
  });

  it("ignores @ not preceded by whitespace", () => {
    const state = computeMentionState("email@s", AGENTS);
    assert.equal(state.visible, false);
  });

  it("starts with selectedIndex 0", () => {
    const state = computeMentionState("@s", AGENTS);
    assert.equal(state.selectedIndex, 0);
  });
});

// ── createMentionActions ────────────────────────────────────────────

describe("createMentionActions", () => {
  function makeState(input: string): MentionState {
    return computeMentionState(input, AGENTS);
  }

  it("onTab cycles through matches", () => {
    const state = makeState("@s"); // 3 matches
    let lastState = state;
    const setState = (s: MentionState) => { lastState = s; };

    const actions = createMentionActions(state, setState, AGENTS, "@s");
    const result = actions.onTab();
    assert.equal(result, null);
    assert.equal(lastState.selectedIndex, 1);
  });

  it("onTab wraps around", () => {
    const state: MentionState = {
      visible: true,
      matches: AGENTS.slice(0, 3),
      selectedIndex: 2,
      query: "",
      atIndex: 0,
    };
    let lastState = state;
    const setState = (s: MentionState) => { lastState = s; };

    const actions = createMentionActions(state, setState, AGENTS, "@");
    actions.onTab();
    assert.equal(lastState.selectedIndex, 0);
  });

  it("onTab accepts immediately when only one match", () => {
    const state: MentionState = {
      visible: true,
      matches: [{ slug: "researcher", name: "Researcher" }],
      selectedIndex: 0,
      query: "re",
      atIndex: 0,
    };
    let lastState = state;
    const setState = (s: MentionState) => { lastState = s; };

    const actions = createMentionActions(state, setState, AGENTS, "@re");
    const result = actions.onTab();
    assert.ok(result, "should return result for single match");
    assert.equal(result!.agentSlug, "researcher");
    assert.ok(result!.text.includes("@researcher"));
    assert.equal(lastState.visible, false);
  });

  it("onShiftTab cycles backwards", () => {
    const state: MentionState = {
      visible: true,
      matches: AGENTS.slice(0, 3),
      selectedIndex: 0,
      query: "",
      atIndex: 0,
    };
    let lastState = state;
    const setState = (s: MentionState) => { lastState = s; };

    const actions = createMentionActions(state, setState, AGENTS, "@");
    actions.onShiftTab();
    assert.equal(lastState.selectedIndex, 2);
  });

  it("onAccept returns selected agent slug", () => {
    const state: MentionState = {
      visible: true,
      matches: [
        { slug: "seo-analyst", name: "SEO Analyst" },
        { slug: "scheduler", name: "Scheduler" },
      ],
      selectedIndex: 1,
      query: "s",
      atIndex: 4,
    };
    let lastState = state;
    const setState = (s: MentionState) => { lastState = s; };

    const actions = createMentionActions(state, setState, AGENTS, "hey @s");
    const result = actions.onAccept();
    assert.ok(result);
    assert.equal(result!.agentSlug, "scheduler");
    assert.ok(result!.text.includes("@scheduler"));
    assert.equal(lastState.visible, false);
  });

  it("onAccept returns null when not visible", () => {
    const state = createMentionState();
    const setState = () => {};

    const actions = createMentionActions(state, setState, AGENTS, "");
    const result = actions.onAccept();
    assert.equal(result, null);
  });

  it("onDismiss hides the overlay", () => {
    const state: MentionState = {
      visible: true,
      matches: AGENTS.slice(0, 3),
      selectedIndex: 1,
      query: "s",
      atIndex: 0,
    };
    let lastState = state;
    const setState = (s: MentionState) => { lastState = s; };

    const actions = createMentionActions(state, setState, AGENTS, "@s");
    actions.onDismiss();
    assert.equal(lastState.visible, false);
    assert.equal(lastState.matches.length, 0);
  });

  it("onTab returns null when not visible", () => {
    const state = createMentionState();
    const setState = () => {};

    const actions = createMentionActions(state, setState, AGENTS, "");
    assert.equal(actions.onTab(), null);
  });

  it("accept replaces @query with @slug in mid-sentence", () => {
    const state: MentionState = {
      visible: true,
      matches: [{ slug: "lead-gen", name: "Lead Generator" }],
      selectedIndex: 0,
      query: "le",
      atIndex: 6,
    };
    let lastState = state;
    const setState = (s: MentionState) => { lastState = s; };

    const actions = createMentionActions(state, setState, AGENTS, "hello @le");
    const result = actions.onTab(); // single match → accept
    assert.ok(result);
    assert.equal(result!.text, "hello @lead-gen ");
  });
});

// ── MentionAutocomplete component ───────────────────────────────────

describe("MentionAutocomplete", () => {
  it("renders nothing when not visible", () => {
    const state = createMentionState();
    const { lastFrame } = render(<MentionAutocomplete state={state} />);
    const frame = lastFrame() ?? "";
    assert.equal(frame.trim(), "");
  });

  it("renders matching agents with @ prefix", () => {
    const state: MentionState = {
      visible: true,
      matches: [
        { slug: "seo-analyst", name: "SEO Analyst" },
        { slug: "scheduler", name: "Scheduler" },
      ],
      selectedIndex: 0,
      query: "s",
      atIndex: 0,
    };
    const { lastFrame } = render(<MentionAutocomplete state={state} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("@seo-analyst"), "should show @seo-analyst");
    assert.ok(frame.includes("@scheduler"), "should show @scheduler");
    assert.ok(frame.includes("SEO Analyst"), "should show name");
  });

  it("highlights the selected item with >", () => {
    const state: MentionState = {
      visible: true,
      matches: [
        { slug: "seo-analyst", name: "SEO Analyst" },
        { slug: "scheduler", name: "Scheduler" },
      ],
      selectedIndex: 1,
      query: "s",
      atIndex: 0,
    };
    const { lastFrame } = render(<MentionAutocomplete state={state} />);
    const frame = strip(lastFrame() ?? "");
    const lines = frame.split("\n");
    const schedulerLine = lines.find((l) => l.includes("@scheduler"));
    assert.ok(schedulerLine, "should find @scheduler line");
    assert.ok(schedulerLine.includes(">"), "selected line should have > indicator");
  });

  it("respects maxVisible", () => {
    const matches: AgentEntry[] = Array.from({ length: 10 }, (_, i) => ({
      slug: `agent${i}`,
      name: `Agent ${i}`,
    }));
    const state: MentionState = {
      visible: true,
      matches,
      selectedIndex: 0,
      query: "",
      atIndex: 0,
    };
    const { lastFrame } = render(
      <MentionAutocomplete state={state} maxVisible={3} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("1/10"), "should show scroll position");
    const agentLines = frame.split("\n").filter((l) => l.includes("@agent"));
    assert.ok(agentLines.length <= 3, `should show at most 3 items, got ${agentLines.length}`);
  });
});
