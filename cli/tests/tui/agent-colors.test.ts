import { describe, it, beforeEach } from "node:test";
import assert from "node:assert/strict";
import {
  getAgentColor,
  getAllAgentColors,
  resetAgentColors,
} from "../../src/tui/agent-colors.js";

describe("agent-colors", () => {
  beforeEach(() => {
    resetAgentColors();
  });

  it("assigns a color on first encounter", () => {
    const color = getAgentColor("seo-analyst");
    assert.ok(color, "should return a color string");
    assert.ok(
      ["cyan", "green", "yellow", "magenta", "blue", "red"].includes(color),
      `should be one of the palette colors, got "${color}"`,
    );
  });

  it("returns the same color for the same slug", () => {
    const first = getAgentColor("lead-gen");
    const second = getAgentColor("lead-gen");
    assert.equal(first, second, "same slug should always return the same color");
  });

  it("assigns different colors to different slugs", () => {
    const a = getAgentColor("agent-a");
    const b = getAgentColor("agent-b");
    assert.notEqual(a, b, "different slugs should get different colors (within palette size)");
  });

  it("cycles through all six palette colors", () => {
    const slugs = ["a", "b", "c", "d", "e", "f"];
    const colors = slugs.map((s) => getAgentColor(s));

    // All six should be unique
    const unique = new Set(colors);
    assert.equal(unique.size, 6, "should use all 6 palette colors for 6 different slugs");
  });

  it("wraps around after exhausting the palette", () => {
    const slugs = ["a", "b", "c", "d", "e", "f", "g"];
    const colors = slugs.map((s) => getAgentColor(s));

    // The 7th should wrap to the same color as the 1st
    assert.equal(
      colors[6],
      colors[0],
      "7th slug should wrap around to the first color in the palette",
    );
  });

  it("getAllAgentColors returns the current map", () => {
    getAgentColor("alpha");
    getAgentColor("beta");
    const map = getAllAgentColors();
    assert.equal(map.size, 2, "should have 2 entries");
    assert.ok(map.has("alpha"), "should contain alpha");
    assert.ok(map.has("beta"), "should contain beta");
  });

  it("resetAgentColors clears all assignments", () => {
    const before = getAgentColor("test");
    resetAgentColors();
    // After reset, the first slug gets the first palette color again
    const after = getAgentColor("different-slug");
    assert.equal(before, after, "after reset, first assignment should start from beginning");
    assert.equal(getAllAgentColors().size, 1, "should only have 1 entry after reset");
  });
});
