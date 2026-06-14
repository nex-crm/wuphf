import { describe, expect, it } from "vitest";

import { ENTITY_ARTICLE_FIXTURE } from "./__fixtures__/entityArticleFixture";
import {
  excerptFromMarkdown,
  makeWikilinkResolver,
  prepareArticleMarkdown,
} from "./articleContent";

describe("prepareArticleMarkdown — B2 generated entity article", () => {
  const prepared = prepareArticleMarkdown(ENTITY_ARTICLE_FIXTURE);

  it("detects the generated-article marker (hatnote trigger)", () => {
    expect(prepared.generated).toBe(true);
  });

  it("lifts the ## Summary definition list out as infobox rows", () => {
    expect(prepared.infobox).not.toBeNull();
    const rows = prepared.infobox ?? [];
    expect(rows.map((r) => r.term)).toEqual([
      "Kind",
      "Article",
      "Facts on record",
      "Tasks",
      "Artifacts",
      "Associated",
    ]);
    expect(rows[0].value).toBe("company");
    expect(rows[5].value).toBe("[[people/eng]]");
    // The Summary section is removed from the rendered body.
    expect(prepared.markdown).not.toContain("## Summary");
    expect(prepared.markdown).not.toContain("Facts on record");
  });

  it("strips the lead H1 (the chrome renders the title)", () => {
    expect(prepared.markdown).not.toContain("# Acme Corp");
    // The bold lead survives.
    expect(prepared.markdown).toContain("**Acme Corp** is a company");
  });

  it("drops the literal ## References heading but keeps footnote defs", () => {
    expect(prepared.hasFootnotes).toBe(true);
    expect(prepared.markdown).not.toContain("## References");
    expect(prepared.markdown).toContain(
      "[^1]: Task TASK-3 — artifact: [team/playbooks/acme-renewal.md]",
    );
  });

  it("keeps themed sections and wikilinks intact", () => {
    expect(prepared.markdown).toContain("## Work history");
    expect(prepared.markdown).toContain("## Observations");
    expect(prepared.markdown).toContain("## Associated");
    expect(prepared.markdown).toContain("[[people/eng]]");
  });
});

describe("prepareArticleMarkdown — non-generated articles", () => {
  it("returns no infobox and no hatnote flag for a plain article", () => {
    const prepared = prepareArticleMarkdown(
      "# Notes\n\nJust some prose.\n\n## Summary\n\nA prose summary, not a definition list.\n",
    );
    expect(prepared.generated).toBe(false);
    expect(prepared.infobox).toBeNull();
    // Prose Summary sections stay in the body.
    expect(prepared.markdown).toContain("## Summary");
    expect(prepared.markdown).toContain("A prose summary");
  });

  it("keeps ## References when it holds prose rather than footnote defs", () => {
    const prepared = prepareArticleMarkdown(
      "Intro.[^1]\n\n## References\n\nSee the runbook.\n\n[^1]: a footnote\n",
    );
    expect(prepared.markdown).toContain("## References");
  });

  it("does not strip an H1 that appears after body prose", () => {
    const prepared = prepareArticleMarkdown("Lead first.\n\n# Late heading\n");
    expect(prepared.markdown).toContain("# Late heading");
  });
});

describe("makeWikilinkResolver", () => {
  const resolver = makeWikilinkResolver([
    "team/people/eng.md",
    "team/companies/acme-corp.md",
    "team/playbooks/renewal.md",
  ]);

  it("resolves kind/slug wikilinks against canonical catalog paths", () => {
    expect(resolver("people/eng")).toBe(true);
    expect(resolver("companies/acme-corp")).toBe(true);
    expect(resolver("companies/unknown")).toBe(false);
  });

  it("resolves bare slugs against any leaf (fetchArticle fan-out)", () => {
    expect(resolver("eng")).toBe(true);
    expect(resolver("acme-corp")).toBe(true);
    expect(resolver("missing")).toBe(false);
  });

  it("accepts canonical team/-prefixed .md paths too", () => {
    expect(resolver("team/people/eng.md")).toBe(true);
  });
});

describe("excerptFromMarkdown", () => {
  it("returns the first prose words with markdown flattened", () => {
    const excerpt = excerptFromMarkdown(ENTITY_ARTICLE_FIXTURE, 12);
    expect(excerpt).toBe(
      "Acme Corp is a company in the team knowledge graph, with 2…",
    );
  });

  it("skips comments, headings, and footnote definitions", () => {
    const excerpt = excerptFromMarkdown(
      "<!-- marker -->\n# Title\n\n[^1]: def\n\nReal prose [[people/eng|Eng]] here.\n",
      10,
    );
    expect(excerpt).toBe("Real prose Eng here.");
  });
});
