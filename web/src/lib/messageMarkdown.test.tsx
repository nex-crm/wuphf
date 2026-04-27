/**
 * Tests for the chat-bubble markdown pipeline. The non-negotiable bar is:
 * no markdown link can produce an <a> with a javascript:/vbscript:/data:
 * href, regardless of how the markdown is structured.
 */

import ReactMarkdown from "react-markdown";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import {
  messageMarkdownComponents,
  messageRemarkPlugins,
} from "./messageMarkdown";

function renderChat(content: string) {
  return render(
    <ReactMarkdown
      remarkPlugins={messageRemarkPlugins}
      components={messageMarkdownComponents}
      skipHtml={true}
    >
      {content}
    </ReactMarkdown>,
  );
}

describe("messageMarkdown — XSS hardening", () => {
  it("neutralises [x](javascript:alert(1))", () => {
    const { container } = renderChat("[click](javascript:alert(1))");
    const anchors = container.querySelectorAll("a");
    for (const a of anchors) {
      const href = a.getAttribute("href") ?? "";
      expect(href.toLowerCase()).not.toMatch(/^javascript:/);
    }
  });

  it("neutralises [x](data:text/html,<script>alert(1)</script>)", () => {
    const { container } = renderChat(
      "[click](data:text/html,<script>alert(1)</script>)",
    );
    const anchors = container.querySelectorAll("a");
    for (const a of anchors) {
      const href = a.getAttribute("href") ?? "";
      expect(href.toLowerCase()).not.toMatch(/^data:text\/html/);
    }
  });

  it("neutralises [x](vbscript:msgbox)", () => {
    const { container } = renderChat("[click](vbscript:msgbox)");
    const anchors = container.querySelectorAll("a");
    for (const a of anchors) {
      const href = a.getAttribute("href") ?? "";
      expect(href.toLowerCase()).not.toMatch(/^vbscript:/);
    }
  });

  it("neutralises whitespace-prefixed javascript: URIs", () => {
    const { container } = renderChat("[click](  javascript:alert(1))");
    const anchors = container.querySelectorAll("a");
    for (const a of anchors) {
      const href = a.getAttribute("href") ?? "";
      expect(href.toLowerCase()).not.toMatch(/javascript:/);
    }
  });

  it("does not render raw HTML <script> tags from agent text", () => {
    const { container } = renderChat("before <script>alert(1)</script> after");
    expect(container.querySelector("script")).toBeNull();
    expect(container.textContent).toContain("alert(1)");
  });

  it("does not render raw HTML event handlers like <img onerror>", () => {
    const { container } = renderChat("<img src=x onerror=alert(1)>");
    // skipHtml on react-markdown v10 drops raw HTML entirely (rather than
    // escaping it to text). The security property is that no live img
    // element exists in the DOM, so the onerror handler can't fire.
    expect(container.querySelectorAll("img").length).toBe(0);
    expect(container.querySelectorAll("script").length).toBe(0);
  });

  it("neutralises ![x](javascript:alert(1)) image markdown", () => {
    const { container } = renderChat("![alt](javascript:alert(1))");
    const imgs = container.querySelectorAll("img");
    for (const img of imgs) {
      const src = img.getAttribute("src") ?? "";
      expect(src.toLowerCase()).not.toMatch(/^javascript:/);
    }
  });

  it("neutralises ![x](data:text/html,...) image markdown", () => {
    const { container } = renderChat(
      "![alt](data:text/html,<script>alert(1)</script>)",
    );
    const imgs = container.querySelectorAll("img");
    for (const img of imgs) {
      const src = img.getAttribute("src") ?? "";
      expect(src.toLowerCase()).not.toMatch(/^data:text\/html/);
    }
  });

  it("neutralises GFM-style autolink <javascript:alert(1)>", () => {
    // remark-gfm autolink literals can produce <a href="javascript:..."> from
    // <scheme:...> syntax. The default urlTransform must strip it.
    const { container } = renderChat("<javascript:alert(1)>");
    const anchors = container.querySelectorAll("a");
    for (const a of anchors) {
      const href = a.getAttribute("href") ?? "";
      expect(href.toLowerCase()).not.toMatch(/^javascript:/);
    }
  });
});

