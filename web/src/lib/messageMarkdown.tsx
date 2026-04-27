/**
 * Markdown pipeline for chat-bubble agent messages.
 *
 * Mirrors the wiki pipeline (wikiMarkdownConfig.tsx) but with a chat-tuned
 * plugin set and CSS class mapping that preserves the legacy formatTrusted
 * visual: msg-h{1,2,3}, msg-codeblock, msg-blockquote, msg-link, msg-ul/ol.
 *
 * SECURITY: This replaces the legacy regex-based formatMarkdown that built
 * its own HTML strings and sent them through dangerouslySetInnerHTML. That
 * path was vulnerable to XSS via markdown links with javascript:/data:
 * URIs because escapeHtml only neutralises < > " &, leaving the URL scheme
 * untouched. ReactMarkdown's default urlTransform strips javascript:,
 * vbscript:, and most data: URIs; we add a belt-and-suspenders allowlist
 * inside the anchor renderer below.
 *
 * @mentions:
 *   Mentions are mapped to mdast link nodes with a synthetic `wuphf-mention:`
 *   URL scheme. The anchor renderer detects that scheme and emits a styled
 *   <span class="mention"> chip — never an <a> with a clickable href.
 */

import type { ComponentProps, ReactElement, ReactNode } from "react";
import type { Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import type { PluggableList } from "unified";

// Mirrors the broker-side mention pattern in internal/team/broker.go.
// Keep in sync with web/src/lib/mentions.tsx.
const MENTION_RE = /(?:^|[^a-zA-Z0-9_])@([a-z0-9][a-z0-9-]{1,29})\b/g;

// Defense-in-depth allowlist applied to anchor href values. ReactMarkdown's
// defaultUrlTransform already strips javascript:, vbscript:, and data:
// (except safe image variants); this re-checks because url handling is too
// important to depend on a single layer.
const SAFE_URL_RE = /^(https?:|mailto:|tel:|\/|#|\.\.?\/|\?)/i;

// ── AST types (minimal mdast surface for the remark plugin) ──

interface MdTextNode {
  type: "text";
  value: string;
}

interface MdLinkNode {
  type: "link";
  url: string;
  children: MdAnyNode[];
  data?: { hProperties?: Record<string, string> };
}

type MdAnyNode =
  | MdTextNode
  | MdLinkNode
  | { type: string; children?: MdAnyNode[]; value?: string };

interface MdParent {
  children: MdAnyNode[];
}

/**
 * Remark plugin that rewrites `@slug` substrings inside text nodes into
 * link nodes with a `wuphf-mention:slug` URL. The renderer below converts
 * those into mention chips. Following the wikilink plugin pattern.
 */
export function mentionRemarkPlugin() {
  return function plugin() {
    return function transformer(tree: unknown) {
      walk(tree as MdAnyNode, (parent) => {
        const children = parent.children;
        for (let i = 0; i < children.length; i++) {
          const child = children[i];
          if (
            child.type !== "text" ||
            typeof (child as MdTextNode).value !== "string"
          )
            continue;
          const value = (child as MdTextNode).value;
          if (!value.includes("@")) continue;

          const replacements = buildMentionReplacements(value);
          if (replacements.length === 0) continue;
          children.splice(i, 1, ...replacements);
          i += replacements.length - 1;
        }
      });
    };
  };
}

function buildMentionReplacements(value: string): MdAnyNode[] {
  // Use matchAll so we don't have to manage a stateful lastIndex on a /g regex.
  const matches = [...value.matchAll(MENTION_RE)];
  if (matches.length === 0) return [];
  const out: MdAnyNode[] = [];
  let cursor = 0;
  for (const m of matches) {
    const slug = m[1];
    if (!slug) continue;
    // The regex captures one optional prefix char (boundary). Find the actual
    // '@' position so we slice the surrounding text correctly.
    const matchStart = m.index ?? 0;
    const atIndex = value.indexOf(`@${slug}`, matchStart);
    if (atIndex === -1) continue;
    if (atIndex > cursor) {
      out.push({ type: "text", value: value.slice(cursor, atIndex) });
    }
    // Use a safe, no-op fragment URL and a data attribute as the discriminator.
    // ReactMarkdown's defaultUrlTransform passes through #-only URLs, and the
    // anchor renderer below checks data-wuphf-mention to swap to a chip.
    out.push({
      type: "link",
      url: "#",
      children: [{ type: "text", value: `@${slug}` }],
      data: {
        hProperties: {
          "data-wuphf-mention": "true",
          "data-slug": slug,
        },
      },
    });
    cursor = atIndex + slug.length + 1; // +1 for the @
  }
  if (cursor === 0) return [];
  if (cursor < value.length) {
    out.push({ type: "text", value: value.slice(cursor) });
  }
  return out;
}

function walk(node: MdAnyNode, onParent: (parent: MdParent) => void) {
  const maybeParent = node as { children?: MdAnyNode[]; type?: string };
  const children = maybeParent.children;
  if (!Array.isArray(children)) return;
  // Don't transform text inside link nodes:
  //  - the user wrote link text deliberately; chipping @slug there would surprise them
  //  - more importantly, we'd recurse into the synthetic mention links we
  //    just inserted (whose text is "@slug"), causing infinite recursion.
  if (maybeParent.type === "link") return;
  onParent(node as MdParent);
  const snapshot = [...(maybeParent.children || [])];
  for (const child of snapshot) {
    if (child && typeof child === "object" && "children" in child) {
      walk(child as MdAnyNode, onParent);
    }
  }
}

// ── Plugin lists ──

/** Remark plugins for chat messages: GFM autolinks/strikethrough + @-mentions. */
export const messageRemarkPlugins: PluggableList = [
  remarkGfm,
  mentionRemarkPlugin(),
];

// ── Component overrides ──

type AnchorProps = ComponentProps<"a">;

/**
 * Returns true if the URL is safe to put in an <a href>. ReactMarkdown's
 * defaultUrlTransform already strips javascript:/vbscript:/most-data: schemes;
 * this is the second layer.
 */
function isSafeHref(href: string | undefined): boolean {
  if (!href) return false;
  return SAFE_URL_RE.test(href.trim());
}

/**
 * Component overrides that preserve the legacy chat-bubble visual classes.
 * Block-level elements use <div> with msg-* classes (the legacy rendering)
 * rather than browser-default <h1>/<blockquote>/etc., so existing CSS in
 * web/src/styles/messages.css continues to apply unchanged.
 */
export const messageMarkdownComponents: Partial<Components> = {
  a: (props: AnchorProps): ReactElement => {
    const { href, children, ...rest } = props;
    const record = rest as Record<string, unknown>;
    if (record["data-wuphf-mention"] === "true") {
      // Mention chip — never a navigable link.
      return <span className="mention">{children}</span>;
    }
    const safe = isSafeHref(href) ? href : undefined;
    return (
      <a
        {...rest}
        href={safe}
        className="msg-link"
        target="_blank"
        rel="noopener noreferrer"
      >
        {children}
      </a>
    );
  },

  h1: ({ children }): ReactElement => (
    <div className="msg-h1">{children as ReactNode}</div>
  ),
  h2: ({ children }): ReactElement => (
    <div className="msg-h2">{children as ReactNode}</div>
  ),
  h3: ({ children }): ReactElement => (
    <div className="msg-h3">{children as ReactNode}</div>
  ),
  h4: ({ children }): ReactElement => (
    <div className="msg-h3">{children as ReactNode}</div>
  ),
  h5: ({ children }): ReactElement => (
    <div className="msg-h3">{children as ReactNode}</div>
  ),
  h6: ({ children }): ReactElement => (
    <div className="msg-h3">{children as ReactNode}</div>
  ),

  blockquote: ({ children }): ReactElement => (
    <div className="msg-blockquote">{children as ReactNode}</div>
  ),

  hr: (): ReactElement => <hr className="msg-hr" />,

  ul: ({ children }): ReactElement => (
    <ul className="msg-ul">{children as ReactNode}</ul>
  ),
  ol: ({ children }): ReactElement => (
    <ol className="msg-ol">{children as ReactNode}</ol>
  ),

  pre: ({ children }): ReactElement => (
    <div className="msg-codeblock">{children as ReactNode}</div>
  ),

  // Inline code stays as <code> (default rendering is fine — fenced blocks
  // get the .msg-codeblock wrapper from the pre override above).

  // Paragraphs in the chat bubble historically rendered as <span>+<br/> to
  // preserve inline flow inside the bubble. ReactMarkdown wraps in <p> by
  // default; we override to keep the legacy DOM shape.
  p: ({ children }): ReactElement => (
    <span>
      {children as ReactNode}
      <br />
    </span>
  ),
};
