/**
 * Markdown -> HTML for the Tiptap wiki editor's initial document.
 *
 * The pipeline is `remark-parse -> remark-gfm -> remark-rehype ->
 * rehype-stringify`, with three WUPHF-specific transforms layered on top so
 * the editor's starting HTML matches what the live preview
 * (`wikiMarkdownConfig`) and the wiki conventions expect:
 *
 *   1. `[[slug]]` / `[[slug|Display]]` wikilinks become anchor nodes carrying
 *      `data-wikilink` / `data-slug`, marked `wk-broken` when an optional
 *      resolver reports the target missing. Reuses `parseWikiLinkInner` so the
 *      grammar matches the textarea + preview exactly.
 *   2. GFM footnote references and definitions are flattened back to their
 *      literal source text (`[^1]` / `[^1]: Title - URL`). We do NOT want the
 *      WYSIWYG to consume citations into a `<sup>` + footnote section, because
 *      `wikiHtmlToMarkdown` could not then reproduce the original markdown.
 *      Showing `[^1]` literally in the editor is the accepted trade.
 *   3. Math (`$…$` / `$$…$$`) is left untouched as literal text — the editor's
 *      input rule converts it on typing; on load it stays text.
 *
 * Fenced code blocks keep their info string (so ```fact / ```decision / ```js
 * all survive as `<pre><code class="language-X">`). Callouts (`> [!note]`)
 * stay blockquotes. Tables, task lists, and strikethrough come from remark-gfm.
 */

import DOMPurify, { type Config as DOMPurifyConfig } from "dompurify";
import rehypeStringify from "rehype-stringify";
import remarkGfm from "remark-gfm";
import remarkParse from "remark-parse";
import remarkRehype from "remark-rehype";
import { unified } from "unified";

import { parseWikiLinkInner } from "@/lib/wikilink";

// ── Minimal mdast surface used by the transforms ───────────────────────────

interface MdPosition {
  start: { offset?: number };
  end: { offset?: number };
}

interface MdNode {
  type: string;
  value?: string;
  url?: string;
  children?: MdNode[];
  identifier?: string;
  position?: MdPosition;
  data?: {
    hName?: string;
    hProperties?: Record<string, string>;
    hChildren?: Array<{ type: string; value: string }>;
  };
}

/**
 * A child mapper returns the nodes that should replace `child`, or `null` to
 * leave it untouched. Returning an array lets a single child expand into many
 * (e.g. a text node carrying several wikilinks).
 */
type ChildMapper = (child: MdNode) => MdNode[] | null;

/** Replace each child in-place per the mapper, expanding arrays inline. */
function mapChildren(children: MdNode[], map: ChildMapper): void {
  for (let i = 0; i < children.length; i++) {
    const replacements = map(children[i]);
    if (replacements === null) continue;
    children.splice(i, 1, ...replacements);
    i += replacements.length - 1;
  }
}

/** Turn one footnote node into its literal-text replacement, or null. */
function footnoteToLiteral(source: string, child: MdNode): MdNode[] | null {
  if (child.type === "footnoteReference") {
    const literal = sliceNode(source, child);
    // Inline: a bare text node carrying `[^id]`.
    return literal === null ? null : [{ type: "text", value: literal }];
  }
  if (child.type === "footnoteDefinition") {
    const literal = sliceNode(source, child);
    // Block: a paragraph so it renders on its own line and turndown brings it
    // back as a standalone block.
    return literal === null
      ? null
      : [{ type: "paragraph", children: [{ type: "text", value: literal }] }];
  }
  return null;
}

/**
 * Replace GFM footnote nodes with their literal source text so citations
 * round-trip losslessly. Slicing the original markdown by node offset keeps
 * the exact bytes (`[^1]: Title - http://x.com`) rather than reconstructing
 * from the parsed children, where the URL has already become a link node.
 */
function literalFootnotesPlugin(source: string) {
  return function plugin() {
    return function transformer(tree: unknown): void {
      walk(tree as MdNode, (node) => {
        if (Array.isArray(node.children)) {
          mapChildren(node.children, (child) =>
            footnoteToLiteral(source, child),
          );
        }
      });
    };
  };
}

/** Turn a text node containing `[[...]]` into wikilink replacement nodes. */
function textToWikilinks(
  child: MdNode,
  resolver?: (slug: string) => boolean,
): MdNode[] | null {
  if (child.type !== "text" || typeof child.value !== "string") return null;
  if (!child.value.includes("[[")) return null;
  const replacements = buildWikilinkNodes(child.value, resolver);
  return replacements.length === 0 ? null : replacements;
}

/**
 * Rewrite `[[slug]]` / `[[slug|Display]]` inside text nodes into link nodes
 * carrying the WUPHF wikilink data-attributes. Mirrors the preview's
 * `wikiLinkRemarkPlugin`, but the resolver is optional here — when omitted no
 * link is flagged broken.
 */
function wikilinkPlugin(resolver?: (slug: string) => boolean) {
  return function plugin() {
    return function transformer(tree: unknown): void {
      walk(tree as MdNode, (node) => {
        if (Array.isArray(node.children)) {
          mapChildren(node.children, (child) =>
            textToWikilinks(child, resolver),
          );
        }
      });
    };
  };
}

