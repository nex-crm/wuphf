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
//                     /visual-artifacts/{id} from the broker.
//   Layer 1 (server): validateRichArtifactHTML at
//                     internal/team/rich_artifact.go strips <script>, every
//                     on* event handler, all external URLs in href/src/url(),
//                     every @import, expression(), and tags the document
//                     sanitizerVersion=sandbox-v2.
//   Layer 2 (client): DOMPurify with a conservative profile (no scripts, no
//                     event handlers, no javascript:/data: URLs in href/src,
//                     no iframe/object/embed/math) runs HERE before any
//                     DOM is mounted, so a sanitizer-bypass on the server
//                     does not reach the parent origin even via the shadow
//                     boundary. SVG is intentionally allowed (safe subset
//                     via USE_PROFILES.svg/svgFilters) so agent-emitted
//                     diagrams render; <script> and <foreignObject> inside
//                     SVG are still stripped by both the profile and the
//                     deterministic post-sweep.
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
  // Enable HTML + safe SVG subset (shapes, paths, gradients, filters). The
  // svg/svgFilters profiles explicitly exclude <script> and <foreignObject>;
  // we still belt-and-suspenders them in FORBIDDEN_TAG_SWEEP below in case
  // a parser quirk (jsdom in tests, or future browser bugs) sneaks one
  // past the tag walker.
  USE_PROFILES: { html: true, svg: true, svgFilters: true },
  FORBID_TAGS: [
    "script",
    "iframe",
    "object",
    "embed",
    "math",
    "foreignObject",
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
    // SMIL animation tags can fire JS via the SVG animation events. We do
    // not need them for diagrams; drop them for now.
    "animate",
    "animateTransform",
    "animateMotion",
    "set",
  ],
  FORBID_ATTR: [
    "srcdoc",
    "formaction",
    "action",
    "ping",
    "background",
    "poster",
    // xlink:href is allowed (SVG <use> needs it). The uponSanitizeAttribute
    // hook below rejects javascript:/data:text/* schemes on it, mirroring
    // the href/src checks.
  ],
  ALLOW_DATA_ATTR: true,
  ALLOW_UNKNOWN_PROTOCOLS: false,
  // Hooks below add the data:/blob:/relative-only URL allowlist for href/src
  // and the catch-all on* event-handler strip.
};

// isAllowedArtifactURL is the single source of truth for which href/src/
// xlink:href values may survive sanitisation. It is intentionally a strict
// allowlist (reject by default) so the hook and the deterministic post-sweep
// in isUnsafeURL cannot drift apart. Allowed, and ONLY these:
//
//   - fragment refs:        "#defId"          (SVG <use>, in-page anchors)
//   - true relative paths:  "/x", "./x", "../x", "img/x.png" (no scheme,
//                           no protocol-relative "//host")
//   - data:image/* EXCEPT data:image/svg*     (svg can carry inline script)
//   - data:font/*
//
// Everything else is rejected: any explicit scheme (http:, https:, mailto:,
// tel:, javascript:, vbscript:, ftp:, ...) and protocol-relative "//host".
// Rejecting http/https here is deliberate — external network refs in an
// agent-authored artifact are an exfiltration / tracking vector, and the
// artifact renders inside the parent origin (no iframe), so there is no
// origin isolation backstop.
function isAllowedArtifactURL(raw: string): boolean {
  const value = (raw ?? "").trim();
  if (value === "") return false;
  const lower = value.toLowerCase();

  // Fragment-only reference.
  if (lower.startsWith("#")) return true;

  // Protocol-relative URLs ("//evil.test/x") inherit the page scheme and hit
  // the network — reject before the scheme check (they have no colon).
  if (lower.startsWith("//")) return false;

  // data: URLs — only non-SVG images and fonts.
  if (lower.startsWith("data:")) {
    const safeImage =
      lower.startsWith("data:image/") && !lower.startsWith("data:image/svg");
    const safeFont = lower.startsWith("data:font/");
    return safeImage || safeFont;
  }

  // Any explicit scheme (token followed by ':') is rejected. A scheme is an
  // ASCII letter followed by letters/digits/+/-/. up to the first ':'. We
  // check before any '/', '?', or '#' so "img/a:b.png" (a path with a colon
  // in a later segment) is NOT mistaken for a scheme.
  const schemeMatch = /^[a-z][a-z0-9+.-]*:/.exec(lower);
  if (schemeMatch) return false;

  // No scheme, not protocol-relative, not a fragment, not a data: URL: it is
  // a true relative path ("/x", "./x", "../x", "img/x.png", "a:later/seg" is
  // handled above as a non-scheme because the ':' is past the first '/').
  return true;
}

