import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import React from "react";
import { render, cleanup } from "ink-testing-library";
import {
  ThreadPanel,
  ThreadHeader,
  ReplyDivider,
} from "../../../../src/tui/components/slack/thread-panel.js";
import type {
  ThreadMessage,
  ThreadPanelProps,
} from "../../../../src/tui/components/slack/thread-panel.js";

// Strip ANSI escape sequences
function strip(s: string): string {
  return s.replace(/\x1b\[[0-9;]*m/g, "");
}

afterEach(() => {
  cleanup();
});

// ── Test data ───────────────────────────────────────────────────────

const parentMessage: ThreadMessage = {
  id: "msg-1",
  sender: "SEO Analyst",
  senderType: "agent",
  initials: "SE",
  content: "Found 3 keywords",
  timestamp: new Date("2026-03-17T14:30:00").getTime(),
  isFirstInGroup: true,
};

const replies: ThreadMessage[] = [
  {
    id: "reply-1",
    sender: "you",
    senderType: "human",
    initials: "YO",
    content: "Focus on the first one",
    timestamp: new Date("2026-03-17T14:35:00").getTime(),
    isFirstInGroup: true,
  },
  {
    id: "reply-2",
    sender: "SEO Analyst",
    senderType: "agent",
    initials: "SE",
    content: "On it. Starting analysis...",
    timestamp: new Date("2026-03-17T14:36:00").getTime(),
    isFirstInGroup: true,
  },
];

const defaultProps: ThreadPanelProps = {
  width: 45,
  focused: true,
  parentMessage,
  replies,
  sourceChannelName: "general",
  sourceChannelType: "channel",
  alsoSendToChannel: false,
  onSendReply: () => {},
  onToggleAlsoSend: () => {},
  onClose: () => {},
  slashCommands: [],
  agents: [],
};

// ── ThreadHeader ────────────────────────────────────────────────────

describe("ThreadHeader", () => {
  it("shows Thread label and channel name", () => {
    const { lastFrame } = render(
      <ThreadHeader
        channelName="general"
        channelType="channel"
        onClose={() => {}}
      />,
    );
    const output = strip(lastFrame());
    assert.ok(output.includes("Thread"));
    assert.ok(output.includes("#general"));
  });

  it("shows close hint", () => {
    const { lastFrame } = render(
      <ThreadHeader
        channelName="general"
        channelType="channel"
        onClose={() => {}}
      />,
    );
    const output = strip(lastFrame());
    assert.ok(output.includes("Esc"));
  });

  it("shows DM name without # for DM type", () => {
    const { lastFrame } = render(
      <ThreadHeader
        channelName="SEO Analyst"
        channelType="dm"
        onClose={() => {}}
      />,
    );
    const output = strip(lastFrame());
    assert.ok(output.includes("SEO Analyst"));
    assert.ok(!output.includes("#SEO Analyst"));
  });
});

// ── ReplyDivider ────────────────────────────────────────────────────

describe("ReplyDivider", () => {
  it("shows reply count (plural)", () => {
    const { lastFrame } = render(<ReplyDivider count={3} width={40} />);
    const output = strip(lastFrame());
    assert.ok(output.includes("3 replies"));
  });

  it("shows singular for 1 reply", () => {
    const { lastFrame } = render(<ReplyDivider count={1} width={40} />);
    const output = strip(lastFrame());
    assert.ok(output.includes("1 reply"));
    assert.ok(!output.includes("1 replies"));
  });
});

// ── ThreadPanel ─────────────────────────────────────────────────────

describe("ThreadPanel", () => {
  it("renders parent message", () => {
    const { lastFrame } = render(<ThreadPanel {...defaultProps} />);
    const output = strip(lastFrame());
    assert.ok(output.includes("SEO Analyst"));
    assert.ok(output.includes("Found 3 keywords"));
  });

  it("renders reply count divider", () => {
    const { lastFrame } = render(<ThreadPanel {...defaultProps} />);
    const output = strip(lastFrame());
    assert.ok(output.includes("2 replies"));
  });

  it("renders replies", () => {
    const { lastFrame } = render(<ThreadPanel {...defaultProps} />);
    const output = strip(lastFrame());
    assert.ok(output.includes("Focus on the first one"));
    assert.ok(output.includes("On it. Starting analysis..."));
  });

  it("shows unchecked also-send checkbox", () => {
    const { lastFrame } = render(<ThreadPanel {...defaultProps} />);
    const output = strip(lastFrame());
    assert.ok(output.includes("☐"));
    assert.ok(output.includes("Also send to #general"));
  });

  it("shows checked also-send checkbox", () => {
    const { lastFrame } = render(
      <ThreadPanel {...defaultProps} alsoSendToChannel={true} />,
    );
    const output = strip(lastFrame());
    assert.ok(output.includes("☑"));
  });

  it("contains reply input area", () => {
    const { lastFrame } = render(<ThreadPanel {...defaultProps} />);
    const output = strip(lastFrame());
    assert.ok(output.includes("Reply"));
  });
});
