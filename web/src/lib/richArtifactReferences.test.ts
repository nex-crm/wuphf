import { describe, expect, it } from "vitest";

import {
  extractRichArtifactIds,
  stripStandaloneRichArtifactReferenceLines,
} from "./richArtifactReferences";

describe("rich artifact references", () => {
  it("extracts explicit visual artifact markers in first-seen order", () => {
    const content = [
      "I made the HTML companion.",
      "visual-artifact:ra_0123456789abcdef",
      "rich artifact id: ra_fedcba9876543210",
      "visual-artifact:ra_0123456789abcdef",
    ].join("\n");

    expect(extractRichArtifactIds(content)).toEqual([
      "ra_0123456789abcdef",
      "ra_fedcba9876543210",
    ]);
  });

  it("extracts notebook and wiki visual artifact paths", () => {
    const content =
      "[open](/notebook/visual-artifacts/ra_aaaaaaaaaaaaaaaa) and wiki/visual-artifacts/ra_bbbbbbbbbbbbbbbb.html";

    expect(extractRichArtifactIds(content)).toEqual([
      "ra_aaaaaaaaaaaaaaaa",
      "ra_bbbbbbbbbbbbbbbb",
    ]);
  });

  it("does not extract references from fenced code blocks", () => {
    const content = [
      "```",
      "visual-artifact:ra_aaaaaaaaaaaaaaaa",
      "```",
      "visual-artifact:ra_bbbbbbbbbbbbbbbb",
    ].join("\n");

    expect(extractRichArtifactIds(content)).toEqual(["ra_bbbbbbbbbbbbbbbb"]);
  });

  it("strips standalone marker lines but preserves prose", () => {
    const content = [
      "Here is the review.",
      "",
      "- visual artifact: ra_0123456789abcdef",
      "",
      "Use it for the implementation pass.",
    ].join("\n");

    expect(stripStandaloneRichArtifactReferenceLines(content)).toBe(
      "Here is the review.\n\nUse it for the implementation pass.",
    );
  });

  it("does not strip a sentence that merely links to an artifact", () => {
    const content =
      "Open [the artifact](/notebook/visual-artifacts/ra_0123456789abcdef) when ready.";

    expect(stripStandaloneRichArtifactReferenceLines(content)).toBe(content);
  });
});
