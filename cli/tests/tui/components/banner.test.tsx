import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import React from "react";
import { render, cleanup } from "ink-testing-library";
import {
  Banner,
  generateLine,
  buildBrandLine,
  generateBannerFrame,
} from "../../../src/tui/components/banner.js";

// Strip ANSI escape sequences for assertion matching
function strip(s: string): string {
  return s.replace(/\x1b\[[0-9;]*m/g, "");
}

afterEach(() => {
  cleanup();
});

// ─── generateLine ───

describe("generateLine", () => {
  it("produces a string of the requested width", () => {
    for (const w of [10, 40, 80, 120]) {
      const line = generateLine(w, 42);
      assert.equal(line.length, w, `expected width ${w}, got ${line.length}`);
    }
  });

  it("is deterministic with the same seed", () => {
    const a = generateLine(60, 123);
    const b = generateLine(60, 123);
    assert.equal(a, b, "same seed should produce identical output");
  });

  it("varies with different seeds", () => {
    const a = generateLine(60, 100);
    const b = generateLine(60, 200);
    assert.notEqual(a, b, "different seeds should produce different output");
  });

  it("contains expected character types", () => {
    // Generate a long line to ensure statistical presence of dots/connectors
    const line = generateLine(200, 7);
    assert.ok(line.includes("─"), "should contain horizontal dashes");
    // At least one non-dash character in a 200-wide line
    const nonDash = [...line].filter((c) => c !== "─");
    assert.ok(nonDash.length > 0, "should contain dots or connectors");
  });

  it("handles width of 0", () => {
    const line = generateLine(0, 1);
    assert.equal(line.length, 0);
  });

  it("handles width of 1", () => {
    const line = generateLine(1, 1);
    assert.equal(line.length, 1);
  });
});

// ─── buildBrandLine ───

describe("buildBrandLine", () => {
  it("includes the brand text 'wuphf'", () => {
    const segments = buildBrandLine(80, 42);
    const brandSeg = segments.find((s) => s.type === "brand");
    assert.ok(brandSeg, "should have a brand segment");
    assert.equal(brandSeg.text, "wuphf");
  });

  it("includes tagline in muted segment", () => {
    const segments = buildBrandLine(80, 42);
    const mutedTexts = segments
      .filter((s) => s.type === "muted")
      .map((s) => s.text)
      .join("");
    assert.ok(
      mutedTexts.includes("powered by wuphf.ai"),
      "should include tagline",
    );
  });

  it("total text width equals requested width", () => {
    for (const w of [40, 60, 80, 100, 120]) {
      const segments = buildBrandLine(w, 42);
      const totalWidth = segments.reduce((sum, s) => sum + s.text.length, 0);
      assert.equal(totalWidth, w, `brand line width should be ${w}, got ${totalWidth}`);
    }
  });

  it("degrades gracefully at narrow widths", () => {
    const segments = buildBrandLine(10, 42);
    const full = segments.map((s) => s.text).join("");
    assert.ok(full.includes("wuphf"), "narrow banner still shows brand");
    assert.equal(full.length, 10);
  });
});

// ─── generateBannerFrame ───

describe("generateBannerFrame", () => {
  it("produces 6 lines", () => {
    const frame = generateBannerFrame(80, 42);
    assert.equal(frame.lines.length, 6, "banner should be 6 lines tall");
  });

  it("line 3 (index 2) is the brand line", () => {
    const frame = generateBannerFrame(80, 42);
    const brandLine = frame.lines[2];
    const hasBrand = brandLine.segments.some((s) => s.type === "brand");
    assert.ok(hasBrand, "third line should contain brand segment");
  });

  it("is deterministic with same seed", () => {
    const a = generateBannerFrame(80, 42);
    const b = generateBannerFrame(80, 42);
    for (let i = 0; i < 6; i++) {
      const textA = a.lines[i].segments.map((s) => s.text).join("");
      const textB = b.lines[i].segments.map((s) => s.text).join("");
      assert.equal(textA, textB, `line ${i} should match with same seed`);
    }
  });

  it("varies with different seeds", () => {
    const a = generateBannerFrame(80, 1);
    const b = generateBannerFrame(80, 2);
    const textA = a.lines[0].segments.map((s) => s.text).join("");
    const textB = b.lines[0].segments.map((s) => s.text).join("");
    assert.notEqual(textA, textB, "different seeds produce different frames");
  });
});

// ─── Banner component ───

describe("Banner", () => {
  it("renders 6 lines of output", () => {
    const { lastFrame } = render(<Banner width={80} interval={0} />);
    const frame = lastFrame() ?? "";
    const lines = frame.split("\n").filter((l) => l.length > 0);
    assert.equal(lines.length, 6, `expected 6 lines, got ${lines.length}`);
  });

  it("contains 'wuphf' brand text", () => {
    const { lastFrame } = render(<Banner width={80} interval={0} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("wuphf"), "should render brand name");
  });

  it("contains tagline", () => {
    const { lastFrame } = render(<Banner width={80} interval={0} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(
      frame.includes("powered by wuphf.ai"),
      "should render tagline",
    );
  });

  it("respects custom width", () => {
    const { lastFrame } = render(<Banner width={50} interval={0} />);
    const frame = strip(lastFrame() ?? "");
    const lines = frame.split("\n").filter((l) => l.length > 0);
    // Each line should be at most 50 characters
    for (const line of lines) {
      assert.ok(
        line.length <= 50,
        `line width ${line.length} exceeds 50: "${line}"`,
      );
    }
  });

  it("caps width at 120", () => {
    const { lastFrame } = render(<Banner width={200} interval={0} />);
    const frame = strip(lastFrame() ?? "");
    const lines = frame.split("\n").filter((l) => l.length > 0);
    for (const line of lines) {
      assert.ok(
        line.length <= 120,
        `line width ${line.length} exceeds cap of 120`,
      );
    }
  });

  it("renders at narrow width without crashing", () => {
    const { lastFrame } = render(<Banner width={15} interval={0} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("wuphf"), "narrow banner still shows brand");
  });
});
