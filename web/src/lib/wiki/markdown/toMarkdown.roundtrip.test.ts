import { describe, expect, it } from "vitest";

import { wikiMarkdownToHtml } from "./toHtml";
import { wikiHtmlToMarkdown } from "./toMarkdown";

/**
 * Normalise whitespace-only differences before comparing round-tripped
 * markdown:
 *   - collapse the padding turndown inserts after a list marker
 *     (`-   item` / `1.  item`) back to a single space,
 *   - strip trailing whitespace from each line,
 *   - strip leading/trailing blank lines from the whole document.
 *
 * It never touches inner content, so a dropped word or mangled token still
 * fails the assertion.
 */
function normalize(markdown: string): string {
  return markdown
    .split("\n")
    .map((line) =>
      line.replace(/^(\s*(?:[-*+]|\d+\.))\s+(\S)/, "$1 $2").replace(/\s+$/, ""),
    )
    .join("\n")
    .replace(/^\n+/, "")
    .replace(/\n+$/, "");
}

function roundTrip(markdown: string): string {
  return wikiHtmlToMarkdown(wikiMarkdownToHtml(markdown));
}

interface RoundTripCase {
  name: string;
  markdown: string;
}

const cases: RoundTripCase[] = [
  { name: "headings", markdown: "# Title\n\n## Section\n\n### Subsection\n" },
  {
    name: "bold and italic",
    markdown: "Some **bold** and *italic* and ~~struck~~ text.\n",
  },
  { name: "bullet list", markdown: "- one\n- two\n- three\n" },
  { name: "ordered list", markdown: "1. first\n2. second\n3. third\n" },
  { name: "task list", markdown: "- [ ] todo\n- [x] done\n" },
  {
    name: "GFM table",
    markdown: "| Name | Role |\n| --- | --- |\n| Nazz | Founder |\n",
  },
  {
    name: "js code block",
    markdown: "```js\nconst answer = 42;\nconsole.warn(answer);\n```\n",
  },
  {
    name: "fact fenced block",
    markdown:
      "```fact\nsubject: Nex\npredicate: is\nobject: a context graph\n```\n",
  },
  {
    name: "wikilink without display",
    markdown: "See [[people/nazz]] for details.\n",
  },
  {
    name: "wikilink with display",
    markdown: "Read the [[x|Display]] page.\n",
  },
  {
    name: "footnote reference and definition",
    markdown: "See [^1] for more.\n\n[^1]: Title - http://example.com\n",
  },
  {
    name: "blockquote callout",
    markdown: "> [!note] Keep this in the wiki.\n",
  },
  {
    name: "combined article",
    markdown:
      "# Onboarding\n\n" +
      "Welcome. Read [[people/nazz]] first.\n\n" +
      "- [x] Skim the wiki\n- [ ] Say hi\n\n" +
      "```decision\ntitle: Use markdown\ndate: 2026-06-02\nrationale: round-trips cleanly\n```\n\n" +
      "Backed by research [^1].\n\n" +
      "[^1]: Source - http://example.com\n",
  },
];

describe("wiki markdown round-trip", () => {
  for (const { name, markdown } of cases) {
    it(`preserves ${name}`, () => {
      // Arrange + Act
      const result = roundTrip(markdown);

      // Assert
      expect(normalize(result)).toBe(normalize(markdown));
    });
  }

  it("keeps a footnote citation byte-for-byte", () => {
    // The literal `[^1]` reference and `[^1]: …` definition must survive
    // verbatim, not collapse into a rendered superscript + footnote section.
    const markdown = "Claim [^1].\n\n[^1]: Evidence - http://example.com\n";

    const result = roundTrip(markdown);

    expect(result).toContain("[^1]");
    expect(result).toContain("[^1]: Evidence - http://example.com");
  });

  it("emits broken-link class when the resolver rejects a slug", () => {
    const html = wikiMarkdownToHtml("See [[missing/page]].", () => false);

    expect(html).toContain('data-wikilink="true"');
    expect(html).toContain('data-slug="missing/page"');
    expect(html).toContain("wk-broken");
  });

  it("does not flag links broken without a resolver", () => {
    const html = wikiMarkdownToHtml("See [[any/page]].");

    expect(html).toContain('class="wk-wikilink"');
    expect(html).not.toContain("wk-broken");
  });

  it("preserves fenced code language info strings", () => {
    const html = wikiMarkdownToHtml("```fact\nsubject: x\n```\n");

    expect(html).toContain('class="language-fact"');
  });

  it("serialises a display wikilink as [[slug|Display]]", () => {
    const markdown = wikiHtmlToMarkdown(
      '<p><a data-wikilink="true" data-slug="team/x">Different</a></p>',
    );

    expect(markdown.trim()).toBe("[[team/x|Different]]");
  });

  it("serialises a matching-display wikilink as [[slug]]", () => {
    const markdown = wikiHtmlToMarkdown(
      '<p><a data-wikilink="true" data-slug="team/x">team/x</a></p>',
    );

    expect(markdown.trim()).toBe("[[team/x]]");
  });
});
