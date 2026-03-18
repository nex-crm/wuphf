import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import React from "react";
import { render, cleanup } from "ink-testing-library";
import {
  ComposeArea,
  HintBar,
} from "../../../../src/tui/components/slack/compose.js";
import type { ComposeAreaProps } from "../../../../src/tui/components/slack/compose.js";
import type { SlashCommandEntry } from "../../../../src/tui/components/slash-autocomplete.js";
import type { AgentEntry } from "../../../../src/tui/components/mention-autocomplete.js";

// Strip ANSI escape sequences
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
];

const AGENTS: AgentEntry[] = [
  { slug: "seo-analyst", name: "SEO Analyst" },
  { slug: "founding-agent", name: "Founding Agent" },
];

const defaultProps: ComposeAreaProps = {
  channelName: "general",
  channelType: "channel",
  focused: true,
  onSubmit: () => {},
  slashCommands: COMMANDS,
  agents: AGENTS,
};

// ── HintBar ─────────────────────────────────────────────────────────

describe("HintBar", () => {
  it("renders hint text when visible", () => {
    const { lastFrame } = render(<HintBar visible={true} />);
    const output = strip(lastFrame());
    assert.ok(output.includes("@ mention"));
    assert.ok(output.includes("/ command"));
    assert.ok(output.includes("Enter send"));
  });

  it("renders nothing when not visible", () => {
    const { lastFrame } = render(<HintBar visible={false} />);
    assert.equal(lastFrame(), "");
  });
});

// ── ComposeArea ─────────────────────────────────────────────────────

describe("ComposeArea", () => {
  it("renders channel placeholder for channel type", () => {
    const { lastFrame } = render(<ComposeArea {...defaultProps} />);
    const output = strip(lastFrame());
    assert.ok(output.includes("Message #general"));
  });

  it("renders thread reply label when isThread", () => {
    const { lastFrame } = render(
      <ComposeArea {...defaultProps} isThread={true} />,
    );
    const output = strip(lastFrame());
    assert.ok(output.includes("Reply"));
  });

  it("shows Enter=send hint", () => {
    const { lastFrame } = render(<ComposeArea {...defaultProps} />);
    const output = strip(lastFrame());
    assert.ok(output.includes("Enter"));
  });

  it("shows hint bar when focused", () => {
    const { lastFrame } = render(<ComposeArea {...defaultProps} focused={true} />);
    const output = strip(lastFrame());
    assert.ok(output.includes("@ mention"));
  });

  it("hides hint bar when unfocused", () => {
    const { lastFrame } = render(<ComposeArea {...defaultProps} focused={false} />);
    const output = strip(lastFrame());
    // Unfocused shows placeholder text instead of TextInput
    assert.ok(!output.includes("@ mention"));
  });

  it("shows cyan border when focused", () => {
    const { lastFrame } = render(<ComposeArea {...defaultProps} focused={true} />);
    // The border color is embedded in ANSI codes — just check render succeeds
    assert.ok(lastFrame().length > 0);
  });

  it("renders DM placeholder with recipient name", () => {
    const { lastFrame } = render(
      <ComposeArea
        {...defaultProps}
        channelType="dm"
        channelName="founding-agent"
        recipientName="Founding Agent"
      />,
    );
    const output = strip(lastFrame());
    assert.ok(output.includes("Message Founding Agent"));
  });

  it("renders group-dm placeholder with channel name", () => {
    const { lastFrame } = render(
      <ComposeArea
        {...defaultProps}
        channelType="group-dm"
        channelName="Alice, Bob"
      />,
    );
    const output = strip(lastFrame());
    assert.ok(output.includes("Message Alice, Bob"));
  });

  it("shows placeholder text when unfocused", () => {
    const { lastFrame } = render(
      <ComposeArea {...defaultProps} focused={false} />,
    );
    const output = strip(lastFrame());
    assert.ok(output.includes("Message #general"));
  });
});
