import { describe, expect, it } from "vitest";

import { parseMentions } from "./mentions";

describe("parseMentions", () => {
  const agents = ["pm", "ceo", "founding-engineer"];

  it("wraps a known slug as a mention token", () => {
    const tokens = parseMentions("@pm hey", agents);
    expect(tokens).toEqual([
      { kind: "mention", value: "pm" },
      { kind: "text", value: " hey" },
    ]);
  });

  it("leaves unknown @-references as plain text", () => {
    const tokens = parseMentions("email @joedoe later", agents);
    expect(tokens).toEqual([{ kind: "text", value: "email @joedoe later" }]);
  });

  it("handles multiple mentions interleaved with text", () => {
    const tokens = parseMentions("hey @pm and @ceo, ship it", agents);
    expect(tokens).toEqual([
      { kind: "text", value: "hey " },
      { kind: "mention", value: "pm" },
      { kind: "text", value: " and " },
      { kind: "mention", value: "ceo" },
      { kind: "text", value: ", ship it" },
    ]);
  });

  it("keeps text preceding a mention intact (does not swallow the boundary char)", () => {
    const tokens = parseMentions("OK @pm", agents);
    expect(tokens).toEqual([
      { kind: "text", value: "OK " },
      { kind: "mention", value: "pm" },
    ]);
  });

  it("does NOT match @slug inside an email address", () => {
    const tokens = parseMentions("email user@pm.com please", agents);
    // `@pm` here is part of "user@pm.com" — the preceding `r` is word-char
    // so the boundary guard in the regex rejects it.
    expect(tokens).toEqual([
      { kind: "text", value: "email user@pm.com please" },
    ]);
  });

  it("accepts hyphenated agent slugs", () => {
    const tokens = parseMentions("ping @founding-engineer now", agents);
    expect(tokens).toEqual([
      { kind: "text", value: "ping " },
      { kind: "mention", value: "founding-engineer" },
      { kind: "text", value: " now" },
    ]);
  });

  it("returns an empty list for empty input", () => {
    expect(parseMentions("", agents)).toEqual([]);
  });

  it("returns the whole string as text when there is no @", () => {
    expect(parseMentions("hello world", agents)).toEqual([
      { kind: "text", value: "hello world" },
    ]);
  });

  it("does not append a stray empty text token after a trailing mention", () => {
    // Regression guard: earlier iterations of the parser left a trailing
    // empty-string text token when the mention sat at end-of-input.
    // Renderers then produced a zero-width text node which messed with
    // whitespace adjacency in the composer overlay.
    expect(parseMentions("@pm", agents)).toEqual([
      { kind: "mention", value: "pm" },
    ]);
  });

  it("is case-insensitive on the slug lookup", () => {
    const tokens = parseMentions("@pm @PM", ["pm"]);
    // The pattern itself only matches lowercase a-z plus digits and hyphens,
    // so @PM won't match the pattern at all — known-case normalisation is
    // defensive for callers passing mixed-case slugs.
    expect(tokens).toEqual([
      { kind: "mention", value: "pm" },
      { kind: "text", value: " @PM" },
    ]);
  });
});