describe("messageMarkdown — link safety preserved for normal URLs", () => {
  it("renders https links with the msg-link class and noreferrer", () => {
    const { container } = renderChat("[ok](https://example.com)");
    const a = container.querySelector("a");
    expect(a).not.toBeNull();
    expect(a?.getAttribute("href")).toBe("https://example.com");
    expect(a?.className).toContain("msg-link");
    expect(a?.getAttribute("rel")).toContain("noopener");
    expect(a?.getAttribute("rel")).toContain("noreferrer");
    expect(a?.getAttribute("target")).toBe("_blank");
  });

  it("renders mailto: links", () => {
    const { container } = renderChat("[email](mailto:nazz@example.com)");
    const a = container.querySelector("a");
    expect(a?.getAttribute("href")).toBe("mailto:nazz@example.com");
  });

  it("renders relative links (/foo)", () => {
    const { container } = renderChat("[doc](/wiki/team/launch)");
    const a = container.querySelector("a");
    expect(a?.getAttribute("href")).toBe("/wiki/team/launch");
  });

  it("renders fragment links (#foo)", () => {
    const { container } = renderChat("[here](#section)");
    const a = container.querySelector("a");
    expect(a?.getAttribute("href")).toBe("#section");
  });
});

describe("messageMarkdown — visual class mapping", () => {
  it("emits msg-h1 for a level-1 heading", () => {
    const { container } = renderChat("# Hello");
    expect(container.querySelector(".msg-h1")?.textContent).toBe("Hello");
  });

  it("emits msg-h2 for a level-2 heading", () => {
    const { container } = renderChat("## Hello");
    expect(container.querySelector(".msg-h2")?.textContent).toBe("Hello");
  });

  it("emits msg-h3 for a level-3 heading", () => {
    const { container } = renderChat("### Hello");
    expect(container.querySelector(".msg-h3")?.textContent).toBe("Hello");
  });

  it("emits msg-blockquote for blockquotes", () => {
    const { container } = renderChat("> quoted");
    const bq = container.querySelector(".msg-blockquote");
    expect(bq).not.toBeNull();
    expect(bq?.textContent).toContain("quoted");
  });

  it("emits msg-codeblock for fenced code", () => {
    const md = "```\nconst x = 1;\n```";
    const { container } = renderChat(md);
    const cb = container.querySelector(".msg-codeblock");
    expect(cb).not.toBeNull();
    expect(cb?.textContent).toContain("const x = 1;");
  });

  it("emits msg-hr for horizontal rule", () => {
    const { container } = renderChat("---");
    expect(container.querySelector("hr.msg-hr")).not.toBeNull();
  });

  it("emits msg-ul + <li> for unordered lists", () => {
    const { container } = renderChat("- one\n- two");
    const ul = container.querySelector("ul.msg-ul");
    expect(ul).not.toBeNull();
    expect(ul?.querySelectorAll("li").length).toBe(2);
  });
});

describe("messageMarkdown — @mentions render as chips", () => {
  it("turns @slug into a mention chip, not a link", () => {
    const { container } = renderChat("hi @nazz how are you");
    const chip = container.querySelector(".mention");
    expect(chip).not.toBeNull();
    expect(chip?.textContent).toBe("@nazz");
    expect(chip?.tagName.toLowerCase()).toBe("span");
    // The mention must render as a span chip; no <a> element of any kind
    // should remain in the document for it.
    expect(container.querySelector("a")).toBeNull();
  });

  it("supports multiple mentions in one message", () => {
    const { container } = renderChat("@pm and @ceo please review");
    const chips = container.querySelectorAll(".mention");
    expect(chips.length).toBe(2);
    expect(chips[0].textContent).toBe("@pm");
    expect(chips[1].textContent).toBe("@ceo");
  });

  it("does not chip an @-address in an email body", () => {
    // The mention regex requires non-alphanumeric prefix; "user@" has 'r'
    // immediately before @ so it should NOT be turned into a chip.
    const { container } = renderChat("send to user@example.com");
    expect(container.querySelector(".mention")).toBeNull();
  });
});
