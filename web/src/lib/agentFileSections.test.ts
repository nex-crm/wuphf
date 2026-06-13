import { describe, expect, it } from "vitest";

import {
  buildEditorBlocks,
  parseAgentFile,
  schemaForFile,
  serializeAgentFile,
} from "./agentFileSections";

const SOUL = `# SOUL — @growth

Some intro prose.

## Who you are
Relentless about pipeline.

## Values
- Bias to action
- Reuse first

## Voice
Direct, numbers-first.

## Boundaries
Stay in your lane.
`;

describe("parseAgentFile", () => {
  it("splits title, preamble, and sections", () => {
    const parsed = parseAgentFile(SOUL);
    expect(parsed.title).toBe("SOUL — @growth");
    expect(parsed.preamble).toBe("Some intro prose.");
    expect(parsed.sections.map((s) => s.heading)).toEqual([
      "Who you are",
      "Values",
      "Voice",
      "Boundaries",
    ]);
    expect(parsed.sections[1].body).toBe("- Bias to action\n- Reuse first");
  });

  it("handles a flat file with no sections (IDENTITY-style)", () => {
    const parsed = parseAgentFile(
      "# IDENTITY — @growth\n\n- Name: Growth Lead\n- Role: demand gen\n",
    );
    expect(parsed.title).toBe("IDENTITY — @growth");
    expect(parsed.sections).toHaveLength(0);
    expect(parsed.preamble).toContain("- Name: Growth Lead");
  });
});

describe("serializeAgentFile round-trip", () => {
  it("re-serializes a parsed file to equivalent markdown", () => {
    const parsed = parseAgentFile(SOUL);
    const out = serializeAgentFile(parsed);
    // Re-parsing the output yields the same structure.
    const reparsed = parseAgentFile(out);
    expect(reparsed.title).toBe(parsed.title);
    expect(reparsed.preamble).toBe(parsed.preamble);
    expect(reparsed.sections).toEqual(parsed.sections);
  });

  it("drops empty sections but keeps headings with content", () => {
    const out = serializeAgentFile({
      title: "SOUL — @x",
      preamble: "",
      sections: [
        { heading: "Who you are", body: "A pragmatic engineer." },
        { heading: "Values", body: "" },
      ],
    });
    expect(out).toContain("## Who you are\nA pragmatic engineer.");
    // An empty section still emits its heading so the structure is preserved.
    expect(out).toContain("## Values");
  });
});

describe("buildEditorBlocks", () => {
  it("orders schema sections first, fills missing ones empty, preserves extras", () => {
    // File missing "Voice" but carrying an unknown "Extra" section.
    const parsed = parseAgentFile(
      "# SOUL — @x\n\n## Who you are\nHi.\n\n## Extra\nkeep me\n",
    );
    const blocks = buildEditorBlocks(parsed, schemaForFile("SOUL"));
    const headings = blocks.map((b) => b.heading);
    // Schema order first (Who you are, Values, Voice, Boundaries), then extras.
    expect(headings).toEqual([
      "Who you are",
      "Values",
      "Voice",
      "Boundaries",
      "Extra",
    ]);
    const voice = blocks.find((b) => b.heading === "Voice");
    expect(voice?.body).toBe("");
    expect(voice?.fromSchema).toBe(true);
    const extra = blocks.find((b) => b.heading === "Extra");
    expect(extra?.body).toBe("keep me");
    expect(extra?.fromSchema).toBe(false);
  });

  it("returns no schema blocks for a file type without a schema", () => {
    const parsed = parseAgentFile("# IDENTITY — @x\n\n- Name: X\n");
    expect(buildEditorBlocks(parsed, schemaForFile("IDENTITY"))).toEqual([]);
  });
});
