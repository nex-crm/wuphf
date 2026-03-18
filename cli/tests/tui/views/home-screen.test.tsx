import { describe, it, beforeEach, afterEach } from "node:test";
import assert from "node:assert/strict";
import React from "react";
import { render, cleanup } from "ink-testing-library";
import { HomeScreen } from "../../../src/tui/views/home-screen.js";
import { resetAgentColors } from "../../../src/tui/agent-colors.js";
import type { HomeChannel, HomeMessage, HomeCalendarEvent } from "../../../src/tui/views/home-screen.js";

// Strip ANSI escape sequences for assertion matching
function strip(s: string): string {
  return s.replace(/\x1b\[[0-9;]*m/g, "");
}

afterEach(() => {
  cleanup();
});

beforeEach(() => {
  resetAgentColors();
});

const baseChannels: HomeChannel[] = [
  { id: "ch-general", name: "general", unread: 0 },
  { id: "ch-leads", name: "leads", unread: 2 },
  { id: "ch-seo", name: "seo", unread: 0 },
];

const now = Date.now();

const baseMessages: HomeMessage[] = [
  {
    id: "m1",
    sender: "SEO Analyst",
    senderType: "agent",
    content: "Found 3 new keyword opportunities",
    timestamp: now - 3000,
  },
  {
    id: "m2",
    sender: "Lead Gen",
    senderType: "agent",
    content: "Updated 12 prospect records",
    timestamp: now - 2000,
  },
  {
    id: "m3",
    sender: "you",
    senderType: "human",
    content: "Great, focus on the keyword opportunities",
    timestamp: now - 1000,
  },
];

const baseCalendarEvents: HomeCalendarEvent[] = [
  { agentName: "SEO", agentColor: "cyan", time: "09:00", day: "Today" },
  { agentName: "CS", agentColor: "green", time: "14:00", day: "Today" },
  { agentName: "Lead", agentColor: "yellow", time: "10:00", day: "Tomorrow" },
  { agentName: "SEO", agentColor: "cyan", time: "14:00", day: "Tomorrow" },
  { agentName: "Research", agentColor: "magenta", time: "09:00", day: "Wed" },
];

const noop = () => {};

describe("HomeScreen", () => {
  // ── Channel bar ──

  it("renders channel bar with active channel highlighted", () => {
    const { lastFrame } = render(
      <HomeScreen
        channels={baseChannels}
        activeChannelId="ch-general"
        messages={[]}
        onSend={noop}
        onChannelChange={noop}
        calendarEvents={[]}
        showCalendar={false}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("#general"), "should show #general");
    assert.ok(frame.includes("#leads"), "should show #leads");
    assert.ok(frame.includes("#seo"), "should show #seo");
  });

  it("shows unread count on channels with unread messages", () => {
    const { lastFrame } = render(
      <HomeScreen
        channels={baseChannels}
        activeChannelId="ch-general"
        messages={[]}
        onSend={noop}
        onChannelChange={noop}
        calendarEvents={[]}
        showCalendar={false}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("(2)"), "should show unread count for #leads");
  });

  it("shows Tab=switch hint when multiple channels", () => {
    const { lastFrame } = render(
      <HomeScreen
        channels={baseChannels}
        activeChannelId="ch-general"
        messages={[]}
        onSend={noop}
        onChannelChange={noop}
        calendarEvents={[]}
        showCalendar={false}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("[Tab=switch]"), "should show tab cycling hint");
  });

  // ── Messages ──

  it("renders messages with agent names", () => {
    const { lastFrame } = render(
      <HomeScreen
        channels={baseChannels}
        activeChannelId="ch-general"
        messages={baseMessages}
        onSend={noop}
        onChannelChange={noop}
        calendarEvents={[]}
        showCalendar={false}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("[SEO Analyst]"), "should show agent name in brackets");
    assert.ok(frame.includes("[Lead Gen]"), "should show second agent name");
    assert.ok(frame.includes("Found 3 new keyword"), "should show agent message content");
    assert.ok(frame.includes("Updated 12 prospect"), "should show second agent message");
  });

  it("renders human messages with > prefix", () => {
    const { lastFrame } = render(
      <HomeScreen
        channels={baseChannels}
        activeChannelId="ch-general"
        messages={baseMessages}
        onSend={noop}
        onChannelChange={noop}
        calendarEvents={[]}
        showCalendar={false}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("> "), "should show > prefix for human messages");
    assert.ok(
      frame.includes("Great, focus on the keyword"),
      "should show human message content",
    );
  });

  it("renders system messages", () => {
    const sysMessages: HomeMessage[] = [
      {
        id: "s1",
        sender: "system",
        senderType: "system",
        content: "Welcome to Nex.",
        timestamp: now,
      },
    ];
    const { lastFrame } = render(
      <HomeScreen
        channels={baseChannels}
        activeChannelId="ch-general"
        messages={sysMessages}
        onSend={noop}
        onChannelChange={noop}
        calendarEvents={[]}
        showCalendar={false}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Welcome to Nex"), "should show system message");
  });

  it("shows empty state when no messages", () => {
    const { lastFrame } = render(
      <HomeScreen
        channels={baseChannels}
        activeChannelId="ch-general"
        messages={[]}
        onSend={noop}
        onChannelChange={noop}
        calendarEvents={[]}
        showCalendar={false}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("No messages yet"), "should show empty state text");
  });

  // ── Calendar strip ──

  it("shows calendar events when showCalendar is true", () => {
    const { lastFrame } = render(
      <HomeScreen
        channels={baseChannels}
        activeChannelId="ch-general"
        messages={[]}
        onSend={noop}
        onChannelChange={noop}
        calendarEvents={baseCalendarEvents}
        showCalendar={true}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Today"), "should show Today column");
    assert.ok(frame.includes("Tomorrow"), "should show Tomorrow column");
    assert.ok(frame.includes("Wed"), "should show Wed column");
    assert.ok(frame.includes("09:00"), "should show event time");
    assert.ok(frame.includes("SEO"), "should show agent name in calendar");
  });

  it("hides calendar when showCalendar is false", () => {
    const { lastFrame } = render(
      <HomeScreen
        channels={baseChannels}
        activeChannelId="ch-general"
        messages={[]}
        onSend={noop}
        onChannelChange={noop}
        calendarEvents={baseCalendarEvents}
        showCalendar={false}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    // "Today" and "Tomorrow" should NOT appear when calendar is hidden
    assert.ok(!frame.includes("Today"), "should NOT show calendar day headers when hidden");
    assert.ok(!frame.includes("Tomorrow"), "should NOT show Tomorrow when hidden");
  });

  it("shows no upcoming events message when calendar is empty", () => {
    const { lastFrame } = render(
      <HomeScreen
        channels={baseChannels}
        activeChannelId="ch-general"
        messages={[]}
        onSend={noop}
        onChannelChange={noop}
        calendarEvents={[]}
        showCalendar={true}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("No upcoming events"), "should show empty calendar message");
  });

  // ── Loading state ──

  it("shows loading indicator when isLoading", () => {
    const { lastFrame } = render(
      <HomeScreen
        channels={baseChannels}
        activeChannelId="ch-general"
        messages={[]}
        onSend={noop}
        onChannelChange={noop}
        calendarEvents={[]}
        showCalendar={false}
        isLoading={true}
        loadingHint="thinking..."
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("thinking..."), "should show loading hint");
  });

  // ── tmux detection ──

  it("shows tmux hint when isTmux is true", () => {
    const { lastFrame } = render(
      <HomeScreen
        channels={baseChannels}
        activeChannelId="ch-general"
        messages={[]}
        onSend={noop}
        onChannelChange={noop}
        calendarEvents={[]}
        showCalendar={false}
        isTmux={true}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("tmux detected"), "should show tmux hint");
  });

  it("does not show tmux hint when isTmux is false", () => {
    const { lastFrame } = render(
      <HomeScreen
        channels={baseChannels}
        activeChannelId="ch-general"
        messages={[]}
        onSend={noop}
        onChannelChange={noop}
        calendarEvents={[]}
        showCalendar={false}
        isTmux={false}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(!frame.includes("tmux detected"), "should NOT show tmux hint");
  });

  // ── Input area ──

  it("renders the input prompt", () => {
    const { lastFrame } = render(
      <HomeScreen
        channels={baseChannels}
        activeChannelId="ch-general"
        messages={[]}
        onSend={noop}
        onChannelChange={noop}
        calendarEvents={[]}
        showCalendar={false}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes(">"), "should show input prompt");
  });

  // ── Single channel ──

  it("works with a single channel (no Tab hint)", () => {
    const singleChannel: HomeChannel[] = [
      { id: "ch-general", name: "general", unread: 0 },
    ];
    const { lastFrame } = render(
      <HomeScreen
        channels={singleChannel}
        activeChannelId="ch-general"
        messages={[]}
        onSend={noop}
        onChannelChange={noop}
        calendarEvents={[]}
        showCalendar={false}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("#general"), "should show channel");
    assert.ok(!frame.includes("[Tab=switch]"), "should NOT show tab hint with single channel");
  });
});
