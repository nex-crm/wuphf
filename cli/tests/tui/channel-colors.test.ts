import { describe, it, beforeEach } from "node:test";
import assert from "node:assert/strict";
import {
  getChannelColor,
  resetChannelColors,
} from "../../src/tui/channel-colors.js";

describe("channel-colors", () => {
  beforeEach(() => {
    resetChannelColors();
  });

  it("assigns well-known colors for standard channels", () => {
    assert.equal(getChannelColor("general"), "cyan");
    assert.equal(getChannelColor("leads"), "green");
    assert.equal(getChannelColor("seo"), "yellow");
    assert.equal(getChannelColor("support"), "magenta");
  });

  it("is case-insensitive for well-known channels", () => {
    assert.equal(getChannelColor("General"), "cyan");
    assert.equal(getChannelColor("LEADS"), "green");
  });

  it("returns stable color for same channel name", () => {
    const first = getChannelColor("custom-channel");
    const second = getChannelColor("custom-channel");
    assert.equal(first, second, "same name should always return same color");
  });

  it("assigns different colors to different unknown channels", () => {
    const a = getChannelColor("alpha");
    const b = getChannelColor("beta");
    assert.notEqual(a, b, "different names should get different colors");
  });

  it("cycles through palette for unknown channels", () => {
    const names = ["x1", "x2", "x3", "x4", "x5", "x6"];
    const colors = names.map((n) => getChannelColor(n));
    const unique = new Set(colors);
    assert.equal(unique.size, 6, "should use all 6 palette colors");
  });

  it("wraps around after exhausting palette", () => {
    const names = ["x1", "x2", "x3", "x4", "x5", "x6", "x7"];
    const colors = names.map((n) => getChannelColor(n));
    assert.equal(colors[6], colors[0], "7th should wrap to first color");
  });

  it("resetChannelColors clears all assignments", () => {
    getChannelColor("custom");
    resetChannelColors();
    // After reset, well-known channels still work
    assert.equal(getChannelColor("general"), "cyan");
  });
});
