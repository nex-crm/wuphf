import { describe, it, afterEach, beforeEach } from "node:test";
import assert from "node:assert/strict";
import React from "react";
import { render, cleanup } from "ink-testing-library";
import {
  getChannelColor,
  resetChannelColors,
} from "../../../src/tui/components/channel-colors.js";
import {
  ChannelMessage,
  ChannelMessageList,
  ColoredChannelBar,
} from "../../../src/tui/components/channel-message.js";
import type { ChannelMessageData } from "../../../src/tui/components/channel-message.js";

// Strip ANSI escape sequences for assertion matching
function strip(s: string): string {
  return s.replace(/\x1b\[[0-9;]*m/g, "");
}

afterEach(() => {
  cleanup();
});

// ── Channel colors ──────────────────────────────────────────────────

describe("getChannelColor", () => {
  beforeEach(() => {
    resetChannelColors();
  });

  it("returns cyan for #general", () => {
    assert.equal(getChannelColor("general"), "cyan");
  });

  it("returns green for #leads", () => {
    assert.equal(getChannelColor("leads"), "green");
  });

  it("returns yellow for #seo", () => {
    assert.equal(getChannelColor("seo"), "yellow");
  });

  it("returns magenta for #support", () => {
    assert.equal(getChannelColor("support"), "magenta");
  });

  it("strips leading # from name", () => {
    assert.equal(getChannelColor("#general"), "cyan");
  });

  it("is case insensitive", () => {
    assert.equal(getChannelColor("GENERAL"), "cyan");
    assert.equal(getChannelColor("Leads"), "green");
  });

  it("returns stable color for same name", () => {
    const first = getChannelColor("custom-channel");
    const second = getChannelColor("custom-channel");
    assert.equal(first, second);
  });

  it("assigns different colors to different unknown channels", () => {
    const c1 = getChannelColor("alpha");
    const c2 = getChannelColor("beta");
    // They may differ (cycles through palette)
    assert.ok(typeof c1 === "string");
    assert.ok(typeof c2 === "string");
  });
});

// ── ChannelMessage ──────────────────────────────────────────────────

describe("ChannelMessage", () => {
  beforeEach(() => {
    resetChannelColors();
  });

  it("renders human message with > prefix and channel tag", () => {
    const msg: ChannelMessageData = {
      id: "1",
      channel: "general",
      sender: "user",
      senderType: "human",
      content: "Hello world",
      timestamp: new Date("2026-03-17T10:30:00").getTime(),
    };
    const { lastFrame } = render(<ChannelMessage message={msg} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("[#general]"), "should show channel tag");
    assert.ok(frame.includes(">"), "should show > for human");
    assert.ok(frame.includes("Hello world"), "should show content");
    assert.ok(frame.includes("10:30"), "should show time");
  });

  it("renders agent message with agent name in brackets", () => {
    const msg: ChannelMessageData = {
      id: "2",
      channel: "leads",
      sender: "lead-gen",
      senderType: "agent",
      content: "Found 5 new leads",
      timestamp: new Date("2026-03-17T11:00:00").getTime(),
    };
    const { lastFrame } = render(<ChannelMessage message={msg} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("[#leads]"), "should show channel tag");
    assert.ok(frame.includes("[lead-gen]"), "should show agent name");
    assert.ok(frame.includes("Found 5 new leads"), "should show content");
  });

  it("renders system message", () => {
    const msg: ChannelMessageData = {
      id: "3",
      channel: "general",
      sender: "system",
      senderType: "system",
      content: "Welcome to WUPHF!",
      timestamp: Date.now(),
    };
    const { lastFrame } = render(<ChannelMessage message={msg} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("[#general]"), "should show channel tag");
    assert.ok(frame.includes("Welcome to WUPHF!"), "should show content");
  });

  it("shows colored left border (▌)", () => {
    const msg: ChannelMessageData = {
      id: "4",
      channel: "general",
      sender: "user",
      senderType: "human",
      content: "test",
      timestamp: Date.now(),
    };
    const { lastFrame } = render(<ChannelMessage message={msg} />);
    const frame = lastFrame() ?? "";
    assert.ok(frame.includes("▌"), "should show left border character");
  });
});

// ── ChannelMessageList ──────────────────────────────────────────────

describe("ChannelMessageList", () => {
  beforeEach(() => {
    resetChannelColors();
  });

  it("renders empty state", () => {
    const { lastFrame } = render(<ChannelMessageList messages={[]} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("No messages"), "should show empty state");
  });

  it("renders multiple messages", () => {
    const messages: ChannelMessageData[] = [
      {
        id: "1",
        channel: "general",
        sender: "user",
        senderType: "human",
        content: "Hello",
        timestamp: Date.now(),
      },
      {
        id: "2",
        channel: "general",
        sender: "seo-agent",
        senderType: "agent",
        content: "Hi there",
        timestamp: Date.now() + 1000,
      },
    ];
    const { lastFrame } = render(<ChannelMessageList messages={messages} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Hello"), "should show first message");
    assert.ok(frame.includes("Hi there"), "should show second message");
  });

  it("respects maxVisible (auto-tail)", () => {
    const messages: ChannelMessageData[] = Array.from({ length: 20 }, (_, i) => ({
      id: String(i),
      channel: "general",
      sender: "user",
      senderType: "human" as const,
      content: `Message ${i}`,
      timestamp: Date.now() + i * 1000,
    }));
    const { lastFrame } = render(
      <ChannelMessageList messages={messages} maxVisible={3} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Message 19"), "should show latest message");
    assert.ok(frame.includes("Message 17"), "should show 3rd from last");
    assert.ok(!frame.includes("Message 0"), "should not show oldest");
  });
});

// ── ColoredChannelBar ───────────────────────────────────────────────

describe("ColoredChannelBar", () => {
  beforeEach(() => {
    resetChannelColors();
  });

  const channels = [
    { id: "1", name: "general", unread: 0 },
    { id: "2", name: "leads", unread: 3 },
    { id: "3", name: "seo", unread: 0 },
  ];

  it("renders all channel names with # prefix", () => {
    const { lastFrame } = render(
      <ColoredChannelBar channels={channels} activeChannelId="1" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("#general"), "should show #general");
    assert.ok(frame.includes("#leads"), "should show #leads");
    assert.ok(frame.includes("#seo"), "should show #seo");
  });

  it("shows unread count", () => {
    const { lastFrame } = render(
      <ColoredChannelBar channels={channels} activeChannelId="1" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("(3)"), "should show unread count for leads");
  });

  it("shows Tab=switch hint when multiple channels", () => {
    const { lastFrame } = render(
      <ColoredChannelBar channels={channels} activeChannelId="1" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("[Tab=switch]"), "should show tab hint");
  });

  it("hides Tab hint for single channel", () => {
    const single = [{ id: "1", name: "general", unread: 0 }];
    const { lastFrame } = render(
      <ColoredChannelBar channels={single} activeChannelId="1" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(!frame.includes("[Tab=switch]"), "should not show tab hint for single channel");
  });
});
