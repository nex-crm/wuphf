import {
  type DetailedHTMLProps,
  type HTMLAttributes,
  useEffect,
  useRef,
} from "react";
import DOMPurify, { type Config as DOMPurifyConfig } from "dompurify";

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

// SECURITY MODEL — layered, trust boundary:
//
//   Untrusted input:  the `html` argument is agent-generated, fetched via
//                     /notebook/visual-artifacts/{id} from the broker.
//   Layer 1 (server): validateRichArtifactHTML at
//                     internal/team/rich_artifact.go strips <script>, every
//                     on* event handler, all external URLs in href/src/url(),
//                     every @import, expression(), and tags the document
//                     sanitizerVersion=sandbox-v2.
//   Layer 2 (client): DOMPurify with a conservative profile (no scripts, no
//                     event handlers, no javascript:/data: URLs in href/src,
//                     no iframe/object/embed/svg/math) runs HERE before any
//                     DOM is mounted, so a sanitizer-bypass on the server
//                     does not reach the parent origin even via the shadow
//                     boundary.
//   Layer 3 (origin): shadow DOM contains style scope. The cost of leaving
//                     the iframe sandbox is that a bypass would execute in
//                     the parent origin; layers 1 + 2 are the mitigation.
//                     A separate app-level CSP (still missing on the web
//                     proxy at internal/team/broker_web_proxy.go) would add
//                     a third defence by blocking unexpected connect-src
//                     and inline event handlers — tracked as a follow-up.
//
// We never set innerHTML with the agent string. Path:
//   1. DOMPurify.sanitize(html, { RETURN_DOM: true })  ← client sanitiser
//   2. cloneNode(true) of body children                ← DOM nodes only
//   3. appendChild into the shadow                     ← still DOM nodes
//
// shadow.innerHTML = "" (line below) clears the root with an empty literal
// — safe. style.textContent = rewriteCSS(...) writes a literal string to a
// <style> text node — safe (textContent does not parse HTML). DO NOT
// refactor this to shadow.innerHTML = html or you give up layer 2.

// Conservative DOMPurify profile for the BODY fragment of the artifact.
// We deliberately do NOT use WHOLE_DOCUMENT: that mode lets some
// structural elements (notably <iframe srcdoc>) survive in practice; the
// safer pattern is to extract body innerHTML up-front and sanitise it as a
// fragment, which forces every element through ALLOWED_TAGS. Mirrors the
// server-side validateRichArtifactHTML invariants and forbids the same
// scriptable container elements so that a bypass on either layer alone
// cannot reach the parent origin.
const PURIFY_CONFIG: DOMPurifyConfig = {
  RETURN_DOM_FRAGMENT: true,
  WHOLE_DOCUMENT: false,
  FORBID_TAGS: [
    "script",
    "iframe",
    "object",
    "embed",
    "svg",
    "math",
    "form",
    "input",
    "button",
    "textarea",
    "select",
    "option",
    "link",
    "meta",
    "base",
    "frame",
    "frameset",
    "applet",
  ],
  FORBID_ATTR: [
    "srcdoc",
    "formaction",
    "action",
    "ping",
    "background",
    "poster",
    "xlink:href",
  ],
  ALLOW_DATA_ATTR: true,
  ALLOW_UNKNOWN_PROTOCOLS: false,
  // Hooks below add the data:/blob:/relative-only URL allowlist for href/src
  // and the catch-all on* event-handler strip.
};

if (typeof window !== "undefined") {
  // Strip every on* attribute. DOMPurify already drops known event handlers,
  // but this guards future-attribute additions (e.g. onbeforetoggle) by
  // pattern.
  DOMPurify.addHook("uponSanitizeAttribute", (_node, data) => {
    if (data.attrName.startsWith("on")) {
      data.keepAttr = false;
    }
  });
  // Tighten URL schemes for href/src: keep relative paths, #fragments,
  // data:image/* and blob:, drop everything else (especially javascript:).
  DOMPurify.addHook("uponSanitizeAttribute", (_node, data) => {
    if (data.attrName !== "href" && data.attrName !== "src") return;
    const value = (data.attrValue || "").trim();
    const lower = value.toLowerCase();
    if (lower.startsWith("javascript:") || lower.startsWith("vbscript:")) {
      data.keepAttr = false;
      return;
    }
    if (
      lower.startsWith("data:") &&
      !lower.startsWith("data:image/") &&
      !lower.startsWith("data:font/")
    ) {
      data.keepAttr = false;
    }
  });
}

