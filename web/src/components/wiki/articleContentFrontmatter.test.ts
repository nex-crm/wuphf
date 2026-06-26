import { describe, expect, it } from "vitest";

import { prepareArticleMarkdown, splitFrontmatter } from "./articleContent";

describe("splitFrontmatter", () => {
  it("strips a leading YAML block and parses top-level scalars", () => {
    const content = `---
title: Reciprocal Rank Fusion
kind: concept
compiled: true
sources:
  - decision-rrf-1
---

Body starts here.`;
    const { frontmatter, body } = splitFrontmatter(content);
    expect(frontmatter).toMatchObject({
      title: "Reciprocal Rank Fusion",
      kind: "concept",
      compiled: "true",
    });
    // The array opener `sources:` is skipped (no scalar value).
    expect(frontmatter?.sources).toBeUndefined();
    expect(body).toBe("Body starts here.");
  });

  it("leaves a body with no frontmatter untouched", () => {
    const content = "# Title\n\nProse with a --- thematic break later.";
    const { frontmatter, body } = splitFrontmatter(content);
    expect(frontmatter).toBeNull();
    expect(body).toBe(content);
  });

  it("treats a block with no closing fence as no frontmatter", () => {
    const content = "---\ntitle: X\n# Heading without a close";
    expect(splitFrontmatter(content).frontmatter).toBeNull();
  });
});

describe("prepareArticleMarkdown — compiled detection", () => {
  it("flags compiled articles and strips frontmatter + lead H1", () => {
    const content = `---
title: RRF
compiled: true
---

# RRF

Lead paragraph.^[task-1]

## Section

More.`;
    const prepared = prepareArticleMarkdown(content);
    expect(prepared.compiled).toBe(true);
    expect(prepared.markdown).not.toContain("compiled:");
    expect(prepared.markdown).not.toContain("# RRF");
    expect(prepared.markdown).toContain("Lead paragraph.^[task-1]");
  });

  it("leaves non-compiled articles unflagged", () => {
    const prepared = prepareArticleMarkdown("# Hand-written\n\nProse.");
    expect(prepared.compiled).toBe(false);
  });
});
