import { describe, expect, it } from "vitest";

import { formatMarkdown } from "./markdown";

// XSS regression sweep specific to local-LLM content shapes. The
// general link sanitizer is covered below; these target the new
// sources from feat/local-llms (chat-template markers leaking,
// JSON-shaped content the parser failed to extract, model output
// that mentions `<script>` mid-prose). MessageBubble renders all of
// these via formatMarkdown — pinning that the helper escapes them
// closes the trust-comment refresh from the v6 security review.
describe("formatMarkdown — local-LLM content shapes", () => {
  it("escapes chat-template markers like <|im_end|> as text", () => {
    const out = formatMarkdown("Hello world <|im_end|>");
    expect(out).toContain("&lt;|im_end|&gt;");
    expect(out).not.toContain("<|im_end|>");
  });

  it("escapes a literal <script> mention from a chatty model", () => {
    const out = formatMarkdown(
      "Reminder: never paste <script>alert(1)</script> into prod.",
    );
    expect(out).toContain("&lt;script&gt;");
    expect(out).not.toContain("<script>");
  });

  it("escapes raw HTML embedded by a model 'helpfully' rendering output", () => {
    const out = formatMarkdown(
      "Here is the result: <img src=x onerror=alert(1) /> — good luck!",
    );
    expect(out).not.toMatch(/<img\s/);
    expect(out).toContain("&lt;img");
  });

  it("renders JSON-shaped content as text without parsing or executing", () => {
    // The runner has a backstop that replaces unparsed tool-call
    // shapes with a friendly hint, but if a model emits real-looking
    // JSON in a chat reply (inside a code block), it still renders
    // as plain text — never as live HTML.
    const out = formatMarkdown(
      'Here is your record: `{"name":"x","arguments":{"y":1}}`',
    );
    expect(out).toContain("&quot;name&quot;");
  });
});

describe("formatMarkdown link sanitization", () => {
  it("renders http and https URLs as links", () => {
    expect(formatMarkdown("[x](https://example.com)")).toContain(
      'href="https://example.com"',
    );
    expect(formatMarkdown("[x](http://example.com)")).toContain(
      'href="http://example.com"',
    );
  });

  it("rejects javascript: scheme", () => {
    const out = formatMarkdown("[x](javascript:alert(1))");
    expect(out).not.toContain("href=");
    expect(out).toContain('class="msg-link"');
  });

  it("rejects data: URLs", () => {
    const out = formatMarkdown("[x](data:text/html,hello)");
    expect(out).not.toContain("href=");
  });

  it("rejects vbscript: scheme", () => {
    const out = formatMarkdown("[x](vbscript:msgbox(1))");
    expect(out).not.toContain("href=");
  });

  it("rejects leading-space + uppercased javascript:", () => {
    const out = formatMarkdown("[x]( JAVASCRIPT:alert(1))");
    expect(out).not.toContain("href=");
  });

  it("rejects HTML-entity encoded scheme bypass", () => {
    // After escapeHtml, `&#x6A;avascript:` becomes `&amp;#x6A;avascript:`
    // which doesn't match any safe prefix.
    const out = formatMarkdown("[x](&#x6A;avascript:alert(1))");
    expect(out).not.toContain("href=");
  });

  it("rejects protocol-relative // URLs", () => {
    const out = formatMarkdown("[x](//evil.com/path)");
    expect(out).not.toContain("href=");
  });

  it("renders internal absolute paths as links", () => {
    expect(formatMarkdown("[x](/threads/abc)")).toContain(
      'href="/threads/abc"',
    );
  });

  it("renders hash anchors as links", () => {
    expect(formatMarkdown("[x](#section)")).toContain('href="#section"');
  });

  it("renders mailto links (scheme accepted as safe)", () => {
    // Note: existing @-mention regex post-processes href content, so the
    // rendered URL contains a span around `@example`. We only assert the
    // mailto: scheme is recognized and the result is a link, not that the
    // mailto URL is preserved verbatim — that's a pre-existing quirk in
    // formatInline ordering, out of scope for this PR.
    const out = formatMarkdown("[x](mailto:hi@example.com)");
    expect(out).toContain('class="msg-link"');
    expect(out).toContain('href="mailto:hi');
  });

  it("emits rel='noopener noreferrer' on safe links", () => {
    const out = formatMarkdown("[x](https://example.com)");
    expect(out).toContain('rel="noopener noreferrer"');
  });
});
