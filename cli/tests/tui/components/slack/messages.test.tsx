import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import React from "react";
import { render, cleanup } from "ink-testing-library";
import {
  groupMessages,
  SlackMessageList,
  DateSeparator,
  UnreadSeparator,
  SystemMessage,
  ThreadIndicator,
} from "../../../../src/tui/components/slack/messages.js";
import type { ChatMessageInput } from "../../../../src/tui/components/slack/messages.js";
import { resetAgentColors } from "../../../../src/tui/agent-colors.js";

function strip(s: string): string {
  return s.replace(/\x1b\[[0-9;]*m/g, "");
}

afterEach(() => {
  cleanup();
  resetAgentColors();
});

// ── Helper: create a message ────────────────────────────────────────

function msg(
  overrides: Partial<ChatMessageInput> & { id: string },
): ChatMessageInput {
  return {
    sender: "SEO Analyst",
    senderType: "agent",
    content: "Hello world",
    timestamp: Date.now(),
    ...overrides,
  };
}

// ── groupMessages ───────────────────────────────────────────────────

describe("groupMessages", () => {
  it("groups same-sender messages within 5 minutes", () => {
    const now = Date.now();
    const msgs: ChatMessageInput[] = [
      msg({ id: "1", sender: "Alice", timestamp: now }),
      msg({ id: "2", sender: "Alice", timestamp: now + 60_000 }), // 1 min later
      msg({ id: "3", sender: "Alice", timestamp: now + 120_000 }), // 2 min later
    ];

    const grouped = groupMessages(msgs);
    assert.equal(grouped[0]!.isFirstInGroup, true, "first message starts group");
    assert.equal(grouped[1]!.isFirstInGroup, false, "second is continuation");
    assert.equal(grouped[2]!.isFirstInGroup, false, "third is continuation");
  });

  it("splits on sender change", () => {
    const now = Date.now();
    const msgs: ChatMessageInput[] = [
      msg({ id: "1", sender: "Alice", timestamp: now }),
      msg({ id: "2", sender: "Bob", timestamp: now + 30_000 }),
      msg({ id: "3", sender: "Alice", timestamp: now + 60_000 }),
    ];

    const grouped = groupMessages(msgs);
    assert.equal(grouped[0]!.isFirstInGroup, true);
    assert.equal(grouped[1]!.isFirstInGroup, true, "sender change starts new group");
    assert.equal(grouped[2]!.isFirstInGroup, true, "sender change back starts new group");
  });

  it("splits when gap exceeds 5 minutes", () => {
    const now = Date.now();
    const msgs: ChatMessageInput[] = [
      msg({ id: "1", sender: "Alice", timestamp: now }),
      msg({ id: "2", sender: "Alice", timestamp: now + 6 * 60_000 }), // 6 min later
    ];

    const grouped = groupMessages(msgs);
    assert.equal(grouped[0]!.isFirstInGroup, true);
    assert.equal(grouped[1]!.isFirstInGroup, true, "6 min gap starts new group");
  });

  it("inserts date separators between different days", () => {
    const day1 = new Date("2026-03-15T10:00:00").getTime();
    const day2 = new Date("2026-03-16T10:00:00").getTime();
    const msgs: ChatMessageInput[] = [
      msg({ id: "1", sender: "Alice", timestamp: day1 }),
      msg({ id: "2", sender: "Alice", timestamp: day2 }),
    ];

    const grouped = groupMessages(msgs);
    assert.ok(grouped[0]!.dateSeparator, "first day has date separator");
    assert.ok(grouped[1]!.dateSeparator, "second day has date separator");
    assert.notEqual(
      grouped[0]!.dateSeparator,
      grouped[1]!.dateSeparator,
      "separators differ",
    );
  });

  it("marks unread boundary", () => {
    const now = Date.now();
    const msgs: ChatMessageInput[] = [
      msg({ id: "1", sender: "Alice", timestamp: now }),
      msg({ id: "2", sender: "Bob", timestamp: now + 60_000 }),
      msg({ id: "3", sender: "Alice", timestamp: now + 120_000 }),
    ];

    const grouped = groupMessages(msgs, now + 30_000);
    assert.equal(grouped[0]!.isUnreadMarker, false, "before unread threshold");
    assert.equal(grouped[1]!.isUnreadMarker, true, "first message after threshold");
    assert.equal(grouped[2]!.isUnreadMarker, false, "only first gets marker");
  });

  it("system messages are always first in group", () => {
    const now = Date.now();
    const msgs: ChatMessageInput[] = [
      msg({ id: "1", sender: "system", senderType: "system", timestamp: now, content: "joined" }),
      msg({ id: "2", sender: "system", senderType: "system", timestamp: now + 1000, content: "left" }),
    ];

    const grouped = groupMessages(msgs);
    assert.equal(grouped[0]!.isFirstInGroup, true);
    assert.equal(grouped[1]!.isFirstInGroup, true, "each system msg is its own group");
    assert.equal(grouped[0]!.isSystem, true);
    assert.equal(grouped[1]!.isSystem, true);
  });
});

// ── Component rendering ─────────────────────────────────────────────

describe("SlackMessageList – first message rendering", () => {
  it("first message shows avatar + name + timestamp", () => {
    const now = Date.now();
    const { lastFrame } = render(
      <SlackMessageList
        messages={[msg({ id: "1", sender: "SEO Analyst", timestamp: now })]}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("[S]"), "should show single-char avatar");
    assert.ok(frame.includes("SEO Analyst"), "should show sender name");
    assert.ok(frame.includes("Hello world"), "should show message content");
  });

  it("human messages use [>] avatar", () => {
    const { lastFrame } = render(
      <SlackMessageList
        messages={[
          msg({ id: "1", sender: "you", senderType: "human", timestamp: Date.now() }),
        ]}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("[>]"), "human avatar should be [>]");
  });
});

describe("SlackMessageList – continuation messages", () => {
  it("continuation message is indented without avatar", () => {
    const now = Date.now();
    const { lastFrame } = render(
      <SlackMessageList
        messages={[
          msg({ id: "1", sender: "Alice", content: "First", timestamp: now }),
          msg({
            id: "2",
            sender: "Alice",
            content: "Second",
            timestamp: now + 30_000,
          }),
        ]}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    // "Alice" should appear only once (in the first message header)
    const aliceCount = (frame.match(/Alice/g) || []).length;
    assert.equal(aliceCount, 1, "sender name appears only once for the group");
    assert.ok(frame.includes("Second"), "continuation content renders");
  });
});

describe("ThreadIndicator", () => {
  it("shows reply count", () => {
    const { lastFrame } = render(
      <ThreadIndicator replyCount={3} lastReplyTimestamp={Date.now()} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("3 replies"), "should show reply count");
    assert.ok(frame.includes("Last reply"), "should show last reply text");
    assert.ok(frame.includes("↳"), "should show thread arrow");
  });

  it("shows singular reply", () => {
    const { lastFrame } = render(
      <ThreadIndicator replyCount={1} lastReplyTimestamp={Date.now()} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("1 reply"), "should show singular reply");
  });
});

describe("SystemMessage", () => {
  it("renders centered system text", () => {
    const { lastFrame } = render(
      <SystemMessage
        content="SEO Analyst joined #general"
        timestamp={Date.now()}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(
      frame.includes("SEO Analyst joined #general"),
      "should show system message content",
    );
    assert.ok(frame.includes("✦"), "should show system icon");
  });
});

describe("UnreadSeparator", () => {
  it("renders New marker with lines", () => {
    const { lastFrame } = render(<UnreadSeparator width={40} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("New"), "should show New text");
    assert.ok(frame.includes("─"), "should show horizontal lines");
  });
});

describe("DateSeparator", () => {
  it("shows day name with horizontal lines", () => {
    const { lastFrame } = render(<DateSeparator label="Today" width={40} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Today"), "should show date label");
    assert.ok(frame.includes("─"), "should show horizontal lines");
  });

  it("shows full date for older days", () => {
    const { lastFrame } = render(
      <DateSeparator label="Sunday, March 15" width={50} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Sunday, March 15"), "should show full date");
  });
});
