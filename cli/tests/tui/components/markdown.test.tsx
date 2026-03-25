import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import React from "react";
import { render, cleanup } from "ink-testing-library";
import { Markdown } from "../../../src/tui/components/markdown.js";

function strip(s: string): string {
  return s.replace(/\x1b\[[0-9;]*m/g, "");
}

afterEach(() => {
  cleanup();
});

// ─── Headings ───

describe("Markdown – Headings", () => {
  it("renders h1 heading as bold text", () => {
    const { lastFrame } = render(<Markdown content="# Hello World" />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Hello World"), "should render heading text");
  });

  it("renders h2 heading", () => {
    const { lastFrame } = render(<Markdown content="## Sub Heading" />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Sub Heading"), "should render h2 text");
  });

  it("renders h3 heading", () => {
    const { lastFrame } = render(<Markdown content="### Third Level" />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Third Level"), "should render h3 text");
  });
});

// ─── Bold & Inline Code ───

describe("Markdown – Inline formatting", () => {
  it("renders bold text", () => {
    const { lastFrame } = render(<Markdown content="This is **bold** text" />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("bold"), "should include bold word");
    assert.ok(!frame.includes("**"), "should strip bold markers");
  });

  it("renders inline code", () => {
    const { lastFrame } = render(<Markdown content="Use `npm install` here" />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("npm install"), "should include code text");
    assert.ok(!frame.includes("`"), "should strip backticks");
  });

  it("renders italic text", () => {
    const { lastFrame } = render(<Markdown content="This is *italic* text" />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("italic"), "should include italic word");
    assert.ok(!frame.includes("*italic*"), "should strip italic markers");
  });

  it("handles mixed inline formatting", () => {
    const { lastFrame } = render(
      <Markdown content="Run **npm** with `--force` flag" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("npm"), "should show bold text");
    assert.ok(frame.includes("--force"), "should show code text");
    assert.ok(!frame.includes("**"), "should strip bold markers");
    assert.ok(!frame.includes("`"), "should strip backtick markers");
  });
});

// ─── Code Blocks ───

describe("Markdown – Code blocks", () => {
  it("renders fenced code block content", () => {
    const md = "```\nconst x = 1;\nconst y = 2;\n```";
    const { lastFrame } = render(<Markdown content={md} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("const x = 1;"), "should show first code line");
    assert.ok(frame.includes("const y = 2;"), "should show second code line");
  });

  it("renders code block with language tag", () => {
    const md = "```typescript\nconst a: number = 5;\n```";
    const { lastFrame } = render(<Markdown content={md} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("typescript"), "should show language tag");
    assert.ok(frame.includes("const a: number = 5;"), "should show code content");
  });

  it("renders code block inside a bordered box", () => {
    const md = "```\nhello\n```";
    const { lastFrame } = render(<Markdown content={md} />);
    const frame = lastFrame() ?? "";
    // Ink round border uses ╭ or similar chars
    assert.ok(
      frame.includes("╭") || frame.includes("┌") || frame.includes("│"),
      "should have border characters",
    );
  });
});

// ─── Bullet Lists ───

describe("Markdown – Bullet lists", () => {
  it("renders bullet list items with markers", () => {
    const md = "- First item\n- Second item\n- Third item";
    const { lastFrame } = render(<Markdown content={md} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("First item"), "should show first item");
    assert.ok(frame.includes("Second item"), "should show second item");
    assert.ok(frame.includes("Third item"), "should show third item");
    assert.ok(frame.includes("●"), "should show bullet marker");
  });

  it("strips the dash prefix", () => {
    const md = "- Item one";
    const { lastFrame } = render(<Markdown content={md} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(!frame.includes("- Item"), "should not show raw dash prefix");
  });
});

// ─── Ordered Lists ───

describe("Markdown – Ordered lists", () => {
  it("renders ordered list with numbers", () => {
    const md = "1. Alpha\n2. Beta\n3. Gamma";
    const { lastFrame } = render(<Markdown content={md} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("1."), "should show number 1");
    assert.ok(frame.includes("2."), "should show number 2");
    assert.ok(frame.includes("Alpha"), "should show first item");
    assert.ok(frame.includes("Gamma"), "should show third item");
  });
});

// ─── Blockquotes ───

describe("Markdown – Blockquotes", () => {
  it("renders blockquote with pipe prefix", () => {
    const md = "> This is a quote\n> Second line";
    const { lastFrame } = render(<Markdown content={md} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("│"), "should show pipe border");
    assert.ok(frame.includes("This is a quote"), "should show quote text");
    assert.ok(frame.includes("Second line"), "should show second quote line");
  });

  it("strips > prefix from content", () => {
    const md = "> Quoted text";
    const { lastFrame } = render(<Markdown content={md} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(!frame.includes("> Quoted"), "should not show raw > prefix");
  });
});

// ─── Paragraphs ───

describe("Markdown – Paragraphs", () => {
  it("renders plain paragraph text", () => {
    const { lastFrame } = render(<Markdown content="Just some text here." />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Just some text here."), "should show paragraph");
  });

  it("joins consecutive lines into one paragraph", () => {
    const md = "Line one\nLine two\nLine three";
    const { lastFrame } = render(<Markdown content={md} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Line one"), "should include first line");
    assert.ok(frame.includes("Line two"), "should include second line");
  });
});

// ─── Mixed content ───

describe("Markdown – Mixed content", () => {
  it("renders a full document with multiple block types", () => {
    const md = [
      "# Title",
      "",
      "Some intro text with **bold** words.",
      "",
      "## Features",
      "",
      "- Fast",
      "- Lightweight",
      "- No deps",
      "",
      "> Note: this is important",
      "",
      "```js",
      "console.log('hello');",
      "```",
      "",
      "1. First step",
      "2. Second step",
    ].join("\n");

    const { lastFrame } = render(<Markdown content={md} />);
    const frame = strip(lastFrame() ?? "");

    assert.ok(frame.includes("Title"), "should render heading");
    assert.ok(frame.includes("bold"), "should render bold inline");
    assert.ok(frame.includes("Features"), "should render h2");
    assert.ok(frame.includes("Fast"), "should render bullet");
    assert.ok(frame.includes("│"), "should render blockquote border");
    assert.ok(frame.includes("console.log"), "should render code block");
    assert.ok(frame.includes("1."), "should render ordered list");
  });

  it("handles empty content", () => {
    const { lastFrame } = render(<Markdown content="" />);
    const frame = lastFrame() ?? "";
    // Should not crash, just render empty
    assert.ok(typeof frame === "string", "should render without error");
  });
});
