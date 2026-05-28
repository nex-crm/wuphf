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
