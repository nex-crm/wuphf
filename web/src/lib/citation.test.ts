import { describe, expect, it } from "vitest";

import { citationRemarkPlugin, extractCitationIds } from "./citation";

describe("extractCitationIds", () => {
  it("returns ids in first-appearance order, de-duplicated", () => {
    const md =
      "A claim.^[task-wup-12] Another.^[decision-rrf-1] Repeat.^[task-wup-12]";
    expect(extractCitationIds(md)).toEqual(["task-wup-12", "decision-rrf-1"]);
  });

  it("ignores bracketed text that is not a citation marker", () => {
    // GFM footnote refs (`[^1]`) put the caret INSIDE the brackets.
    expect(extractCitationIds("See note[^1] and [a link](x).")).toEqual([]);
  });

  it("rejects markers containing whitespace", () => {
    expect(extractCitationIds("prose ^[not an id] more")).toEqual([]);
  });
});

describe("citationRemarkPlugin", () => {
  // Drive the transformer directly over a minimal mdast tree so the test does
  // not depend on the full react-markdown pipeline.
  it("splits a text node into text + citation link nodes", () => {
    const tree = {
      type: "root",
      children: [
        {
          type: "paragraph",
          children: [{ type: "text", value: "Claim.^[task-wup-12] end" }],
        },
      ],
    };
    const transformer = citationRemarkPlugin()();
    transformer(tree);

    const para = tree.children[0] as {
      children: Array<Record<string, unknown>>;
    };
    expect(para.children).toHaveLength(3);
    expect(para.children[0]).toMatchObject({ type: "text", value: "Claim." });
    expect(para.children[1]).toMatchObject({
      type: "link",
      data: {
        hProperties: {
          "data-citation": "true",
          "data-source-id": "task-wup-12",
        },
      },
    });
    expect(para.children[2]).toMatchObject({ type: "text", value: " end" });
  });
});
