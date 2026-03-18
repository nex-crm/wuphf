import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import React from "react";
import { render, cleanup } from "ink-testing-library";
import {
  QuickSwitcher,
  fuzzyMatch,
  filterItems,
} from "../../../../src/tui/components/slack/quick-switcher.js";
import type {
  QuickSwitcherItem,
  QuickSwitcherProps,
} from "../../../../src/tui/components/slack/quick-switcher.js";

// Strip ANSI escape sequences
function strip(s: string): string {
  return s.replace(/\x1b\[[0-9;]*m/g, "");
}

afterEach(() => {
  cleanup();
});

// ── Test data ───────────────────────────────────────────────────────

const ITEMS: QuickSwitcherItem[] = [
  {
    id: "dm-founding",
    name: "Founding Agent",
    type: "dm",
    online: true,
    unread: 0,
    score: 100,
  },
  {
    id: "ch-general",
    name: "general",
    type: "channel",
    unread: 0,
    score: 80,
  },
  {
    id: "dm-seo",
    name: "SEO Analyst",
    type: "dm",
    online: false,
    unread: 0,
    score: 60,
  },
  {
    id: "ch-leads",
    name: "leads",
    type: "channel",
    unread: 2,
    score: 40,
  },
];

const defaultProps: QuickSwitcherProps = {
  open: true,
  items: ITEMS,
  onSelect: () => {},
  onClose: () => {},
};

// ── fuzzyMatch ──────────────────────────────────────────────────────

describe("fuzzyMatch", () => {
  it("matches empty query to anything", () => {
    assert.ok(fuzzyMatch("", "general"));
  });

  it("matches case-insensitive substring", () => {
    assert.ok(fuzzyMatch("gen", "general"));
    assert.ok(fuzzyMatch("GEN", "general"));
  });

  it("rejects non-matching string", () => {
    assert.ok(!fuzzyMatch("xyz", "general"));
  });

  it("matches token initials", () => {
    // "fa" matches initials of "Founding Agent"
    assert.ok(fuzzyMatch("fa", "Founding Agent"));
  });

  it("matches partial name", () => {
    assert.ok(fuzzyMatch("seo", "SEO Analyst"));
  });

  it("matches hyphenated token initials", () => {
    assert.ok(fuzzyMatch("la", "lead-analyst"));
  });
});

// ── filterItems ─────────────────────────────────────────────────────

describe("filterItems", () => {
  it("returns all items for empty query, sorted by score", () => {
    const result = filterItems("", ITEMS);
    assert.equal(result.length, 4);
    // Should be sorted by score descending
    assert.equal(result[0].id, "dm-founding");
    assert.equal(result[1].id, "ch-general");
  });

  it("filters by substring", () => {
    const result = filterItems("lead", ITEMS);
    assert.equal(result.length, 1);
    assert.equal(result[0].id, "ch-leads");
  });

  it("returns empty for no matches", () => {
    const result = filterItems("nonexistent", ITEMS);
    assert.equal(result.length, 0);
  });

  it("maintains score ordering in results", () => {
    const result = filterItems("a", ITEMS);
    // All items with "a" in name
    for (let i = 1; i < result.length; i++) {
      assert.ok(result[i - 1].score >= result[i].score);
    }
  });
});

// ── QuickSwitcher component ─────────────────────────────────────────

describe("QuickSwitcher", () => {
  it("renders nothing when not open", () => {
    const { lastFrame } = render(
      <QuickSwitcher {...defaultProps} open={false} />,
    );
    assert.equal(lastFrame(), "");
  });

  it("renders switcher header when open", () => {
    const { lastFrame } = render(<QuickSwitcher {...defaultProps} />);
    const output = strip(lastFrame());
    assert.ok(output.includes("Switch to..."));
  });

  it("shows all items when no query", () => {
    const { lastFrame } = render(<QuickSwitcher {...defaultProps} />);
    const output = strip(lastFrame());
    assert.ok(output.includes("Founding Agent"));
    assert.ok(output.includes("general"));
    assert.ok(output.includes("SEO Analyst"));
    assert.ok(output.includes("leads"));
  });

  it("shows channel with # prefix", () => {
    const { lastFrame } = render(<QuickSwitcher {...defaultProps} />);
    const output = strip(lastFrame());
    assert.ok(output.includes("#"));
    assert.ok(output.includes("general"));
  });

  it("shows DM with online/offline dots", () => {
    const { lastFrame } = render(<QuickSwitcher {...defaultProps} />);
    const output = strip(lastFrame());
    // Online: ● , Offline: ○
    assert.ok(output.includes("●"));
    assert.ok(output.includes("○"));
  });

  it("shows unread badge", () => {
    const { lastFrame } = render(<QuickSwitcher {...defaultProps} />);
    const output = strip(lastFrame());
    assert.ok(output.includes("(2)"));
  });

  it("shows keyboard hints", () => {
    const { lastFrame } = render(<QuickSwitcher {...defaultProps} />);
    const output = strip(lastFrame());
    assert.ok(output.includes("navigate"));
    assert.ok(output.includes("Enter select"));
    assert.ok(output.includes("Esc close"));
  });

  it("first item is selected by default", () => {
    const { lastFrame } = render(<QuickSwitcher {...defaultProps} />);
    const output = strip(lastFrame());
    // First item (Founding Agent, score=100) should have > prefix
    assert.ok(output.includes("> "));
  });

  it("shows search placeholder", () => {
    const { lastFrame } = render(<QuickSwitcher {...defaultProps} />);
    const output = strip(lastFrame());
    assert.ok(output.includes("Search channels and DMs"));
  });
});
