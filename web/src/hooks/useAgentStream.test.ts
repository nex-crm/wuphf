import { describe, expect, it } from "vitest";

import { appendStreamLine, type StreamLine } from "./useAgentStream";

describe("appendStreamLine", () => {
  it("starts a new raw line when the buffer is empty", () => {
    const { lines, usedId } = appendStreamLine([], "hello", undefined, 1);
    expect(lines).toHaveLength(1);
    expect(lines[0]).toMatchObject({ id: 1, data: "hello", parsed: undefined });
    expect(usedId).toBe(true);
  });

  it("coalesces consecutive raw chunks into a single line", () => {
    // Regression: the local-LLM path streams ~5ms per chunk; without
    // coalescing every chunk renders as its own <div> in the Live
    // Output panel and the user sees "one word per line". This
    // assertion ensures consecutive raw events merge into ONE line
    // with concatenated text and the original line id.
    let lines: StreamLine[] = [];
    let nextId = 1;
    for (const chunk of ["I'm ", "the ", "planner — ", "what next?"]) {
      const result = appendStreamLine(lines, chunk, undefined, nextId);
      lines = result.lines;
      if (result.usedId) nextId += 1;
    }
    expect(lines).toHaveLength(1);
    expect(lines[0].data).toBe("I'm the planner — what next?");
    expect(lines[0].id).toBe(1);
    expect(nextId).toBe(2); // only the FIRST chunk consumed an id
  });

  it("starts a new line when the previous event was structured", () => {
    const initial: StreamLine[] = [
      {
        id: 1,
        data: '{"type":"mcp_tool_event"}',
        parsed: { type: "mcp_tool_event" },
      },
    ];
    const { lines, usedId } = appendStreamLine(
      initial,
      "raw text",
      undefined,
      2,
    );
    expect(lines).toHaveLength(2);
    expect(lines[1]).toMatchObject({ id: 2, data: "raw text" });
    expect(usedId).toBe(true);
  });

  it("never merges into a structured line", () => {
    // The defensive case — make sure we don't accidentally append a
    // raw chunk's text onto a parsed JSON line and break downstream
    // rendering that depends on `data` being valid JSON.
    const initial: StreamLine[] = [
      {
        id: 1,
        data: '{"phase":"call","tool":"team_broadcast"}',
        parsed: { phase: "call", tool: "team_broadcast" },
      },
    ];
    const { lines } = appendStreamLine(initial, "extra", undefined, 2);
    expect(lines[0].data).toBe('{"phase":"call","tool":"team_broadcast"}');
    expect(lines[1].data).toBe("extra");
  });

  it("structured event after raw still starts its own line", () => {
    // Mirror flow: model streams raw text, then emits a tool_event
    // on tool dispatch. The structured event must NOT merge into
    // the raw line.
    const initial: StreamLine[] = [
      { id: 1, data: "Hello world", parsed: undefined },
    ];
    const { lines, usedId } = appendStreamLine(
      initial,
      '{"type":"mcp_tool_event","tool":"team_broadcast"}',
      { type: "mcp_tool_event", tool: "team_broadcast" },
      2,
    );
    expect(lines).toHaveLength(2);
    expect(lines[0].data).toBe("Hello world");
    expect(lines[1].parsed).toBeDefined();
    expect(usedId).toBe(true);
  });

  it("trims to MAX_LINES (50) on overflow", () => {
    // Each entry alternates structured/raw so coalescing doesn't
    // collapse them — we want the 50-cap behavior tested directly.
    let lines: StreamLine[] = [];
    let nextId = 1;
    for (let i = 0; i < 60; i++) {
      const isStructured = i % 2 === 0;
      const result = appendStreamLine(
        lines,
        isStructured ? `{"i":${i}}` : `r${i}`,
        isStructured ? { i } : undefined,
        nextId,
      );
      lines = result.lines;
      if (result.usedId) nextId += 1;
    }
    expect(lines.length).toBeLessThanOrEqual(50);
  });
});
