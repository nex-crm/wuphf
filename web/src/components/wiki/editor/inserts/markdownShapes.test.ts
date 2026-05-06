/**
 * Round-trip tests for the WUPHF-specific insert shapes.
 *
 * Each insert action emits a fragment that gets concatenated into article
 * markdown. This file proves that:
 *   1. The fragment alone survives Milkdown parse then serialize then
 *      `postProcessWikilinks` unchanged.
 *   2. The fragment composed with surrounding article content also
 *      survives (no parse interaction with neighbouring blocks).
 *
 * The harness mirrors `RichWikiEditor.roundtrip.test.tsx` exactly so the
 * test stays a faithful proxy for the production editor pipeline.
 */
import {
  defaultValueCtx,
  Editor,
  remarkPluginsCtx,
  remarkStringifyOptionsCtx,
} from "@milkdown/core";
import { commonmark } from "@milkdown/preset-commonmark";
import { gfm } from "@milkdown/preset-gfm";
import { getMarkdown } from "@milkdown/utils";
import { describe, expect, it } from "vitest";

import { wikiLinkRemarkPlugin } from "../../../../lib/wikilink";
import { STRINGIFY_DEFAULTS } from "../../../../lib/wikilinkStringify";
import { postProcessWikilinks } from "../wikilinkPostProcess";
import {
  appendBlockToTail,
  appendCitationDefinition,
  buildCitation,
  buildDecisionBlock,
  buildFactBlock,
  buildRelatedBlock,
  buildWikilink,
  nextFootnoteId,
} from "./markdownShapes";

function normalise(s: string): string {
  return s
    .split("\n")
    .map((l) => l.trimEnd())
    .join("\n")
    .trim();
}

async function roundTrip(initial: string): Promise<string> {
  const root = document.createElement("div");
  document.body.appendChild(root);
  try {
    const editor = await Editor.make()
      .config((ctx) => {
        ctx.set(defaultValueCtx, initial);
        ctx.update(remarkPluginsCtx, (prev) => [
          ...prev,
          {
            plugin: wikiLinkRemarkPlugin(() => true),
            options: {},
          },
        ]);
        ctx.update(remarkStringifyOptionsCtx, (prev) => ({
          ...prev,
          ...STRINGIFY_DEFAULTS,
        }));
      })
      .use(commonmark)
      .use(gfm)
      .create();
    try {
      const md = editor.action(getMarkdown());
      return postProcessWikilinks(md);
    } finally {
      await editor.destroy();
    }
  } finally {
    root.remove();
  }
}

// ─── buildWikilink ─────────────────────────────────────────────────────────

describe("buildWikilink", () => {
  it("emits bare slug form when display matches slug", () => {
    expect(buildWikilink("alex")).toBe("[[alex]]");
  });

  it("emits piped form when display differs from slug", () => {
    expect(buildWikilink("team/people/alex", "Alex Chen")).toBe(
      "[[team/people/alex|Alex Chen]]",
    );
  });

  it("rejects path traversal", () => {
    expect(buildWikilink("../escape")).toBeNull();
  });

  it("rejects empty slug", () => {
    expect(buildWikilink("   ")).toBeNull();
  });

  it("survives round-trip in a paragraph", async () => {
    const md = `See ${buildWikilink("alex")} for details.\n`;
    expect(normalise(await roundTrip(md))).toBe(normalise(md));
  });

  it("survives round-trip with display text in a paragraph", async () => {
    const md = `Owner: ${buildWikilink("team/people/alex", "Alex Chen")}.\n`;
    expect(normalise(await roundTrip(md))).toBe(normalise(md));
  });
});

// ─── buildCitation ─────────────────────────────────────────────────────────

