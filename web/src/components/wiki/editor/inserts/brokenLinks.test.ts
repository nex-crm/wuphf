import { describe, expect, it } from "vitest";

import { findBrokenWikilinks } from "./brokenLinks";

describe("findBrokenWikilinks", () => {
  it("returns the slugs missing from the catalog", () => {
    const md = "See [[alex]] and [[ghost]]. Also [[sarah]].";
    const known = new Set(["alex", "sarah"]);
    const broken = findBrokenWikilinks(md, (s) => known.has(s));
    expect(broken.map((b) => b.slug)).toEqual(["ghost"]);
  });

  it("returns empty when every slug resolves", () => {
    const md = "[[alex]] worked with [[sarah]].";
    const broken = findBrokenWikilinks(md, () => true);
    expect(broken).toEqual([]);
  });

  it("ignores wikilinks inside fenced code blocks", () => {
    const md = "Real [[alex]].\n\n```\n[[ghost]]\n```\n";
    const broken = findBrokenWikilinks(md, (s) => s === "alex");
    expect(broken).toEqual([]);
  });

  it("ignores wikilinks inside inline code spans", () => {
    const md = "See `[[ghost]]` for syntax. Real link [[alex]].";
    const broken = findBrokenWikilinks(md, (s) => s === "alex");
    expect(broken).toEqual([]);
  });

  it("dedupes repeated broken slugs", () => {
    const md = "[[ghost]] then [[ghost]] again.";
    const broken = findBrokenWikilinks(md, () => false);
    expect(broken.map((b) => b.slug)).toEqual(["ghost"]);
  });

  it("ignores malformed wikilink shapes", () => {
    const md = "[[..]] [[/abs]] [[good]]";
    const broken = findBrokenWikilinks(md, () => false);
    expect(broken.map((b) => b.slug)).toEqual(["good"]);
  });

  it("ignores wikilinks inside tilde-fenced code blocks", () => {
    const md = "Real [[alex]].\n\n~~~\n[[ghost]]\n~~~\n";
    const broken = findBrokenWikilinks(md, (s) => s === "alex");
    expect(broken).toEqual([]);
  });

  it("ignores wikilinks inside multi-backtick inline spans", () => {
    const md = "Use ``[[ghost]]`` for examples. Real [[alex]].";
    const broken = findBrokenWikilinks(md, (s) => s === "alex");
    expect(broken).toEqual([]);
  });
});