function mountArtifact(shadow: ShadowRoot, html: string): void {
  // Step A: structural parse only. DOMParser is no-execute by spec, so
  // scripts in the input do not run; we just walk the tree to pull <style>
  // tags out of <head> and grab the body's innerHTML. This is the only
  // place the unsanitised string is touched, and we never reattach it to
  // the live DOM in that form.
  const doc = new DOMParser().parseFromString(html, "text/html");
  const headStyles = Array.from(doc.head.querySelectorAll("style"));
  const bodyHTML = doc.body.innerHTML;

  // Step B: Layer 2 client-side sanitiser. Runs over the body fragment
  // (NOT the whole document — see PURIFY_CONFIG comment for why). The
  // broker has already stripped this material once on write; running it
  // again at render time is cheap (sub-millisecond on 16KB artifacts) and
  // means a bypass in the server sanitiser does not become a bypass in
  // the parent origin.
  const sanitizedBody = DOMPurify.sanitize(
    bodyHTML,
    PURIFY_CONFIG,
  ) as unknown as DocumentFragment;

  // Layer 2.5: deterministic post-sweep. Defense-in-depth for cases where
  // the host HTML parser (in tests, jsdom) mis-tokenises adversarial input
  // and DOMPurify's tag walk skips an element it would normally drop. This
  // sweep runs on the already-parsed DOM tree so it cannot be tricked by
  // any of those parser quirks: any element whose name appears in
  // FORBIDDEN_TAG_SWEEP is removed outright. Cheap (single
  // querySelectorAll on a ~16KB tree).
  sweepForbiddenTags(sanitizedBody);
  sweepForbiddenAttributes(sanitizedBody);

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
  //    instead of falling off the shadow boundary. Style content is plain
  //    CSS text — not HTML — and is assigned via textContent, so DOMPurify
  //    is not the right tool for it; the server-side validateRichArtifactCSS
  //    already strips @import, expression(), and url() schemes.
  for (const style of headStyles) {
    const rewritten = document.createElement("style");
    rewritten.textContent = rewriteCSS(style.textContent ?? "");
    shadow.appendChild(rewritten);
  }

  // 3. Append the sanitised body fragment under the synthetic .artifact-body
  //    wrapper. Nothing inside `sanitizedBody` has been re-parsed since
  //    DOMPurify built it, so the shape we mount is exactly what passed
  //    the sanitiser.
  const wrapper = document.createElement("div");
  wrapper.className = "artifact-body";
  wrapper.appendChild(sanitizedBody);
  shadow.appendChild(wrapper);
}

const FORBIDDEN_TAG_SWEEP = [
  "script",
  "iframe",
  "object",
  "embed",
  "svg",
  "math",
  "form",
  "input",
  "button",
  "textarea",
  "select",
  "option",
  "link",
  "meta",
  "base",
  "frame",
  "frameset",
  "applet",
  "noscript",
];

function sweepForbiddenTags(root: ParentNode): void {
  const selector = FORBIDDEN_TAG_SWEEP.join(",");
  for (const el of Array.from(root.querySelectorAll(selector))) {
    el.remove();
  }
}

function sweepForbiddenAttributes(root: ParentNode): void {
  for (const el of Array.from(root.querySelectorAll("*"))) {
    for (const attr of Array.from(el.attributes)) {
      const name = attr.name.toLowerCase();
      if (name.startsWith("on")) {
        el.removeAttribute(attr.name);
        continue;
      }
      if (
        (name === "href" || name === "src" || name === "xlink:href") &&
        isUnsafeURL(attr.value)
      ) {
        el.removeAttribute(attr.name);
      }
    }
  }
}

function isUnsafeURL(raw: string): boolean {
  const value = raw.trim().toLowerCase();
  if (value.startsWith("javascript:") || value.startsWith("vbscript:")) {
    return true;
  }
  // Allow data:image/* and data:font/* only; everything else under data: is
  // either useless to artifacts or a known vector (e.g. data:text/html).
  if (value.startsWith("data:") && !value.startsWith("data:image/") && !value.startsWith("data:font/")) {
    return true;
  }
  return false;
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
