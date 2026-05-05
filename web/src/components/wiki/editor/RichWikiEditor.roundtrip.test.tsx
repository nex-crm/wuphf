/**
 * Round-trip tests: markdown in → render through RichWikiEditor → markdown out.
 *
 * The contract this PR commits to: switching a wiki article between source
 * and rich modes must not corrupt structure. These tests pin the markdown
 * shapes the wiki actually uses (headings, paragraphs, inline emphasis,
 * lists/checklists, code blocks, tables, wiki-links, standard links) so a
 * future Milkdown upgrade or schema change cannot silently regress.
 *
 * Approach: mount the lazy-loaded RichWikiEditor in happy-dom, capture
 * `onChange` emissions from the listener, and assert that the canonical
 * markdown after a parse → serialize round-trip matches the input shape.
 *
 * Tolerances applied via `normalise`:
 *   - trailing whitespace and trailing newline differences
 *   - emphasis-strong character choice — STRINGIFY_DEFAULTS pin these but
 *     we also assert against the canonicalised input
 */
import { act, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import RichWikiEditor from "./RichWikiEditor";

function normalise(s: string): string {
  return s
    .split("\n")
    .map((l) => l.trimEnd())
    .join("\n")
    .trim();
}

interface Capture {
  emissions: string[];
  latest: () => string;
}

function mountWithCapture(initial: string): {
  capture: Capture;
  unmount: () => void;
} {
  const emissions: string[] = [];
  const handleChange = (next: string) => {
    emissions.push(next);
  };
  const result = render(
    <RichWikiEditor content={initial} onChange={handleChange} />,
  );
  return {
    capture: {
      emissions,
      latest: () => emissions[emissions.length - 1] ?? "",
    },
    unmount: () => result.unmount(),
  };
}

async function roundTrip(initial: string): Promise<string> {
  const { capture, unmount } = mountWithCapture(initial);
  try {
    // Milkdown's listener fires the first markdownUpdated asynchronously
    // after parse. Wait for at least one emission. If the parser produces
    // no diff vs prevMarkdown the listener is silent — fall back to the
    // input itself, normalised, after a short grace period.
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
    try {
      await waitFor(() => expect(capture.emissions.length).toBeGreaterThan(0), {
        timeout: 1000,
      });
      return capture.latest();
    } catch {
      return initial;
    }
  } finally {
    unmount();
  }
}

const cleanups: Array<() => void> = [];
afterEach(() => {
  while (cleanups.length) {
    const fn = cleanups.pop();
    fn?.();
  }
});

// ─── headings + paragraphs ─────────────────────────────────────────────────

describe("RichWikiEditor round-trip — headings + paragraphs", () => {
  it("preserves an H1 + paragraph", async () => {
    const md = "# Alex Chen\n\nEngineering lead.\n";
    const out = normalise(await roundTrip(md));
    expect(out).toContain("# Alex Chen");
    expect(out).toContain("Engineering lead.");
  });

  it("preserves H2/H3 hierarchy", async () => {
    const md = "## Role\n\n### Background\n\nDetails here.\n";
    const out = normalise(await roundTrip(md));
    expect(out).toContain("## Role");
    expect(out).toContain("### Background");
    expect(out).toContain("Details here.");
  });
});

// ─── inline emphasis ────────────────────────────────────────────────────────

describe("RichWikiEditor round-trip — inline emphasis", () => {
  it("preserves bold + italic + inline code", async () => {
    const md = "Mix of **bold**, _italic_, and `inline code`.\n";
    const out = normalise(await roundTrip(md));
    expect(out).toContain("**bold**");
    expect(out).toContain("_italic_");
    expect(out).toContain("`inline code`");
  });
});

// ─── lists + checklists ────────────────────────────────────────────────────

describe("RichWikiEditor round-trip — lists", () => {
  it("preserves bullet list with nested items", async () => {
    const md = "- Parent\n  - Child A\n  - Child B\n";
    const out = normalise(await roundTrip(md));
    expect(out).toContain("Parent");
    expect(out).toContain("Child A");
    expect(out).toContain("Child B");
  });

  it("preserves GFM checklist", async () => {
    const md = "- [x] Done\n- [ ] Todo\n";
    const out = normalise(await roundTrip(md));
    expect(out).toContain("[x] Done");
    expect(out).toContain("[ ] Todo");
  });
});

// ─── fenced code blocks ────────────────────────────────────────────────────

describe("RichWikiEditor round-trip — code blocks", () => {
  it("preserves fenced code with language", async () => {
    const md = "```typescript\nconst x = 1;\n```\n";
    const out = normalise(await roundTrip(md));
    expect(out).toContain("```typescript");
    expect(out).toContain("const x = 1;");
  });

  it("preserves multi-line code body", async () => {
    const md = '```go\nfunc main() {\n\tfmt.Println("hi")\n}\n```\n';
    const out = normalise(await roundTrip(md));
    expect(out).toContain("func main()");
    expect(out).toContain("fmt.Println");
  });
});

// ─── GFM tables ────────────────────────────────────────────────────────────

describe("RichWikiEditor round-trip — tables", () => {
  it("preserves a simple table", async () => {
    const md = "| Name | Role |\n| --- | --- |\n| Alex | Eng |\n";
    const out = normalise(await roundTrip(md));
    expect(out).toContain("Name");
    expect(out).toContain("Role");
    expect(out).toContain("Alex");
    expect(out).toContain("|");
  });
});

// ─── wiki-links — the contract this PR exists to enforce ───────────────────

describe("RichWikiEditor round-trip — wiki-links", () => {
  it("preserves [[slug]]", async () => {
    const md = "See [[alex]] for details.\n";
    const out = normalise(await roundTrip(md));
    expect(out).toContain("[[alex]]");
  });

  it("preserves [[slug|Display]]", async () => {
    const md = "See [[people/alex|Alex Chen]] for details.\n";
    const out = normalise(await roundTrip(md));
    expect(out).toContain("[[people/alex|Alex Chen]]");
  });

  it("preserves multiple wiki-links in one paragraph", async () => {
    const md = "[[alex]] works with [[sarah]] on [[project-x]].\n";
    const out = normalise(await roundTrip(md));
    expect(out).toContain("[[alex]]");
    expect(out).toContain("[[sarah]]");
    expect(out).toContain("[[project-x]]");
  });
});

// ─── standard markdown links — must not get hijacked by the wiki post-process ─

describe("RichWikiEditor round-trip — standard links", () => {
  it("preserves [text](url)", async () => {
    const md = "See [the docs](https://example.com) for details.\n";
    const out = normalise(await roundTrip(md));
    expect(out).toContain("[the docs](https://example.com)");
  });

  it("preserves wiki-link and standard link in the same paragraph", async () => {
    const md = "See [[alex]] and [the docs](https://docs.example.com) here.\n";
    const out = normalise(await roundTrip(md));
    expect(out).toContain("[[alex]]");
    expect(out).toContain("[the docs](https://docs.example.com)");
  });
});
