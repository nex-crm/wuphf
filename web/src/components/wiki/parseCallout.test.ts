import { describe, expect, it } from "vitest";

import { calloutRemarkPlugin, parseCalloutMarker } from "./parseCallout";

describe("parseCalloutMarker", () => {
  it("parses `[!note] Title` with body", () => {
    const result = parseCalloutMarker("[!note] Title\nbody");
    expect(result).toEqual({
      type: "note",
      title: "Title",
      body: "body",
      defaultOpen: undefined,
    });
  });

  it("parses `[!warning]+` as defaultOpen=true", () => {
    const result = parseCalloutMarker("[!warning]+\nbody");
    expect(result).toMatchObject({
      type: "warning",
      title: "",
      defaultOpen: true,
    });
  });

  it("parses `[!caution]-` as defaultOpen=false", () => {
    const result = parseCalloutMarker("[!caution]-\nbody");
    expect(result).toMatchObject({
      type: "caution",
      title: "",
      defaultOpen: false,
    });
  });

  it("returns null for a regular blockquote with no marker", () => {
    expect(parseCalloutMarker("just a quote\nwith two lines")).toBeNull();
  });

  it("returns null for empty input", () => {
    expect(parseCalloutMarker("")).toBeNull();
  });

  it("returns null for non-string input", () => {
    // @ts-expect-error intentional misuse
    expect(parseCalloutMarker(null)).toBeNull();
    // @ts-expect-error intentional misuse
    expect(parseCalloutMarker(undefined)).toBeNull();
  });

  it("falls back to `note` for unknown types", () => {
    const result = parseCalloutMarker("[!frobnicate] Something\nbody");
    expect(result).toMatchObject({
      type: "note",
      title: "Something",
    });
  });

  it("normalizes type casing to lowercase", () => {
    const result = parseCalloutMarker("[!WARNING] Heads up\nbody");
    expect(result).toMatchObject({
      type: "warning",
      title: "Heads up",
    });
  });

  it("handles `[!type]` with no title and no body", () => {
    const result = parseCalloutMarker("[!tip]");
    expect(result).toEqual({
      type: "tip",
      title: "",
      body: "",
      defaultOpen: undefined,
    });
  });

  it("handles `[!type]+` with a title on the same line", () => {
    const result = parseCalloutMarker("[!important]+ Read this\nbody line");
    expect(result).toMatchObject({
      type: "important",
      title: "Read this",
      body: "body line",
      defaultOpen: true,
    });
  });
});

describe("calloutRemarkPlugin", () => {
  function build() {
    return calloutRemarkPlugin()();
  }

  it("tags a callout-shaped blockquote with data attributes and strips the marker", () => {
    const transformer = build();
    const tree = {
      type: "root",
      children: [
        {
          type: "blockquote",
          children: [
            {
              type: "paragraph",
              children: [
                { type: "text", value: "[!warning]+ Heads up\nBody line" },
              ],
            },
          ],
        },
      ],
    };
    transformer(tree);
    const bq = tree.children[0] as {
      data?: { hProperties?: Record<string, string> };
      children: Array<{ type: string; children: Array<{ value: string }> }>;
    };
    expect(bq.data?.hProperties).toMatchObject({
      "data-callout": "true",
      "data-callout-type": "warning",
      "data-callout-title": "Heads up",
      "data-callout-fold": "open",
    });
    const firstParaText = bq.children[0].children[0].value;
    expect(firstParaText).toBe("Body line");
  });

  it("leaves a regular blockquote untouched", () => {
    const transformer = build();
    const tree = {
      type: "root",
      children: [
        {
          type: "blockquote",
          children: [
            {
              type: "paragraph",
              children: [{ type: "text", value: "just a quote" }],
            },
          ],
        },
      ],
    };
    transformer(tree);
    const bq = tree.children[0] as {
      data?: unknown;
      children: Array<{ type: string }>;
    };
    expect(bq.data).toBeUndefined();
    expect(bq.children).toHaveLength(1);
  });

  it("drops the first paragraph when only the marker line was present", () => {
    const transformer = build();
    const tree = {
      type: "root",
      children: [
        {
          type: "blockquote",
          children: [
            {
              type: "paragraph",
              children: [{ type: "text", value: "[!info]" }],
            },
            {
              type: "paragraph",
              children: [{ type: "text", value: "Body paragraph" }],
            },
          ],
        },
      ],
    };
    transformer(tree);
    const bq = tree.children[0] as {
      data?: { hProperties?: Record<string, string> };
      children: Array<{ type: string }>;
    };
    expect(bq.data?.hProperties).toMatchObject({
      "data-callout": "true",
      "data-callout-type": "info",
    });
    expect(bq.children).toHaveLength(1);
  });

  it("falls back to `note` for unknown types", () => {
    const transformer = build();
    const tree = {
      type: "root",
      children: [
        {
          type: "blockquote",
          children: [
            {
              type: "paragraph",
              children: [{ type: "text", value: "[!frobnicate]" }],
            },
          ],
        },
      ],
    };
    transformer(tree);
    const bq = tree.children[0] as {
      data?: { hProperties?: Record<string, string> };
    };
    expect(bq.data?.hProperties?.["data-callout-type"]).toBe("note");
  });
});