function buildWikilinkNodes(
  value: string,
  resolver?: (slug: string) => boolean,
): MdNode[] {
  const re = /\[\[([^\]\n]+)\]\]/g;
  const out: MdNode[] = [];
  let lastIndex = 0;
  let changed = false;
  let match = re.exec(value);
  while (match !== null) {
    const link = parseWikiLinkInner(match[1]);
    const { lastIndex: nextLastIndex } = re;
    if (!link) {
      match = re.exec(value);
      continue;
    }
    changed = true;
    if (match.index > lastIndex) {
      out.push({ type: "text", value: value.slice(lastIndex, match.index) });
    }
    const broken = resolver ? !resolver(link.slug) : false;
    out.push({
      type: "link",
      url: `#/wiki/${encodeURI(link.slug)}`,
      children: [{ type: "text", value: link.display }],
      data: {
        hProperties: {
          "data-wikilink": "true",
          "data-slug": link.slug,
          className: broken ? "wk-wikilink wk-broken" : "wk-wikilink",
        },
      },
    });
    lastIndex = nextLastIndex;
    match = re.exec(value);
  }
  if (!changed) return [];
  if (lastIndex < value.length) {
    out.push({ type: "text", value: value.slice(lastIndex) });
  }
  return out;
}

function sliceNode(source: string, node: MdNode): string | null {
  const start = node.position?.start.offset;
  const end = node.position?.end.offset;
  if (typeof start !== "number" || typeof end !== "number") return null;
  return source.slice(start, end);
}

function walk(node: MdNode, onParent: (parent: MdNode) => void): void {
  if (!Array.isArray(node.children)) return;
  onParent(node);
  // Snapshot because onParent may splice the children array.
  for (const child of [...(node.children ?? [])]) {
    if (child && typeof child === "object" && Array.isArray(child.children)) {
      walk(child, onParent);
    }
  }
}

/**
 * DOMPurify profile for the editor's seed HTML.
 *
 * The pipeline uses `allowDangerousHtml: true` (remark/rehype-raw is NOT in the
 * chain, so genuine HTML in the source survives as-is). The output is only ever
 * fed to Tiptap, whose ProseMirror schema already drops any tag/attr it has no
 * node/mark for — so the practical XSS sink is small. This pass is
 * defence-in-depth: it strips scriptable content before the string ever reaches
 * `setContent`, while explicitly preserving every shape the markdown round-trip
 * (`toMarkdown.ts`) relies on:
 *
 *   - `<a>` carrying `data-wikilink` / `data-slug` / `data-broken` / `class` /
 *     `href` (wikilink serialisation),
 *   - `<mark>` (highlight), `<u>` / `<sub>` / `<sup>` (script formatting),
 *   - `<span style>` (inline colour),
 *   - tables, task-list `<input type=checkbox checked disabled>`,
 *   - `<pre><code class="language-X">` fenced blocks.
 *
 * `class` and `style` are in DOMPurify's default attribute allow-list and data
 * attributes are kept via `ALLOW_DATA_ATTR`; they are named in `ADD_ATTR` to
 * pin the contract so a future DOMPurify default change cannot silently drop
 * them. `FORBID_TAGS` drops the scriptable containers we never emit.
 */
const SEED_PURIFY_CONFIG: DOMPurifyConfig = {
  ALLOW_DATA_ATTR: true,
  ADD_ATTR: ["class", "style", "href", "type", "checked", "disabled"],
  FORBID_TAGS: [
    "script",
    "style",
    "iframe",
    "object",
    "embed",
    "form",
    "link",
    "meta",
    "base",
  ],
  FORBID_ATTR: ["srcdoc"],
};

/**
 * Strip scriptable content from the seed HTML when a DOM is available.
 *
 * DOMPurify needs a `window`; this module is browser-only in practice (Tiptap
 * is lazy-loaded), and the test/SSR environments that exercise the round-trip
 * provide happy-dom. When no DOM exists we return the HTML untouched — the
 * ProseMirror-schema boundary at `setContent` is the backstop, and the source
 * is never raw-rendered as HTML elsewhere (the live preview goes through
 * react-markdown without `rehype-raw`).
 */
function sanitizeSeedHtml(html: string): string {
  if (typeof window === "undefined") return html;
  return DOMPurify.sanitize(html, SEED_PURIFY_CONFIG);
}

/**
 * Convert wiki markdown into the HTML string Tiptap loads as its initial doc.
 *
 * @param markdown Source markdown (wikilinks, GFM, citations, fenced blocks).
 * @param resolver Optional `(slug) => exists`. When provided, wikilinks whose
 *   slug does not resolve are tagged `wk-broken`.
 */
export function wikiMarkdownToHtml(
  markdown: string,
  resolver?: (slug: string) => boolean,
): string {
  // `literalFootnotesPlugin` slices the original source by node offset, so it
  // must read the exact string handed to `remarkParse`. Build the processor
  // per call rather than freezing a shared one: the source closure changes
  // every call and the resolver is caller-specific.
  const processor = unified()
    .use(remarkParse)
    .use(remarkGfm)
    .use(literalFootnotesPlugin(markdown))
    .use(wikilinkPlugin(resolver))
    .use(remarkRehype, { allowDangerousHtml: true })
    .use(rehypeStringify, { allowDangerousHtml: true });

  const file = processor.processSync(markdown);
  return sanitizeSeedHtml(String(file));
}
