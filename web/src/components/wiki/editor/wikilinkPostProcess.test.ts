import { describe, expect, it } from "vitest";

import { postProcessWikilinks } from "./wikilinkPostProcess";

describe("postProcessWikilinks", () => {
  it("rewrites a bare wikilink whose display matches the slug", () => {
    expect(postProcessWikilinks("See [alex](#/wiki/alex) for details.")).toBe(
      "See [[alex]] for details.",
    );
  });

  it("rewrites a wikilink with a distinct display label", () => {
    expect(
      postProcessWikilinks("See [Alex Chen](#/wiki/people/alex) for details."),
    ).toBe("See [[people/alex|Alex Chen]] for details.");
  });

  it("decodes percent-encoded slugs", () => {
    expect(
      postProcessWikilinks("Owner: [Alex Chen](#/wiki/Alex%20Chen)."),
    ).toBe("Owner: [[Alex Chen]].");
  });

  it("rewrites every wikilink in a paragraph", () => {
    expect(
      postProcessWikilinks(
        "[alex](#/wiki/alex) works with [sarah](#/wiki/sarah) on [project-x](#/wiki/project-x).",
      ),
    ).toBe("[[alex]] works with [[sarah]] on [[project-x]].");
  });

  it("preserves a trailing title attribute by dropping it", () => {
    // Title metadata has no wikilink equivalent; emit canonical form.
    expect(
      postProcessWikilinks('See [alex](#/wiki/alex "person page") today.'),
    ).toBe("See [[alex]] today.");
  });

  it("leaves standard external links untouched", () => {
    expect(
      postProcessWikilinks("See [the docs](https://example.com) for details."),
    ).toBe("See [the docs](https://example.com) for details.");
  });

  it("leaves anchor links untouched when not /wiki/", () => {
    expect(postProcessWikilinks("Jump to [intro](#intro).")).toBe(
      "Jump to [intro](#intro).",
    );
  });

  it("leaves a malformed percent-escape as a standard link rather than corrupt wikilink", () => {
    const malformed = "See [alex](#/wiki/bad%E0) here.";
    expect(postProcessWikilinks(malformed)).toBe(malformed);
  });

  it("rewrites wikilinks inside list items", () => {
    expect(
      postProcessWikilinks(
        "- Owner: [alex](#/wiki/alex)\n- See: [docs](https://x.com)\n",
      ),
    ).toBe("- Owner: [[alex]]\n- See: [docs](https://x.com)\n");
  });

  it("rewrites wikilinks inside table cells", () => {
    const md =
      "| Owner | Status |\n| --- | --- |\n| [alex](#/wiki/alex) | Active |\n";
    expect(postProcessWikilinks(md)).toBe(
      "| Owner | Status |\n| --- | --- |\n| [[alex]] | Active |\n",
    );
  });

  it("does not rewrite wikilink-shaped text inside an inline code span", () => {
    const md = "Use the syntax `[alex](#/wiki/alex)` to link.";
    expect(postProcessWikilinks(md)).toBe(md);
  });

  it("does not rewrite wikilink-shaped text inside a fenced code block", () => {
    const md = "```markdown\n[alex](#/wiki/alex)\n```\n";
    expect(postProcessWikilinks(md)).toBe(md);
  });

  it("rewrites prose wikilinks even when a code block is present elsewhere", () => {
    const md =
      "See [alex](#/wiki/alex) below.\n\n```markdown\n[bob](#/wiki/bob)\n```\n";
    expect(postProcessWikilinks(md)).toBe(
      "See [[alex]] below.\n\n```markdown\n[bob](#/wiki/bob)\n```\n",
    );
  });

  it("rewrites prose wikilinks separated by an inline code span", () => {
    const md =
      "[alex](#/wiki/alex) wrote `[bob](#/wiki/bob)` then [carol](#/wiki/carol).";
    expect(postProcessWikilinks(md)).toBe(
      "[[alex]] wrote `[bob](#/wiki/bob)` then [[carol]].",
    );
  });
});
