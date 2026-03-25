import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import React from "react";
import { render, cleanup } from "ink-testing-library";
import { InlineSelect } from "../../../src/tui/components/inline-select.js";
import type { SelectOption } from "../../../src/tui/components/inline-select.js";

function strip(s: string): string {
  return s.replace(/\x1b\[[0-9;]*m/g, "");
}

afterEach(() => {
  cleanup();
});

const noop = () => {};

const sampleOptions: SelectOption[] = [
  { label: "Option A", value: "a", description: "First option" },
  { label: "Option B", value: "b", description: "Second option" },
  { label: "Option C", value: "c" },
];

describe("InlineSelect", () => {
  it("renders the title", () => {
    const { lastFrame } = render(
      <InlineSelect title="Pick one:" options={sampleOptions} onSelect={noop} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Pick one:"), "should show the title");
  });

  it("renders option labels", () => {
    const { lastFrame } = render(
      <InlineSelect title="Pick one:" options={sampleOptions} onSelect={noop} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Option A"), "should show first option label");
    assert.ok(frame.includes("Option B"), "should show second option label");
    assert.ok(frame.includes("Option C"), "should show third option label");
  });

  it("renders descriptions inline with labels", () => {
    const { lastFrame } = render(
      <InlineSelect title="Pick one:" options={sampleOptions} onSelect={noop} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("First option"), "should show first option description");
    assert.ok(frame.includes("Second option"), "should show second option description");
  });

  it("calls onSelect when an option is chosen", async () => {
    let selected: string | null = null;
    const { stdin } = render(
      <InlineSelect
        title="Pick one:"
        options={sampleOptions}
        onSelect={(v) => {
          selected = v;
        }}
      />,
    );
    // Allow component to mount and process
    await new Promise((r) => setTimeout(r, 50));
    // Press Enter to select the first (highlighted) option
    stdin.write("\r");
    await new Promise((r) => setTimeout(r, 50));
    assert.equal(selected, "a", "should call onSelect with first option value");
  });

  it("navigates with arrow down and selects second option", async () => {
    let selected: string | null = null;
    const { stdin } = render(
      <InlineSelect
        title="Pick:"
        options={sampleOptions}
        onSelect={(v) => {
          selected = v;
        }}
      />,
    );
    await new Promise((r) => setTimeout(r, 50));
    // Arrow down then enter
    stdin.write("\x1B[B"); // Down arrow
    await new Promise((r) => setTimeout(r, 50));
    stdin.write("\r");
    await new Promise((r) => setTimeout(r, 50));
    assert.equal(selected, "b", "should select second option after arrow down");
  });

  it("renders with single option", () => {
    const { lastFrame } = render(
      <InlineSelect
        title="Confirm:"
        options={[{ label: "Only choice", value: "only" }]}
        onSelect={noop}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Only choice"), "should show single option");
    assert.ok(frame.includes("Confirm:"), "should show title");
  });
});
