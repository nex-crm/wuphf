import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import React from "react";
import { render, cleanup } from "ink-testing-library";
import { TimelineView } from "../../../src/tui/views/timeline.js";
import type { TimelineEvent } from "../../../src/tui/views/timeline.js";

// Strip ANSI escape sequences for assertion matching
function strip(s: string): string {
  return s.replace(/\x1b\[[0-9;]*m/g, "");
}

afterEach(() => {
  cleanup();
});

const noop = () => {};

const sampleEvents: TimelineEvent[] = [
  {
    id: "ev-1",
    type: "created",
    summary: "Record created",
    actor: "admin",
    timestamp: "2026-03-17T10:00:00Z",
  },
  {
    id: "ev-2",
    type: "updated",
    summary: "Name changed to Acme Corp",
    actor: "admin",
    timestamp: "2026-03-17T10:05:00Z",
  },
  {
    id: "ev-3",
    type: "note",
    summary: "Added meeting notes",
    actor: "jdoe",
    timestamp: "2026-03-17T11:00:00Z",
  },
  {
    id: "ev-4",
    type: "task",
    summary: "Follow-up task assigned",
    timestamp: "2026-03-17T12:00:00Z",
  },
  {
    id: "ev-5",
    type: "relationship",
    summary: "Linked to Globex Inc",
    actor: "admin",
    timestamp: "2026-03-17T13:00:00Z",
  },
  {
    id: "ev-6",
    type: "deleted",
    summary: "Removed old address",
    actor: "admin",
    timestamp: "2026-03-17T14:00:00Z",
  },
];

describe("TimelineView", () => {
  it("renders the header with record label and id", () => {
    const { lastFrame } = render(
      <TimelineView
        recordLabel="Acme Corp"
        recordId="rec-123"
        events={[]}
        onBack={noop}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Timeline"), "should show Timeline header");
    assert.ok(frame.includes("Acme Corp"), "should show record label");
    assert.ok(frame.includes("rec-123"), "should show record id");
  });

  it("shows empty state when no events", () => {
    const { lastFrame } = render(
      <TimelineView
        recordLabel="Acme Corp"
        recordId="rec-123"
        events={[]}
        onBack={noop}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(
      frame.includes("No timeline events"),
      "should show empty state message",
    );
  });

  it("renders event summaries", () => {
    const { lastFrame } = render(
      <TimelineView
        recordLabel="Acme Corp"
        recordId="rec-123"
        events={sampleEvents}
        onBack={noop}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(
      frame.includes("Record created"),
      "should show created event summary",
    );
    assert.ok(
      frame.includes("Name changed to Acme Corp"),
      "should show updated event summary",
    );
    assert.ok(
      frame.includes("Added meeting notes"),
      "should show note event summary",
    );
    assert.ok(
      frame.includes("Follow-up task assigned"),
      "should show task event summary",
    );
    assert.ok(
      frame.includes("Linked to Globex Inc"),
      "should show relationship event summary",
    );
    assert.ok(
      frame.includes("Removed old address"),
      "should show deleted event summary",
    );
  });

  it("renders event type icons", () => {
    const { lastFrame } = render(
      <TimelineView
        recordLabel="Acme Corp"
        recordId="rec-123"
        events={sampleEvents}
        onBack={noop}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("●"), "should show created icon");
    assert.ok(frame.includes("◆"), "should show updated icon");
    assert.ok(frame.includes("✎"), "should show note icon");
    assert.ok(frame.includes("☐"), "should show task icon");
    assert.ok(frame.includes("⇄"), "should show relationship icon");
    assert.ok(frame.includes("✕"), "should show deleted icon");
  });

  it("renders actor names", () => {
    const { lastFrame } = render(
      <TimelineView
        recordLabel="Acme Corp"
        recordId="rec-123"
        events={sampleEvents}
        onBack={noop}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("by admin"), "should show actor name");
    assert.ok(frame.includes("by jdoe"), "should show second actor name");
  });

  it("renders timestamps", () => {
    const { lastFrame } = render(
      <TimelineView
        recordLabel="Acme Corp"
        recordId="rec-123"
        events={sampleEvents}
        onBack={noop}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(
      frame.includes("2026-03-17T10:00:00Z"),
      "should show timestamp",
    );
    assert.ok(
      frame.includes("2026-03-17T14:00:00Z"),
      "should show last timestamp",
    );
  });

  it("renders vertical connectors between events", () => {
    const { lastFrame } = render(
      <TimelineView
        recordLabel="Acme Corp"
        recordId="rec-123"
        events={sampleEvents}
        onBack={noop}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("│"), "should show vertical connectors");
  });

  it("does not render connector after last event", () => {
    const singleEvent: TimelineEvent[] = [
      {
        id: "ev-1",
        type: "created",
        summary: "Record created",
        timestamp: "2026-03-17T10:00:00Z",
      },
    ];
    const { lastFrame } = render(
      <TimelineView
        recordLabel="Acme Corp"
        recordId="rec-123"
        events={singleEvent}
        onBack={noop}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    // The only │ should be from the header separator, not a connector
    const lines = frame.split("\n");
    const eventLine = lines.find((l) => l.includes("Record created"));
    assert.ok(eventLine, "should find event line");
    // Lines after the event line should not start with │
    const eventLineIdx = lines.indexOf(eventLine!);
    const linesAfterEvent = lines.slice(eventLineIdx + 1);
    const connectorLines = linesAfterEvent.filter(
      (l) => l.trim() === "│",
    );
    assert.equal(
      connectorLines.length,
      0,
      "should not have connector after single event",
    );
  });

  it("handles events without actor gracefully", () => {
    const noActorEvents: TimelineEvent[] = [
      {
        id: "ev-1",
        type: "task",
        summary: "Auto-generated task",
        timestamp: "2026-03-17T10:00:00Z",
      },
    ];
    const { lastFrame } = render(
      <TimelineView
        recordLabel="Acme Corp"
        recordId="rec-123"
        events={noActorEvents}
        onBack={noop}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(
      frame.includes("Auto-generated task"),
      "should show summary without actor",
    );
    assert.ok(!frame.includes("by "), "should not show 'by' when no actor");
  });

  it("shows escape hint", () => {
    const { lastFrame } = render(
      <TimelineView
        recordLabel="Acme Corp"
        recordId="rec-123"
        events={[]}
        onBack={noop}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("[Esc=back]"), "should show escape hint");
  });
});
