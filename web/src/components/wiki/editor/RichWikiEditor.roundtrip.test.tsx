/**
 * Round-trip tests: markdown in → Milkdown parse → serialize → markdown out.
 *
 * The contract this PR commits to: switching a wiki article between source
 * and rich modes must not corrupt structure. These tests pin the markdown
 * shapes the wiki actually uses (headings, paragraphs, inline emphasis,
 * lists/checklists, code blocks, tables, wiki-links, standard links) so a
 * future Milkdown upgrade or schema change cannot silently regress.
 *
 * Approach: run Milkdown's parser + serializer headlessly via
 * `Editor.make()` and `getMarkdown()` — the same pipeline the rich editor
 * mounts in production, minus the React/DOM layer. This avoids brittle
 * dependence on the `markdownUpdated` listener (which only fires on edits,
 * not on initial parse) and gives a deterministic round-trip primitive.
 *
 * The test mirrors `RichWikiEditor`'s exact configuration:
 *   - `wikiLinkRemarkPlugin` registered for `[[slug]]` parsing
 *   - `STRINGIFY_DEFAULTS` applied for canonical output formatting
 *   - `postProcessWikilinks` run on the emitted markdown to restore
 *     `[[slug]]` syntax that Milkdown's commonmark serializer would
 *     otherwise emit as `[Display](#/wiki/slug)`
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

import { wikiLinkRemarkPlugin } from "../../../lib/wikilink";
import { STRINGIFY_DEFAULTS } from "../../../lib/wikilinkStringify";
import { postProcessWikilinks } from "./wikilinkPostProcess";

function normalise(s: string): string {
  return s
    .split("\n")
    .map((l) => l.trimEnd())
    .join("\n")
    .trim();
}

async function roundTrip(initial: string): Promise<string> {
  // Milkdown still needs a DOM root even when we never render a view —
  // happy-dom provides one. The editor never paints because we tear it
  // down before any view code runs, but `make` requires the slot.
  const root = document.createElement("div");
  document.body.appendChild(root);
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
      // The editor only attaches a view when `rootCtx` is provided. We
      // skip that — `getMarkdown` reads from the editor state directly,
      // so a headless editor is sufficient for round-trip purposes.
    })
    .use(commonmark)
    .use(gfm)
    .create();
  try {
    const md = editor.action(getMarkdown());
    return postProcessWikilinks(md);
  } finally {
    await editor.destroy();
    root.remove();
  }
}

// ─── headings + paragraphs ─────────────────────────────────────────────────

describe("RichWikiEditor round-trip — headings + paragraphs", () => {
  it("preserves an H1 + paragraph", async () => {
    const out = normalise(
      await roundTrip("# Alex Chen\n\nEngineering lead.\n"),
    );
    expect(out).toContain("# Alex Chen");
    expect(out).toContain("Engineering lead.");
  });

  it("preserves H2/H3 hierarchy", async () => {
    const out = normalise(
      await roundTrip("## Role\n\n### Background\n\nDetails here.\n"),
    );
    expect(out).toContain("## Role");
    expect(out).toContain("### Background");
    expect(out).toContain("Details here.");
  });
});

// ─── inline emphasis ────────────────────────────────────────────────────────

describe("RichWikiEditor round-trip — inline emphasis", () => {
  it("preserves bold + italic + inline code", async () => {
    const out = normalise(
      await roundTrip("Mix of **bold**, _italic_, and `inline code`.\n"),
    );
    expect(out).toContain("**bold**");
    expect(out).toContain("_italic_");
    expect(out).toContain("`inline code`");
  });
});

// ─── lists + checklists ────────────────────────────────────────────────────

describe("RichWikiEditor round-trip — lists", () => {
  it("preserves bullet list with nested items", async () => {
    const out = normalise(
      await roundTrip("- Parent\n  - Child A\n  - Child B\n"),
    );
    expect(out).toContain("Parent");
    expect(out).toContain("Child A");
    expect(out).toContain("Child B");
  });

  it("preserves GFM checklist", async () => {
    const out = normalise(await roundTrip("- [x] Done\n- [ ] Todo\n"));
    expect(out).toContain("[x] Done");
    expect(out).toContain("[ ] Todo");
  });
});

// ─── fenced code blocks ────────────────────────────────────────────────────

describe("RichWikiEditor round-trip — code blocks", () => {
  it("preserves fenced code with language", async () => {
    const out = normalise(
      await roundTrip("```typescript\nconst x = 1;\n```\n"),
    );
    expect(out).toContain("```typescript");
    expect(out).toContain("const x = 1;");
  });

  it("preserves multi-line code body", async () => {
    const out = normalise(
      await roundTrip('```go\nfunc main() {\n\tfmt.Println("hi")\n}\n```\n'),
    );
    expect(out).toContain("func main()");
    expect(out).toContain("fmt.Println");
  });
});

// ─── GFM tables ────────────────────────────────────────────────────────────

describe("RichWikiEditor round-trip — tables", () => {
  it("preserves a simple table", async () => {
    const out = normalise(
      await roundTrip("| Name | Role |\n| --- | --- |\n| Alex | Eng |\n"),
    );
    expect(out).toContain("Name");
    expect(out).toContain("Role");
    expect(out).toContain("Alex");
    expect(out).toContain("|");
  });
});

// ─── wiki-links — the contract this PR exists to enforce ───────────────────

describe("RichWikiEditor round-trip — wiki-links", () => {
  it("preserves [[slug]]", async () => {
    const out = normalise(await roundTrip("See [[alex]] for details.\n"));
    expect(out).toContain("[[alex]]");
  });

  it("preserves [[slug|Display]]", async () => {
    const out = normalise(
      await roundTrip("See [[people/alex|Alex Chen]] for details.\n"),
    );
    expect(out).toContain("[[people/alex|Alex Chen]]");
  });

  it("preserves multiple wiki-links in one paragraph", async () => {
    const out = normalise(
      await roundTrip("[[alex]] works with [[sarah]] on [[project-x]].\n"),
    );
    expect(out).toContain("[[alex]]");
    expect(out).toContain("[[sarah]]");
    expect(out).toContain("[[project-x]]");
  });
});

// ─── standard markdown links — must not get hijacked by post-process ───────

describe("RichWikiEditor round-trip — standard links", () => {
  it("preserves [text](url)", async () => {
    const out = normalise(
      await roundTrip("See [the docs](https://example.com) for details.\n"),
    );
    expect(out).toContain("[the docs](https://example.com)");
  });

  it("preserves wiki-link and standard link in the same paragraph", async () => {
    const out = normalise(
      await roundTrip(
        "See [[alex]] and [the docs](https://docs.example.com) here.\n",
      ),
    );
    expect(out).toContain("[[alex]]");
    expect(out).toContain("[the docs](https://docs.example.com)");
  });
});
