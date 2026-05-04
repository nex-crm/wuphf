/**
 * Tests for `wikilinkStringify` — the markdown serialization handler that
 * keeps `[[slug]]` syntax intact through a parse → serialize round-trip.
 *
 * Coverage focuses on:
 *   - The wiki-link contract (the silent-failure class of bug this code
 *     prevents): with the handler, `[[slug]]` survives; without it, every
 *     wiki-link gets corrupted into a standard markdown link.
 *   - GFM features the wiki actively uses (tables, fenced code, checklists,
 *     nested lists) so a future swap of the markdown pipeline cannot
 *     silently break them.
 *
 * Trivial structures (H1, paragraphs, bold/italic) are left to the upstream
 * `remark-stringify` test suite — they're not at risk of regression from
 * our handler.
 */
import remarkGfm from "remark-gfm";
import remarkParse from "remark-parse";
import remarkStringify from "remark-stringify";
import { unified } from "unified";
import { describe, expect, it } from "vitest";

import { wikiLinkRemarkPlugin } from "./wikilink";
import {
  remarkWikilinkStringify,
  STRINGIFY_DEFAULTS,
} from "./wikilinkStringify";

function buildProcessor() {
  return unified()
    .use(remarkParse)
    .use(remarkGfm)
    .use(wikiLinkRemarkPlugin(() => true))
    .use(remarkWikilinkStringify)
    .use(remarkStringify, STRINGIFY_DEFAULTS);
}

function buildProcessorWithoutHandler() {
  return unified()
    .use(remarkParse)
    .use(remarkGfm)
    .use(wikiLinkRemarkPlugin(() => true))
    .use(remarkStringify, STRINGIFY_DEFAULTS);
}

async function roundTrip(input: string): Promise<string> {
  return String(await buildProcessor().process(input));
}

async function roundTripWithoutHandler(input: string): Promise<string> {
  return String(await buildProcessorWithoutHandler().process(input));
}

function normalise(s: string): string {
  return s
    .split("\n")
    .map((l) => l.trimEnd())
    .join("\n")
    .trim();
}

// ─── wiki links — the contract this module exists to enforce ────────────────

describe("wiki links", () => {
  it("[[slug]] survives round-trip", async () => {
    const result = normalise(await roundTrip("See [[alex]] for details.\n"));
    expect(result).toBe("See [[alex]] for details.");
  });

  it("[[slug|Display]] preserves both slug and display text", async () => {
    const result = normalise(
      await roundTrip("See [[people/alex|Alex Chen]] for details.\n"),
    );
    expect(result).toBe("See [[people/alex|Alex Chen]] for details.");
  });

  it("multiple wiki links in one paragraph all survive", async () => {
    const result = normalise(
      await roundTrip("[[alex]] works with [[sarah]] on [[project-x]].\n"),
    );
    expect(result).toContain("[[alex]]");
    expect(result).toContain("[[sarah]]");
    expect(result).toContain("[[project-x]]");
  });

  it("wiki link inside a heading survives", async () => {
    const result = normalise(await roundTrip("## About [[alex]]\n"));
    expect(result).toContain("[[alex]]");
  });

  it("wiki link inside a list item survives", async () => {
    const result = normalise(await roundTrip("- Contact [[sarah]] for help\n"));
    expect(result).toContain("[[sarah]]");
  });

  // Regression guard for the silent-failure class: without the handler,
  // every save corrupts every wiki link. If this test starts passing, the
  // handler has been disabled and saves will silently destroy syntax.
  it("[[slug]] is CORRUPTED when the handler is missing", async () => {
    const result = normalise(
      await roundTripWithoutHandler("See [[alex]] for details.\n"),
    );
    expect(result).not.toContain("[[alex]]");
    expect(result).toContain("[alex]");
    expect(result).toContain("#/wiki/alex");
  });
});

// ─── GFM structures the wiki actively uses ─────────────────────────────────

describe("GFM tables", () => {
  it("simple table survives", async () => {
    const result = normalise(
      await roundTrip("| Name | Role |\n| --- | --- |\n| Alex | Eng |\n"),
    );
    expect(result).toContain("Name");
    expect(result).toContain("Role");
    expect(result).toContain("Alex");
    expect(result).toContain("|");
  });

  it("table with alignment survives", async () => {
    const result = normalise(
      await roundTrip(
        "| Left | Centre | Right |\n| :--- | :---: | ---: |\n| a | b | c |\n",
      ),
    );
    expect(result).toContain("Left");
    expect(result).toContain("Centre");
    expect(result).toContain("Right");
  });
});

describe("code blocks", () => {
  it("fenced code with language survives", async () => {
    const result = normalise(
      await roundTrip("```typescript\nconst x = 1;\n```\n"),
    );
    expect(result).toContain("```typescript");
    expect(result).toContain("const x = 1;");
  });

  it("internal newlines and indentation are preserved", async () => {
    const result = await roundTrip(
      '```go\nfunc main() {\n\tfmt.Println("hi")\n}\n```\n',
    );
    expect(result).toContain("func main()");
    expect(result).toContain("fmt.Println");
  });
});

describe("lists", () => {
  it("GFM checklist survives", async () => {
    const result = normalise(await roundTrip("- [x] Done\n- [ ] Todo\n"));
    expect(result).toContain("[x] Done");
    expect(result).toContain("[ ] Todo");
  });

  it("nested list survives", async () => {
    const result = normalise(
      await roundTrip("- Parent\n  - Child\n  - Child two\n"),
    );
    expect(result).toContain("Parent");
    expect(result).toContain("Child");
  });
});

// ─── realistic article — the wiki-link contract under load ─────────────────

describe("mixed document", () => {
  it("a realistic wiki article round-trips cleanly", async () => {
    const md = [
      "# Alex Chen",
      "",
      "## Role",
      "",
      "Engineering lead at [[company/nex]].",
      "",
      "## Skills",
      "",
      "- TypeScript",
      "- **Architecture**",
      "",
      "## Notes",
      "",
      "| Date | Event |",
      "| --- | --- |",
      "| 2026-01-01 | Joined |",
      "",
      "```go",
      'fmt.Println("hello")',
      "```",
      "",
    ].join("\n");

    const result = await roundTrip(md);

    expect(result).toContain("# Alex Chen");
    expect(result).toContain("[[company/nex]]");
    expect(result).toContain("Date");
    expect(result).toContain("Joined");
    expect(result).toContain("```go");
    expect(result).toContain("**Architecture**");
  });
});