if (typeof window !== "undefined") {
  // Strip every on* attribute. DOMPurify already drops known event handlers,
  // but this guards future-attribute additions (e.g. onbeforetoggle) by
  // pattern.
  DOMPurify.addHook("uponSanitizeAttribute", (_node, data) => {
    if (data.attrName.startsWith("on")) {
      data.keepAttr = false;
    }
  });
  // Tighten URL schemes for href/src/xlink:href: keep ONLY relative paths,
  // #fragments, data:image/* (excluding SVG which can carry script) and
  // data:font/*; drop everything else — including http:/https:/mailto: and
  // protocol-relative "//host". xlink:href is included because SVG <use>
  // reaches sibling <defs> via xlink:href="#id" — internal fragment refs are
  // exactly what we want to keep, and they pass the same gate as href/src.
  // The post-sweep isUnsafeURL mirrors this allowlist via the same helper.
  DOMPurify.addHook("uponSanitizeAttribute", (_node, data) => {
    if (
      data.attrName !== "href" &&
      data.attrName !== "src" &&
      data.attrName !== "xlink:href"
    ) {
      return;
    }
    if (!isAllowedArtifactURL(data.attrValue || "")) {
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
  //
  //    We also copy attributes from the original <html> and <body> elements
  //    onto the wrapper so selectors that key off body/html state still
  //    match after the rewriteCSS substitution. Without this,
  //    `body.dark { ... }` or `html[dir="rtl"] { ... }` rewrites correctly
  //    on the CSS side but has nothing to bind to in the DOM. attributes
  //    are pulled from the unsanitised doc head/body (read-only, no script
  //    execution), filtered through copyHostAttributes to drop anything
  //    that could carry behaviour (style URLs, event handlers).
  const wrapper = document.createElement("div");
  wrapper.className = "artifact-body";
  copyHostAttributes(wrapper, doc.documentElement);
  copyHostAttributes(wrapper, doc.body);
  wrapper.appendChild(sanitizedBody);
  shadow.appendChild(wrapper);
}

// copyHostAttributes lifts class/id/dir/lang/data-*/aria-* attributes from
// the source element onto the wrapper. Anything that could execute (on*)
// or fetch (src, href, style with url()) is skipped — the sanitiser is the
// authority for those. The "class" attribute is merged with the existing
// .artifact-body class rather than replacing it.
const HOST_ATTR_ALLOWLIST = new Set(["id", "dir", "lang", "title", "role"]);

function isHostAttrAllowed(name: string): boolean {
  return (
    HOST_ATTR_ALLOWLIST.has(name) ||
    name.startsWith("data-") ||
    name.startsWith("aria-")
  );
}

function copyHostAttributes(target: HTMLElement, source: Element | null): void {
  if (!source) return;
  for (const attr of Array.from(source.attributes)) {
    const name = attr.name.toLowerCase();
    if (name === "class") {
      mergeClasses(target, attr.value);
    } else if (isHostAttrAllowed(name)) {
      target.setAttribute(attr.name, attr.value);
    }
    // Everything else (on*, style, src, href, xlink:href, etc.) is
    // dropped silently — the sanitiser owns those decisions.
  }
}

function mergeClasses(target: HTMLElement, raw: string): void {
  for (const cls of raw.split(/\s+/)) {
    if (cls) target.classList.add(cls);
  }
}

// Deterministic post-sweep targets. SVG itself is intentionally NOT here —
// we want the safe SVG subset (shapes, paths, gradients, filters) to render
// — but the dangerous SVG children (script, foreignObject which can smuggle
// HTML, SMIL animation tags which can trigger JS event handlers) ARE swept
// so a parser quirk cannot leave one behind.
const FORBIDDEN_TAG_SWEEP = [
  "script",
  "iframe",
  "object",
  "embed",
  "math",
  "foreignObject",
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
  "animate",
  "animateTransform",
  "animateMotion",
  "set",
];

function sweepForbiddenTags(root: ParentNode): void {
  // querySelectorAll matches lower-cased tag names in HTML documents but
  // SVG elements like <foreignObject> are namespaced and case-sensitive
  // when parsed inside an <svg>. Run both selectors to cover both shapes.
  const selectors = FORBIDDEN_TAG_SWEEP.map((tag) => tag.toLowerCase())
    .concat(FORBIDDEN_TAG_SWEEP)
    .join(",");
  for (const el of Array.from(root.querySelectorAll(selectors))) {
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

// isUnsafeURL is the deterministic post-sweep mirror of the
// uponSanitizeAttribute hook. It MUST stay in lockstep with the hook, so it
// delegates to the same isAllowedArtifactURL allowlist rather than
// re-deriving its own (looser) scheme checks. Anything the allowlist does
// not explicitly permit is unsafe.
function isUnsafeURL(raw: string): boolean {
  return !isAllowedArtifactURL(raw);
}

// rewriteCSS does two small substitutions so document-level selectors in the
// artifact's CSS still hit something useful inside the shadow root.
// Conservative on purpose: anything fancier should be done with a real CSS
// parser. Tests cover the common shapes the agent emits today.
export function rewriteCSS(css: string): string {
  return (
    css
      .replace(/:root\b/g, ":host")
      .replace(/(^|[\s,{}])body\b/g, "$1.artifact-body")
      .replace(/(^|[\s,{}])html\b/g, "$1.artifact-body")
      // After the two substitutions above, `html body` becomes
      // `.artifact-body .artifact-body` which never matches (only one
      // synthetic wrapper exists). Collapse the duplicate descendant chain
      // back to a single class so common resets like `html body { margin: 0 }`
      // still apply to the artifact.
      .replace(/\.artifact-body(\s+\.artifact-body)+/g, ".artifact-body")
  );
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
