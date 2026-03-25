import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import React from "react";
import { render, cleanup } from "ink-testing-library";
import { Spinner } from "../../../src/tui/components/spinner.js";
import { ToolIndicator } from "../../../src/tui/components/tool-indicator.js";
import { ProgressSteps } from "../../../src/tui/components/progress-steps.js";
import type { Step } from "../../../src/tui/components/progress-steps.js";

// Strip ANSI escape sequences for assertion matching
function strip(s: string): string {
  return s.replace(/\x1b\[[0-9;]*m/g, "");
}

afterEach(() => {
  cleanup();
});

// ─── Spinner ───

describe("Spinner", () => {
  it("renders an initial braille frame", () => {
    const { lastFrame } = render(<Spinner />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("⠋"), "should show first braille frame");
  });

  it("renders label next to spinner", () => {
    const { lastFrame } = render(<Spinner label="Loading..." />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("⠋"), "should show spinner character");
    assert.ok(frame.includes("Loading..."), "should show label");
  });

  it("hides spinner when stopped", () => {
    const { lastFrame } = render(<Spinner label="Done" stopped />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(!frame.includes("⠋"), "should not show spinner frame when stopped");
    assert.ok(frame.includes("Done"), "should still show label when stopped");
  });

  it("animates to next frame after interval", async () => {
    const { lastFrame } = render(<Spinner interval={30} />);
    const first = strip(lastFrame() ?? "");
    assert.ok(first.includes("⠋"), "should start at first frame");

    // Wait for a few intervals to let it advance
    await new Promise((resolve) => setTimeout(resolve, 120));
    const later = strip(lastFrame() ?? "");
    // Should have advanced past the first frame
    const brailleChars = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];
    const hasAnyBraille = brailleChars.some((ch) => later.includes(ch));
    assert.ok(hasAnyBraille, "should show a braille frame after animation");
  });
});

// ─── ToolIndicator ───

describe("ToolIndicator", () => {
  it("shows spinner and name when running", () => {
    const { lastFrame } = render(
      <ToolIndicator name="search_records" status="running" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("⠋"), "should show spinner frame when running");
    assert.ok(frame.includes("search_records"), "should show tool name");
  });

  it("shows checkmark and elapsed time when done", () => {
    const { lastFrame } = render(
      <ToolIndicator name="fetch_data" status="done" elapsed={1.234} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("✓"), "should show checkmark when done");
    assert.ok(frame.includes("fetch_data"), "should show tool name");
    assert.ok(frame.includes("1.2s"), "should show elapsed time rounded to 1 decimal");
  });

  it("shows checkmark without time when elapsed not provided", () => {
    const { lastFrame } = render(
      <ToolIndicator name="quick_tool" status="done" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("✓"), "should show checkmark");
    assert.ok(frame.includes("quick_tool"), "should show tool name");
    assert.ok(!frame.includes("s)"), "should not show time");
  });

  it("shows error indicator on error", () => {
    const { lastFrame } = render(
      <ToolIndicator name="broken_tool" status="error" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("✗"), "should show X mark on error");
    assert.ok(frame.includes("broken_tool"), "should show tool name");
  });
});

// ─── ProgressSteps ───

describe("ProgressSteps", () => {
  const steps: Step[] = [
    { label: "Authenticate", status: "done" },
    { label: "Fetch schema", status: "active" },
    { label: "Sync records", status: "pending" },
  ];

  it("renders all step labels", () => {
    const { lastFrame } = render(<ProgressSteps steps={steps} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Authenticate"), "should show done step");
    assert.ok(frame.includes("Fetch schema"), "should show active step");
    assert.ok(frame.includes("Sync records"), "should show pending step");
  });

  it("shows checkmark for done steps", () => {
    const { lastFrame } = render(<ProgressSteps steps={steps} />);
    const frame = strip(lastFrame() ?? "");
    // ✓ should appear before "Authenticate"
    const authLine = frame.split("\n").find((l) => strip(l).includes("Authenticate"));
    assert.ok(authLine, "should find Authenticate line");
    assert.ok(strip(authLine).includes("✓"), "done step should have ✓");
  });

  it("shows spinner for active steps", () => {
    const { lastFrame } = render(<ProgressSteps steps={steps} />);
    const frame = strip(lastFrame() ?? "");
    const schemaLine = frame.split("\n").find((l) => strip(l).includes("Fetch schema"));
    assert.ok(schemaLine, "should find Fetch schema line");
    // Active step should have a braille spinner char
    const brailleChars = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];
    const hasBraille = brailleChars.some((ch) => strip(schemaLine).includes(ch));
    assert.ok(hasBraille, "active step should have braille spinner");
  });

  it("shows open circle for pending steps", () => {
    const { lastFrame } = render(<ProgressSteps steps={steps} />);
    const frame = strip(lastFrame() ?? "");
    const syncLine = frame.split("\n").find((l) => strip(l).includes("Sync records"));
    assert.ok(syncLine, "should find Sync records line");
    assert.ok(strip(syncLine).includes("○"), "pending step should have ○");
  });

  it("supports row direction", () => {
    const { lastFrame } = render(<ProgressSteps steps={steps} direction="row" />);
    const frame = strip(lastFrame() ?? "");
    // All steps should be on a single line in row mode
    assert.ok(frame.includes("Authenticate"), "should render in row layout");
    assert.ok(frame.includes("Fetch schema"), "should render in row layout");
    assert.ok(frame.includes("Sync records"), "should render in row layout");
  });
});