describe("buildCitation", () => {
  it("returns inline reference plus block definition", () => {
    const built = buildCitation({
      id: "1",
      title: "WUPHF launch",
      url: "https://example.com/launch",
    });
    expect(built.reference).toBe("[^1]");
    expect(built.definition).toBe(
      "[^1]: WUPHF launch - https://example.com/launch\n",
    );
  });

  it("falls back to URL when title is empty", () => {
    const built = buildCitation({
      id: "1",
      title: "",
      url: "https://example.com",
    });
    expect(built.definition).toBe(
      "[^1]: https://example.com - https://example.com\n",
    );
  });

  it("strips disallowed footnote-id characters", () => {
    const built = buildCitation({
      id: "weird id",
      title: "x",
      url: "https://x.com",
    });
    expect(built.reference).toBe("[^weirdid]");
  });

  it("survives round-trip when reference and definition coexist", async () => {
    const cite = buildCitation({
      id: "1",
      title: "Source",
      url: "https://example.com",
    });
    const md = `Body${cite.reference}.\n\n${cite.definition}`;
    const out = normalise(await roundTrip(md));
    expect(out).toContain(cite.reference);
    expect(out).toContain("[^1]:");
    expect(out).toContain("https://example.com");
  });
});

describe("nextFootnoteId", () => {
  it("returns 1 when no footnotes exist", () => {
    expect(nextFootnoteId("plain markdown")).toBe("1");
  });

  it("skips ids already in use", () => {
    expect(nextFootnoteId("ref [^1] and [^2]\n[^1]: a\n[^2]: b\n")).toBe("3");
  });

  it("keeps numbering even when ids are non-numeric", () => {
    expect(nextFootnoteId("see [^source-a]")).toBe("1");
  });
});

describe("appendCitationDefinition", () => {
  it("appends when the id is new", () => {
    const out = appendCitationDefinition(
      "Body of article.\n",
      "[^1]: Title - https://example.com\n",
    );
    expect(out).toContain("[^1]: Title - https://example.com");
    expect(out.endsWith("\n")).toBe(true);
  });

  it("does not append when the id already exists", () => {
    const before = "Body.\n\n[^1]: Existing - https://existing.com\n";
    const out = appendCitationDefinition(
      before,
      "[^1]: Different - https://different.com\n",
    );
    expect(out).toBe(before);
  });

  it("preserves a single trailing newline", () => {
    const out = appendCitationDefinition(
      "Body.\n\n\n",
      "[^1]: T - https://x.com\n",
    );
    expect(out.endsWith("\n")).toBe(true);
    expect(out.endsWith("\n\n")).toBe(false);
  });
});

describe("appendBlockToTail", () => {
  it("appends a block separated by a blank line", () => {
    const out = appendBlockToTail("Body.\n", "```fact\nx\n```\n");
    expect(out).toBe("Body.\n\n```fact\nx\n```\n");
  });

  it("returns just the block when the document is empty", () => {
    const out = appendBlockToTail("", "```fact\nx\n```\n");
    expect(out).toBe("```fact\nx\n```\n");
  });

  it("ignores empty blocks", () => {
    const out = appendBlockToTail("Body.\n", "");
    expect(out).toBe("Body.\n");
  });

  it("collapses any trailing whitespace before the separator", () => {
    const out = appendBlockToTail("Body.\n\n\n", "```fact\nx\n```");
    expect(out).toBe("Body.\n\n```fact\nx\n```\n");
  });
});

// ─── buildFactBlock ────────────────────────────────────────────────────────

describe("buildFactBlock", () => {
  it("emits a fenced block with the required keys", () => {
    const block = buildFactBlock({
      subject: "alex",
      predicate: "works_at",
      object: "nex",
    });
    expect(block).toBe(
      "```fact\nsubject: alex\npredicate: works_at\nobject: nex\n```\n",
    );
  });

  it("includes confidence when provided", () => {
    const block = buildFactBlock({
      subject: "alex",
      predicate: "works_at",
      object: "nex",
      confidence: 0.9,
    });
    expect(block).toContain("confidence: 0.9");
  });

  it("clamps confidence to [0, 1]", () => {
    expect(
      buildFactBlock({
        subject: "a",
        predicate: "b",
        object: "c",
        confidence: 1.5,
      }),
    ).toContain("confidence: 1");
    expect(
      buildFactBlock({
        subject: "a",
        predicate: "b",
        object: "c",
        confidence: -0.4,
      }),
    ).toContain("confidence: 0");
  });

  it("escapes embedded fences so the surrounding fence stays intact", () => {
    const block = buildFactBlock({
      subject: "alex",
      predicate: "works_at",
      // Adversarial input: try to terminate the enclosing fence.
      object: "evil ``` payload",
    });
    // The embedded triple-backtick must be escaped so the fence has
    // exactly one opener and one closer (split produces 3 parts).
    expect(block.split("```").length).toBe(3); // opener + closer only
    // No bare ``` on a line by itself inside the block content.
    const lines = block.split("\n").slice(1, -2); // strip opener/closer
    expect(lines.every((l) => l !== "```")).toBe(true);
  });

  it("survives round-trip through Milkdown", async () => {
    const block = buildFactBlock({
      subject: "alex",
      predicate: "works_at",
      object: "nex",
      confidence: 0.9,
    });
    const md = `Some prose.\n\n${block}\nMore prose.\n`;
    expect(normalise(await roundTrip(md))).toBe(normalise(md));
  });
});

