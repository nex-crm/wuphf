import {
  type DetailedHTMLProps,
  type HTMLAttributes,
  useEffect,
  useRef,
} from "react";

import "../../styles/rich-artifacts.css";

const ELEMENT_NAME = "rich-artifact-embed";

interface RichArtifactEmbedElement extends HTMLElement {
  setArtifactHTML(html: string): void;
}

// JSX intrinsic-element augmentation for the custom element. React 19 reads
// types from `React.JSX` rather than the global `JSX` namespace, so we have
// to extend both for forward + backward compat across the project's TS setup.
declare module "react" {
  namespace JSX {
    interface IntrinsicElements {
      "rich-artifact-embed": DetailedHTMLProps<
        HTMLAttributes<HTMLElement>,
        HTMLElement
      >;
    }
  }
}

// Define the custom element once per module load. HMR can re-evaluate the
// module so we guard against the "this name is already used" error from
// customElements.define.
if (typeof window !== "undefined" && !customElements.get(ELEMENT_NAME)) {
  class RichArtifactEmbedImpl extends HTMLElement {
    private shadow: ShadowRoot;

    constructor() {
      super();
      this.shadow = this.attachShadow({ mode: "open" });
    }

    setArtifactHTML(html: string): void {
      mountArtifact(this.shadow, html);
    }
  }
  customElements.define(ELEMENT_NAME, RichArtifactEmbedImpl);
}

// SECURITY MODEL — trust boundary:
//
//   Untrusted input:  the `html` argument is agent-generated, fetched via
//                     /notebook/visual-artifacts/{id} from the broker.
//   Broker guarantee: validateRichArtifactHTML at
//                     internal/team/rich_artifact.go has already stripped
//                     <script>, every on* event handler, all external
//                     URLs in href/src/url(), every @import, expression(),
//                     and the document is tagged sanitizerVersion=sandbox-v2.
//
// We never set innerHTML with that string. Path:
//   1. DOMParser.parseFromString(html, "text/html")   ← no script exec
//   2. cloneNode(true) of body children                ← DOM nodes only
//   3. appendChild into the shadow                     ← still DOM nodes
//
// shadow.innerHTML = "" (line below) clears the root with an empty literal
// — safe. style.textContent = rewriteCSS(...) writes a literal string to a
// <style> text node — safe (textContent does not parse HTML). DO NOT
// refactor this to shadow.innerHTML = html or you give up the
// defence-in-depth that the cloneNode path provides on top of the broker
// sanitizer.
function mountArtifact(shadow: ShadowRoot, html: string): void {
  const doc = new DOMParser().parseFromString(html, "text/html");

  shadow.innerHTML = "";

  // 1. Reset that makes the embed behave like a block-level container and
  //    cuts inheritance from the host page (the agent's CSS should fully
  //    own its visual presentation).
  const reset = document.createElement("style");
  reset.textContent =
    ":host{display:block;all:initial;contain:layout style;}" +
    ".artifact-body{display:block;}";
  shadow.appendChild(reset);

  // 2. Author styles, rewritten so document-level selectors hit our wrapper
  //    instead of falling off the shadow boundary.
  for (const style of Array.from(doc.head.querySelectorAll("style"))) {
    const rewritten = document.createElement("style");
    rewritten.textContent = rewriteCSS(style.textContent ?? "");
    shadow.appendChild(rewritten);
  }

  // 3. The body's children become the artifact, wrapped in a single
  //    .artifact-body div that acts as the synthetic "body" the rewritten
  //    body selectors above now target.
  const wrapper = document.createElement("div");
  wrapper.className = "artifact-body";
  for (const child of Array.from(doc.body.childNodes)) {
    wrapper.appendChild(child.cloneNode(true));
  }
  shadow.appendChild(wrapper);
}

// rewriteCSS does two small substitutions so document-level selectors in the
// artifact's CSS still hit something useful inside the shadow root.
// Conservative on purpose: anything fancier should be done with a real CSS
// parser. Tests cover the common shapes the agent emits today.
export function rewriteCSS(css: string): string {
  return css
    .replace(/:root\b/g, ":host")
    .replace(/(^|[\s,{}])body\b/g, "$1.artifact-body")
    .replace(/(^|[\s,{}])html\b/g, "$1.artifact-body");
}

interface RichArtifactEmbedProps {
  title: string;
  html: string;
}

export default function RichArtifactEmbed({
  title,
  html,
}: RichArtifactEmbedProps) {
  const ref = useRef<RichArtifactEmbedElement | null>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.setArtifactHTML(html);
  }, [html]);

  // <figure> wraps the custom element so screen readers expose the title as
  // the accessible name of a figure. Adding role=figure directly on the
  // custom element trips biome's interactive-element heuristic (the "embed"
  // suffix collides with the <embed> built-in). aria-label is mirrored on
  // the custom element so test queries like findByLabelText(title, {
  // selector: "rich-artifact-embed" }) still locate it directly.
  return (
    <figure className="rich-artifact-figure" aria-label={title}>
      <rich-artifact-embed
        ref={ref as unknown as React.Ref<HTMLElement>}
        className="rich-artifact-embed"
        aria-label={title}
        data-testid="rich-artifact-embed"
      />
    </figure>
  );
}
