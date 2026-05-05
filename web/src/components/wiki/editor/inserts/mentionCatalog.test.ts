import { describe, expect, it } from "vitest";

import type { WikiCatalogEntry } from "../../../../api/wiki";
import {
  categoryOrder,
  groupMentionItems,
  searchMentionItems,
  toMentionItem,
} from "./mentionCatalog";

function entry(
  partial: Partial<WikiCatalogEntry> & { path: string; group: string },
): WikiCatalogEntry {
  return {
    title: partial.path.split("/").pop()?.replace(/\.md$/, "") ?? "",
    author_slug: "human",
    last_edited_ts: "2026-01-01T00:00:00Z",
    ...partial,
  } as WikiCatalogEntry;
}

/**
 * Test helper that throws when an entry fails wikilink validation. Lets
 * the test list stay readable without scattered non-null assertions.
 */
function mentionFromEntry(
  partial: Partial<WikiCatalogEntry> & { path: string; group: string },
) {
  const item = toMentionItem(entry(partial));
  if (!item)
    throw new Error(`fixture path is not a valid mention: ${partial.path}`);
  return item;
}

describe("toMentionItem", () => {
  it("strips the .md extension from the slug", () => {
    const item = toMentionItem(
      entry({ path: "team/people/alex.md", group: "people", title: "Alex" }),
    );
    expect(item).not.toBeNull();
    expect(item?.slug).toBe("team/people/alex");
    expect(item?.category).toBe("people");
    expect(item?.title).toBe("Alex");
  });

  it("classifies legacy agents/ paths as agents even when group is empty", () => {
    const item = toMentionItem(
      entry({ path: "agents/operator/notebook/index.md", group: "" }),
    );
    expect(item?.category).toBe("agents");
  });

  it("falls back to pages bucket for unknown groups", () => {
    const item = toMentionItem(
      entry({ path: "team/playbooks/onboarding.md", group: "playbooks" }),
    );
    expect(item?.category).toBe("pages");
  });

  it("rejects entries whose path is not a legal wikilink slug", () => {
    expect(
      toMentionItem(entry({ path: "../escape.md", group: "x" })),
    ).toBeNull();
    expect(
      toMentionItem(entry({ path: "/leading-slash.md", group: "x" })),
    ).toBeNull();
  });
});

describe("searchMentionItems", () => {
  const items = [
    mentionFromEntry({
      path: "team/people/alex.md",
      group: "people",
      title: "Alex Chen",
    }),
    mentionFromEntry({
      path: "team/people/sarah.md",
      group: "people",
      title: "Sarah Lee",
    }),
    mentionFromEntry({
      path: "team/projects/backend.md",
      group: "projects",
      title: "Backend rewrite",
    }),
    mentionFromEntry({
      path: "agents/operator/notebook/today.md",
      group: "agents",
      title: "Operator today",
    }),
  ];

  it("returns all items when query is empty (limited)", () => {
    const out = searchMentionItems(items, "");
    expect(out).toHaveLength(4);
  });

  it("ranks title-prefix matches highest", () => {
    const out = searchMentionItems(items, "alex");
    expect(out[0]?.title).toBe("Alex Chen");
  });

  it("falls through to substring matches", () => {
    const out = searchMentionItems(items, "rewrite");
    expect(out.map((i) => i.title)).toContain("Backend rewrite");
  });

  it("respects the limit", () => {
    const out = searchMentionItems(items, "", 2);
    expect(out).toHaveLength(2);
  });
});

describe("groupMentionItems", () => {
  it("groups items by category in canonical order", () => {
    const items = [
      mentionFromEntry({ path: "team/projects/backend.md", group: "projects" }),
      mentionFromEntry({ path: "team/people/alex.md", group: "people" }),
      mentionFromEntry({
        path: "agents/operator/notebook/today.md",
        group: "agents",
      }),
    ];
    const grouped = groupMentionItems(items);
    expect(grouped.map((g) => g.category)).toEqual([
      "people",
      "projects",
      "agents",
    ]);
  });

  it("omits empty buckets", () => {
    const items = [
      mentionFromEntry({ path: "team/people/alex.md", group: "people" }),
    ];
    const grouped = groupMentionItems(items);
    expect(grouped.map((g) => g.category)).toEqual(["people"]);
  });
});

describe("categoryOrder", () => {
  it("includes every supported category", () => {
    const cats = categoryOrder();
    expect(cats).toContain("pages");
    expect(cats).toContain("agents");
    expect(cats).toContain("tasks");
    expect(cats).toContain("people");
    expect(cats).toContain("companies");
    expect(cats).toContain("projects");
  });
});