// ─── buildDecisionBlock ────────────────────────────────────────────────────

describe("buildDecisionBlock", () => {
  it("emits a fenced block with title, date, rationale", () => {
    const block = buildDecisionBlock({
      title: "Adopt Milkdown for rich editor",
      date: "2026-05-06",
      rationale: "Smallest workable surface for round-trip wikilinks.",
    });
    expect(block).toContain("```decision");
    expect(block).toContain("title: Adopt Milkdown for rich editor");
    expect(block).toContain("date: 2026-05-06");
    expect(block).toContain(
      "rationale: Smallest workable surface for round-trip wikilinks.",
    );
  });

  it("appends comma-joined alternatives when provided", () => {
    const block = buildDecisionBlock({
      title: "x",
      date: "2026-05-06",
      rationale: "y",
      alternatives: ["TipTap", "ProseMirror raw", " "],
    });
    expect(block).toContain("alternatives: TipTap, ProseMirror raw");
  });

  it("survives round-trip through Milkdown", async () => {
    const block = buildDecisionBlock({
      title: "Adopt Milkdown",
      date: "2026-05-06",
      rationale: "Smallest workable surface.",
      alternatives: ["TipTap", "ProseMirror"],
    });
    const md = `# Engineering log\n\n${block}\n`;
    expect(normalise(await roundTrip(md))).toBe(normalise(md));
  });
});

// ─── buildRelatedBlock ─────────────────────────────────────────────────────

describe("buildRelatedBlock", () => {
  it("emits a heading + bullet list of wikilinks", () => {
    const block = buildRelatedBlock([
      { slug: "team/people/alex", display: "Alex Chen" },
      { slug: "team/projects/backend" },
    ]);
    expect(block).toBe(
      "## Related\n\n- [[team/people/alex|Alex Chen]]\n- [[team/projects/backend]]\n",
    );
  });

  it("returns empty string when no entries are valid", () => {
    expect(buildRelatedBlock([{ slug: ".." }])).toBe("");
  });

  it("drops malformed entries without aborting the block", () => {
    const block = buildRelatedBlock([
      { slug: "alex" },
      { slug: ".." },
      { slug: "/abs" },
      { slug: "team/projects/backend" },
    ]);
    expect(block).toContain("- [[alex]]");
    expect(block).toContain("- [[team/projects/backend]]");
    expect(block).not.toContain("..");
  });

  it("survives round-trip through Milkdown in canonical loose-list form", async () => {
    // Milkdown's commonmark serializer renders bullet lists in loose form
    // (a blank line between siblings) — the same shape the existing
    // RichWikiEditor.roundtrip.test.tsx pins for nested lists. Edits
    // settle on this canonical shape after one save and remain stable.
    const block = buildRelatedBlock([
      { slug: "team/people/alex", display: "Alex Chen" },
      { slug: "team/projects/backend" },
    ]);
    const canonical =
      "# Page\n\nSome prose.\n\n## Related\n\n- [[team/people/alex|Alex Chen]]\n\n- [[team/projects/backend]]\n";
    const md = `# Page\n\nSome prose.\n\n${block}`;
    expect(normalise(await roundTrip(md))).toBe(normalise(canonical));
  });
});
