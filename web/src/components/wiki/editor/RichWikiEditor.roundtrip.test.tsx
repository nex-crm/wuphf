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
  //
  // The root must always be removed even if `Editor.make().create()`
  // rejects, otherwise an init failure on the first test would leak an
  // orphan <div> into document.body for every subsequent test in the
  // file. Outer try/finally guarantees cleanup regardless of where
  // construction fails.
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
    }
  } finally {
    root.remove();
  }
}

// Each test asserts full normalised equality (`toBe`) — looser substring
// checks would let regressions like list-flattening or table-shape change
// slip through.
//
// Where Milkdown deterministically *normalises* the source (loose-form
// nested lists, padded table cells), the test uses an explicit canonical
// expected value. That IS the round-trip contract: edits settle on the
// canonical shape after one save and remain stable thereafter.

// ─── headings + paragraphs ─────────────────────────────────────────────────

describe("RichWikiEditor round-trip — headings + paragraphs", () => {
  it("preserves an H1 + paragraph", async () => {
    const md = "# Alex Chen\n\nEngineering lead.\n";
    expect(normalise(await roundTrip(md))).toBe(normalise(md));
  });

  it("preserves H2/H3 hierarchy", async () => {
    const md = "## Role\n\n### Background\n\nDetails here.\n";
    expect(normalise(await roundTrip(md))).toBe(normalise(md));
  });
});

// ─── inline emphasis ────────────────────────────────────────────────────────

describe("RichWikiEditor round-trip — inline emphasis", () => {
  it("preserves bold + italic + inline code", async () => {
    const md = "Mix of **bold**, _italic_, and `inline code`.\n";
    expect(normalise(await roundTrip(md))).toBe(normalise(md));
  });
});

// ─── lists + checklists ────────────────────────────────────────────────────
//
// Milkdown's commonmark serializer renders nested and checklist lists in
// loose form (a blank line between siblings). Content survives intact,
// structure is preserved, and the shape stabilises after one save.

describe("RichWikiEditor round-trip — lists", () => {
  it("preserves bullet list with nested items in canonical loose form", async () => {
    const md = "- Parent\n  - Child A\n  - Child B\n";
    const canonical = "- Parent\n\n  - Child A\n\n  - Child B\n";
    expect(normalise(await roundTrip(md))).toBe(normalise(canonical));
  });

  it("preserves GFM checklist in canonical loose form", async () => {
    const md = "- [x] Done\n- [ ] Todo\n";
    const canonical = "- [x] Done\n\n- [ ] Todo\n";
    expect(normalise(await roundTrip(md))).toBe(normalise(canonical));
  });
});

// ─── fenced code blocks ────────────────────────────────────────────────────

describe("RichWikiEditor round-trip — code blocks", () => {
  it("preserves fenced code with language", async () => {
    const md = "```typescript\nconst x = 1;\n```\n";
    expect(normalise(await roundTrip(md))).toBe(normalise(md));
  });

  it("preserves multi-line code body with tabs and quotes", async () => {
    const md = '```go\nfunc main() {\n\tfmt.Println("hi")\n}\n```\n';
    expect(normalise(await roundTrip(md))).toBe(normalise(md));
  });
});

// ─── GFM tables ────────────────────────────────────────────────────────────
//
// Milkdown aligns columns and pads cells to the widest entry. That's
// canonical GFM table output and stabilises on first save.

describe("RichWikiEditor round-trip — tables", () => {
  it("preserves a simple table with canonical column padding", async () => {
    const md = "| Name | Role |\n| --- | --- |\n| Alex | Eng |\n";
    const canonical = "| Name | Role |\n| ---- | ---- |\n| Alex | Eng  |\n";
    expect(normalise(await roundTrip(md))).toBe(normalise(canonical));
  });
});

// ─── wiki-links — the contract this PR exists to enforce ───────────────────

describe("RichWikiEditor round-trip — wiki-links", () => {
  it("preserves [[slug]]", async () => {
    const md = "See [[alex]] for details.\n";
    expect(normalise(await roundTrip(md))).toBe(normalise(md));
  });

  it("preserves [[slug|Display]]", async () => {
    const md = "See [[people/alex|Alex Chen]] for details.\n";
    expect(normalise(await roundTrip(md))).toBe(normalise(md));
  });

  it("preserves multiple wiki-links in one paragraph", async () => {
    const md = "[[alex]] works with [[sarah]] on [[project-x]].\n";
    expect(normalise(await roundTrip(md))).toBe(normalise(md));
  });
});

// ─── standard markdown links — must not get hijacked by post-process ───────

describe("RichWikiEditor round-trip — standard links", () => {
  it("preserves [text](url)", async () => {
    const md = "See [the docs](https://example.com) for details.\n";
    expect(normalise(await roundTrip(md))).toBe(normalise(md));
  });

  it("preserves wiki-link and standard link in the same paragraph", async () => {
    const md = "See [[alex]] and [the docs](https://docs.example.com) here.\n";
    expect(normalise(await roundTrip(md))).toBe(normalise(md));
  });
});
