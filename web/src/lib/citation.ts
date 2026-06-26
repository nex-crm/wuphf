/**
 * Citation-marker parser + remark plugin for compiled wiki articles.
 *
 * The deterministic compile engine ends every cited claim with a marker of
 * the form `^[source-id]` (see internal/team/wiki_compile_page.go), where the
 * source-id encodes its kind as the prefix before the first "-"
 * (e.g. `^[task-wup-12]`, `^[chat-general-2026-06-25]`).
 *
 * This module turns those inline text tokens into link AST nodes carrying a
 * `data-citation` marker + the source id, which the read view intercepts and
 * mounts as a {@link CitationBadge}. The shape mirrors `lib/wikilink.ts`.
 *
 * NOTE: this is distinct from GFM footnotes (`[^n]`, caret INSIDE the
 * brackets). Citation markers put the caret BEFORE the brackets (`^[id]`), so
 * the two never collide.
 */

// A source id has no whitespace and no closing bracket. We deliberately reject
// markers with spaces so an accidental `^[ not an id ]` in prose is left alone.
const CITATION_RE = /\^\[([^\]\s]+)\]/g;

/**
 * Extract the ordered, de-duplicated list of source ids cited in a markdown
 * body — the order of first appearance. Drives Wikipedia-style `[n]` numbering
 * (a repeated id keeps its first number).
 */
export function extractCitationIds(markdown: string): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const match of markdown.matchAll(CITATION_RE)) {
    const [, id] = match;
    if (seen.has(id)) continue;
    seen.add(id);
    out.push(id);
  }
  return out;
}

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
 * Build a remark plugin that rewrites `^[source-id]` tokens inside text nodes
 * into link AST nodes tagged with `data-citation="true"`. The read view's
 * `a` component override intercepts these and mounts a citation badge.
 */
export function citationRemarkPlugin() {
  return function plugin() {
    return function transformer(tree: unknown) {
      walk(tree as MdAnyNode, (parent) =>
        replaceCitationsInParent(parent.children),
      );
    };
  };
}

/** Rewrite `^[id]` markers inside one parent's direct text children in place. */
function replaceCitationsInParent(children: MdAnyNode[]): void {
  for (let i = 0; i < children.length; i++) {
    const child = children[i];
    if (child.type !== "text") continue;
    const { value } = child as MdTextNode;
    if (typeof value !== "string" || !value.includes("^[")) continue;

    const replacements = buildReplacements(value);
    if (replacements.length === 0) continue;
    children.splice(i, 1, ...replacements);
    i += replacements.length - 1;
  }
}

function buildReplacements(value: string): MdAnyNode[] {
  const re = new RegExp(CITATION_RE.source, "g");
  const out: MdAnyNode[] = [];
  let lastIndex = 0;
  let changed = false;
  let match = re.exec(value);
  while (match !== null) {
    const [, id] = match;
    const { lastIndex: nextLastIndex } = re;
    if (match.index > lastIndex) {
      out.push({ type: "text", value: value.slice(lastIndex, match.index) });
    }
    out.push({
      type: "link",
      url: `#cite-${encodeURIComponent(id)}`,
      children: [{ type: "text", value: id }],
      data: {
        hProperties: {
          "data-citation": "true",
          "data-source-id": id,
          className: "wk-citation",
        },
      },
    });
    changed = true;
    lastIndex = nextLastIndex;
    match = re.exec(value);
  }
  if (!changed) return [];
  if (lastIndex < value.length) {
    out.push({ type: "text", value: value.slice(lastIndex) });
  }
  return out;
}

function walk(node: MdAnyNode, onParent: (parent: MdParent) => void) {
  const maybeParent = node as { children?: MdAnyNode[] };
  const { children } = maybeParent;
  if (!Array.isArray(children)) return;
  onParent(node as MdParent);
  // Walk a snapshot because onParent may have mutated children.
  const snapshot = [...children];
  for (const child of snapshot) {
    if (child && typeof child === "object" && "children" in child) {
      walk(child as MdAnyNode, onParent);
    }
  }
}
