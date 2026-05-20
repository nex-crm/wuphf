/**
 * Obsidian-flavored callout detection.
 *
 * A callout is a markdown blockquote whose first line starts with
 * `[!type]` optionally followed by `+` (start expanded) or `-` (start
 * collapsed), and optionally followed by a single-line title:
 *
 *   > [!note] Optional title
 *   > Body content
 *
 *   > [!warning]+
 *   > Expanded by default
 *
 *   > [!caution]-
 *   > Collapsed by default
 *
 * The renderer in `Callout.tsx` consumes the data attributes attached
 * by `calloutRemarkPlugin` below.
 *
 * Phase 4 of `docs/specs/WIKI-OBSIDIAN-COMPATIBILITY.md` §7.1.
 */

export type CalloutType =
  | "note"
  | "info"
  | "tip"
  | "important"
  | "warning"
  | "caution";

const KNOWN_TYPES: ReadonlySet<string> = new Set<CalloutType>([
  "note",
  "info",
  "tip",
  "important",
  "warning",
  "caution",
]);

export interface CalloutMarker {
  type: CalloutType;
  title: string;
  /** Markdown source of the body, with the marker line stripped. May be empty. */
  body: string;
  /**
   * `undefined` for the bare `[!type]` form (renders as a non-folding block),
   * `true` for `[!type]+` (folded, start expanded),
   * `false` for `[!type]-` (folded, start collapsed).
   */
  defaultOpen: boolean | undefined;
}

const MARKER_RE = /^\[!([a-zA-Z]+)\]([+-]?)[ \t]*(.*?)\s*$/;

/**
 * Parse the leading marker line of a callout from a string containing
 * the blockquote's contents (i.e. with the `> ` prefixes already removed).
 *
 * Returns null if the input does not start with a recognized callout marker.
 * Unknown types fall back to `note`.
 */
export function parseCalloutMarker(input: string): CalloutMarker | null {
  if (typeof input !== "string" || input.length === 0) return null;
  const newlineIdx = input.indexOf("\n");
  const firstLine = newlineIdx === -1 ? input : input.slice(0, newlineIdx);
  const rest = newlineIdx === -1 ? "" : input.slice(newlineIdx + 1);
  const match = firstLine.match(MARKER_RE);
  if (!match) return null;
  const [, rawTypeRaw, foldMark, title] = match;
  const rawType = rawTypeRaw.toLowerCase();
  const type: CalloutType = (
    KNOWN_TYPES.has(rawType) ? rawType : "note"
  ) as CalloutType;
  let defaultOpen: boolean | undefined;
  if (foldMark === "+") defaultOpen = true;
  else if (foldMark === "-") defaultOpen = false;
  else defaultOpen = undefined;
  return { type, title, body: rest, defaultOpen };
}

// ── Remark plugin: detect callout-shaped blockquotes and tag them ──

interface MdTextNode {
  type: "text";
  value: string;
}

interface MdParagraph {
  type: "paragraph";
  children: MdInlineNode[];
}

type MdInlineNode = MdTextNode | { type: string; children?: MdInlineNode[] };

interface MdBlockquote {
  type: "blockquote";
  children: MdBlockNode[];
  data?: { hProperties?: Record<string, string> };
}

type MdBlockNode =
  | MdParagraph
  | MdBlockquote
  | { type: string; children?: unknown };

interface MdRoot {
  type: string;
  children?: MdBlockNode[];
}

function isTextNode(n: MdInlineNode): n is MdTextNode {
  return n.type === "text" && typeof (n as MdTextNode).value === "string";
}

function isParagraph(n: MdBlockNode): n is MdParagraph {
  return n.type === "paragraph" && Array.isArray((n as MdParagraph).children);
}

function isBlockquote(n: MdBlockNode): n is MdBlockquote {
  return n.type === "blockquote" && Array.isArray((n as MdBlockquote).children);
}

/**
 * Walk the mdast tree and rewrite callout-shaped blockquotes so the
 * react-markdown blockquote override (`Callout.tsx`) can render them.
 *
 * Detection: the blockquote's first child is a paragraph whose first
 * inline child is a text node beginning with `[!type]...`.
 *
 * Rewrite: strip the marker (and any title text on the same line) from
 * the first text node, and attach `data-callout-*` hProperties to the
 * blockquote so the React layer can pick them up. Non-callout
 * blockquotes are left untouched.
 */
export function calloutRemarkPlugin() {
  return function plugin() {
    return function transformer(tree: unknown) {
      visitBlockquotes(tree as MdRoot, transformBlockquote);
    };
  };
}

function visitBlockquotes(
  node: MdRoot | MdBlockNode,
  visit: (bq: MdBlockquote) => void,
) {
  const { children } = node as { children?: MdBlockNode[] };
  if (!Array.isArray(children)) return;
  for (const child of children) {
    if (isBlockquote(child)) {
      visit(child);
    }
    // Recurse so nested callouts (callouts inside list items, etc.) still
    // get detected. Cheap because the wiki uses short articles.
    visitBlockquotes(child as MdBlockNode, visit);
  }
}

interface DetectedMarker {
  type: CalloutType;
  title: string;
  foldMark: string;
}

function detectMarker(text: string): {
  marker: DetectedMarker;
  remainder: string;
} | null {
  if (!text.startsWith("[!")) return null;
  const newlineIdx = text.indexOf("\n");
  const firstLine = newlineIdx === -1 ? text : text.slice(0, newlineIdx);
  const remainder = newlineIdx === -1 ? "" : text.slice(newlineIdx + 1);
  const match = firstLine.match(MARKER_RE);
  if (!match) return null;
  const [, rawTypeRaw, foldMark, title] = match;
  const rawType = rawTypeRaw.toLowerCase();
  const type: CalloutType = (
    KNOWN_TYPES.has(rawType) ? rawType : "note"
  ) as CalloutType;
  return { marker: { type, title, foldMark }, remainder };
}

function stripMarkerLine(
  bq: MdBlockquote,
  firstBlock: MdParagraph,
  firstInline: MdTextNode,
  remainder: string,
): void {
  if (remainder.length === 0 && firstBlock.children.length === 1) {
    bq.children.shift();
    return;
  }
  firstInline.value = remainder;
  if (remainder.length === 0) {
    // Drop the now-empty leading text node so the paragraph can start
    // with an inline element (e.g. a wikilink) cleanly.
    firstBlock.children.shift();
  }
  if (firstBlock.children.length === 0) {
    bq.children.shift();
  }
}

function attachCalloutData(bq: MdBlockquote, marker: DetectedMarker): void {
  const data = bq.data ?? {};
  const hProperties = data.hProperties ?? {};
  hProperties["data-callout"] = "true";
  hProperties["data-callout-type"] = marker.type;
  if (marker.title) hProperties["data-callout-title"] = marker.title;
  if (marker.foldMark === "+") hProperties["data-callout-fold"] = "open";
  else if (marker.foldMark === "-") hProperties["data-callout-fold"] = "closed";
  data.hProperties = hProperties;
  bq.data = data;
}

function transformBlockquote(bq: MdBlockquote) {
  const [firstBlock] = bq.children;
  if (!(firstBlock && isParagraph(firstBlock))) return;
  const [firstInline] = firstBlock.children;
  if (!(firstInline && isTextNode(firstInline))) return;
  const detected = detectMarker(firstInline.value);
  if (!detected) return;
  stripMarkerLine(bq, firstBlock, firstInline, detected.remainder);
  attachCalloutData(bq, detected.marker);
}
