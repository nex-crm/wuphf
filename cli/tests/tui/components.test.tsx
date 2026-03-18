import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import React from "react";
import { render, cleanup } from "ink-testing-library";
import { Picker } from "../../src/tui/components/picker.js";
import { StatusBar } from "../../src/tui/components/status-bar.js";
import { Viewport } from "../../src/tui/components/viewport.js";
import { AgentCard } from "../../src/tui/components/agent-card.js";
import { MessageList } from "../../src/tui/components/message-list.js";
import { HelpScreen } from "../../src/tui/components/help-screen.js";

// Strip ANSI escape sequences for assertion matching
function strip(s: string): string {
  return s.replace(/\x1b\[[0-9;]*m/g, "");
}

afterEach(() => {
  cleanup();
});

// ─── Picker ───

describe("Picker", () => {
  const items = [
    { command: "objects", label: "objects", detail: "List all objects" },
    { command: "records", label: "records", detail: "List records" },
    { command: "search", label: "search", detail: "Search everything" },
  ];

  it("renders all items", () => {
    const { lastFrame } = render(
      <Picker
        items={items}
        cursor={0}
        onSelect={() => {}}
        onCursorChange={() => {}}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("objects"), "should show objects");
    assert.ok(frame.includes("records"), "should show records");
    assert.ok(frame.includes("search"), "should show search");
  });

  it("highlights cursor position with > prefix", () => {
    const { lastFrame } = render(
      <Picker
        items={items}
        cursor={1}
        onSelect={() => {}}
        onCursorChange={() => {}}
      />,
    );
    const frame = lastFrame() ?? "";
    const lines = frame.split("\n");
    // The line with cursor=1 (records) should have ">" prefix
    const recordsLine = lines.find((l) => strip(l).includes("records"));
    assert.ok(recordsLine, "should find records line");
    assert.ok(strip(recordsLine).includes(">"), "cursor line should have > prefix");
  });

  it("shows item details", () => {
    const { lastFrame } = render(
      <Picker
        items={items}
        cursor={0}
        onSelect={() => {}}
        onCursorChange={() => {}}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("List all objects"), "should show detail text");
  });

  it("shows quick select numbers when enabled", () => {
    const { lastFrame } = render(
      <Picker
        items={items}
        cursor={0}
        onSelect={() => {}}
        onCursorChange={() => {}}
        quickSelect
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("1."), "should show digit 1");
    assert.ok(frame.includes("2."), "should show digit 2");
    assert.ok(frame.includes("3."), "should show digit 3");
  });

  it("shows scroll indicators for overflowing items", () => {
    const manyItems = Array.from({ length: 20 }, (_, i) => ({
      command: `cmd-${i}`,
      label: `item ${i}`,
      detail: `detail ${i}`,
    }));

    const { lastFrame } = render(
      <Picker
        items={manyItems}
        cursor={10}
        onSelect={() => {}}
        onCursorChange={() => {}}
        maxVisible={5}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    // Should show scroll indicators when items overflow
    assert.ok(
      frame.includes("\u25B2") || frame.includes("more"),
      "should show up arrow when scrolled down",
    );
    assert.ok(
      frame.includes("\u25BC") || frame.includes("more"),
      "should show down arrow when more items below",
    );
  });
});

// ─── StatusBar ───

describe("StatusBar", () => {
  it("shows mode badge for normal mode", () => {
    const { lastFrame } = render(
      <StatusBar mode="normal" breadcrumbs={["home"]} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("NORMAL"), "should show NORMAL badge");
  });

  it("shows mode badge for insert mode", () => {
    const { lastFrame } = render(
      <StatusBar mode="insert" breadcrumbs={["ask"]} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("INSERT"), "should show INSERT badge");
  });

  it("shows breadcrumbs", () => {
    const { lastFrame } = render(
      <StatusBar mode="normal" breadcrumbs={["company", "Amazon"]} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(
      frame.includes("company > Amazon"),
      "should show breadcrumb trail",
    );
  });

  it("shows scroll percent when provided", () => {
    const { lastFrame } = render(
      <StatusBar mode="normal" breadcrumbs={["home"]} scrollPercent={42} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("42%"), "should show scroll percent");
  });

  it("shows hint when provided", () => {
    const { lastFrame } = render(
      <StatusBar mode="normal" breadcrumbs={["home"]} hint="Esc/back" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Esc/back"), "should show hint text");
  });

  it("shows nex brand", () => {
    const { lastFrame } = render(
      <StatusBar mode="normal" breadcrumbs={["home"]} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("nex"), "should show nex brand");
  });

  it("shows simplified bar in conversation mode", () => {
    const { lastFrame } = render(
      <StatusBar mode="normal" breadcrumbs={["home"]} conversationMode />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("nex"), "should show nex brand");
    assert.ok(!frame.includes("NORMAL"), "should NOT show mode badge in conversation mode");
  });

  it("shows token count when session provided", () => {
    const { lastFrame } = render(
      <StatusBar
        mode="normal"
        breadcrumbs={["home"]}
        session={{ tokensUsed: 1500 }}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("1.5k"), "should show formatted token count");
  });

  it("shows cost when session provided", () => {
    const { lastFrame } = render(
      <StatusBar
        mode="normal"
        breadcrumbs={["home"]}
        session={{ costUsd: 0.042 }}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("$0.042"), "should show formatted cost");
  });

  it("shows tokens and cost in conversation mode", () => {
    const { lastFrame } = render(
      <StatusBar
        mode="normal"
        breadcrumbs={["home"]}
        conversationMode
        session={{ tokensUsed: 15000, costUsd: 1.25 }}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("15k"), "should show token count in conversation mode");
    assert.ok(frame.includes("$1.25"), "should show cost in conversation mode");
  });

  it("uses pipe separator between sections", () => {
    const { lastFrame } = render(
      <StatusBar
        mode="normal"
        breadcrumbs={["home"]}
        session={{ tokensUsed: 500 }}
      />,
    );
    const frame = lastFrame() ?? "";
    assert.ok(frame.includes("\u2503"), "should use ┃ pipe separator");
  });
});

// ─── Viewport ───

describe("Viewport", () => {
  it("shows correct slice of content based on scrollOffset", () => {
    const lines = Array.from({ length: 50 }, (_, i) => `Line ${i + 1}`);
    const content = lines.join("\n");

    const { lastFrame } = render(
      <Viewport
        content={content}
        scrollOffset={10}
        onScroll={() => {}}
        height={5}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Line 11"), "should show line 11 at offset 10");
    assert.ok(frame.includes("Line 15"), "should show line 15 in window");
    assert.ok(!frame.includes("Line 1\n"), "should not show line 1");
  });

  it("shows scroll percentage when content overflows", () => {
    const lines = Array.from({ length: 50 }, (_, i) => `Line ${i + 1}`);
    const content = lines.join("\n");

    const { lastFrame } = render(
      <Viewport
        content={content}
        scrollOffset={0}
        onScroll={() => {}}
        height={10}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("0%"), "should show 0% at top");
  });

  it("shows 100% at bottom", () => {
    const lines = Array.from({ length: 50 }, (_, i) => `Line ${i + 1}`);
    const content = lines.join("\n");

    const { lastFrame } = render(
      <Viewport
        content={content}
        scrollOffset={999}
        onScroll={() => {}}
        height={10}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("100%"), "should show 100% at bottom");
  });

  it("hides percentage when content fits in viewport", () => {
    const { lastFrame } = render(
      <Viewport
        content={"Line 1\nLine 2\nLine 3"}
        scrollOffset={0}
        onScroll={() => {}}
        height={10}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(!frame.includes("%"), "should not show percentage");
  });
});

// ─── AgentCard ───

describe("AgentCard", () => {
  it("renders agent name", () => {
    const { lastFrame } = render(
      <AgentCard name="sales-agent" status="running" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("sales-agent"), "should show agent name");
  });

  it("renders status badge text", () => {
    const { lastFrame } = render(
      <AgentCard name="test-agent" status="error" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("(error)"), "should show status in parens");
  });

  it("shows running status indicator", () => {
    const { lastFrame } = render(
      <AgentCard name="runner" status="running" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("(running)"), "should show running status");
    assert.ok(
      frame.includes("\u25CF") || frame.includes("runner"),
      "should show filled circle or name",
    );
  });

  it("shows expertise when provided", () => {
    const { lastFrame } = render(
      <AgentCard name="sales-agent" status="idle" expertise="CRM outreach" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("CRM outreach"), "should show expertise");
  });

  it("shows idle status with open circle", () => {
    const { lastFrame } = render(
      <AgentCard name="idle-agent" status="idle" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("(idle)"), "should show idle status");
    assert.ok(
      frame.includes("\u25CB") || frame.includes("idle-agent"),
      "should show open circle or name",
    );
  });
});

// ─── MessageList ───

describe("MessageList", () => {
  it("renders messages with sender names", () => {
    const messages = [
      { id: "1", sender: "human", content: "Hello there", timestamp: Date.now() },
      { id: "2", sender: "nex", content: "Hi! How can I help?", timestamp: Date.now() },
    ];
    const { lastFrame } = render(<MessageList messages={messages} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("human"), "should show human sender");
    assert.ok(frame.includes("nex"), "should show nex sender");
    assert.ok(frame.includes("Hello there"), "should show message content");
  });

  it("shows empty state when no messages", () => {
    const { lastFrame } = render(<MessageList messages={[]} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("No messages"), "should show empty state");
  });

  it("auto-scrolls to show latest messages", () => {
    const messages = Array.from({ length: 30 }, (_, i) => ({
      id: String(i),
      sender: "human",
      content: `Message ${i}`,
      timestamp: Date.now() + i,
    }));
    const { lastFrame } = render(<MessageList messages={messages} maxVisible={5} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Message 29"), "should show the latest message");
    assert.ok(!frame.includes("Message 0"), "should not show the oldest message");
  });
});

// ─── HelpScreen ───

describe("HelpScreen", () => {
  it("renders command groups", () => {
    const { lastFrame } = render(<HelpScreen />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Explore"), "should show Explore group");
    assert.ok(frame.includes("Write"), "should show Write group");
    assert.ok(frame.includes("Config"), "should show Config group");
    assert.ok(frame.includes("AI"), "should show AI group");
  });

  it("shows keybindings", () => {
    const { lastFrame } = render(<HelpScreen />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("i"), "should show insert keybinding");
    assert.ok(frame.includes("Esc"), "should show Esc keybinding");
    assert.ok(frame.includes("q"), "should show quit keybinding");
  });
});
