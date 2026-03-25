import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import React from "react";
import { render, cleanup } from "ink-testing-library";
import { InlineConfirm } from "../../../src/tui/components/inline-confirm.js";

function strip(s: string): string {
  return s.replace(/\x1b\[[0-9;]*m/g, "");
}

afterEach(() => {
  cleanup();
});

const noop = () => {};

describe("InlineConfirm", () => {
  it("renders the question", () => {
    const { lastFrame } = render(
      <InlineConfirm question="Are you sure?" onConfirm={noop} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Are you sure?"), "should show the question text");
  });

  it("calls onConfirm with true when user presses y", async () => {
    let result: boolean | null = null;
    const { stdin } = render(
      <InlineConfirm
        question="Delete this?"
        onConfirm={(confirmed) => {
          result = confirmed;
        }}
      />,
    );
    await new Promise((r) => setTimeout(r, 50));
    // ConfirmInput fires onConfirm immediately on 'y' key
    stdin.write("y");
    await new Promise((r) => setTimeout(r, 50));
    assert.equal(result, true, "should call onConfirm with true");
  });

  it("calls onConfirm with false when user presses n", async () => {
    let result: boolean | null = null;
    const { stdin } = render(
      <InlineConfirm
        question="Delete this?"
        onConfirm={(confirmed) => {
          result = confirmed;
        }}
      />,
    );
    await new Promise((r) => setTimeout(r, 50));
    // ConfirmInput fires onCancel immediately on 'n' key
    stdin.write("n");
    await new Promise((r) => setTimeout(r, 50));
    assert.equal(result, false, "should call onConfirm with false");
  });

  it("renders with a long question", () => {
    const longQuestion = "Do you really want to permanently delete all records from this workspace?";
    const { lastFrame } = render(
      <InlineConfirm question={longQuestion} onConfirm={noop} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("permanently delete"), "should show long question text");
  });
});
