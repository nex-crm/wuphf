/**
 * Pure markdown transforms that turn a raw wiki article body into the
 * Wikipedia-parity read view's inputs:
 *
 * - detect B2's `## Summary` definition list and lift it out as infobox data
 * - drop the literal `## References` heading when GFM footnote definitions
 *   are present (the rendered footnote section is relabeled "References")
 * - strip the lead H1 (the article chrome already renders the title)
 * - detect the generated-article marker comments (hatnote trigger)
 *
 * Everything here is deterministic string work — no React, no fetch — so the
 * read view stays testable against B2's golden article fixture.
 */

export interface InfoboxRow {
  /** Definition-list term, e.g. "Kind". */
  term: string;
  /** Raw markdown value, e.g. "[[people/eng]]" — rendered inline. */
  value: string;
}

export interface PreparedArticle {
  /** Markdown to render after the lifted sections are removed. */
  markdown: string;
  /** Infobox rows extracted from `## Summary`, or null when absent. */
  infobox: InfoboxRow[] | null;
  /** True when the body carries a generated-article marker comment. */
  generated: boolean;
  /** True when the body defines GFM footnotes (`[^n]: …`). */
  hasFootnotes: boolean;
}

/** Marker comments stamped by the deterministic article generators (B2/B3). */
const GENERATED_MARKERS = [
  "<!-- wuphf:entity-article",
  "<!-- wuphf:playbook-draft",
];

const SUMMARY_HEADING_RE = /^##\s+Summary\s*$/;
const HEADING_2_RE = /^##\s+/;
const FOOTNOTE_DEF_RE = /^\[\^[^\]]+\]:/;
const REFERENCES_HEADING_RE = /^##\s+References\s*$/;

export function prepareArticleMarkdown(content: string): PreparedArticle {
  const generated = GENERATED_MARKERS.some((m) => content.includes(m));
  const hasFootnotes = /^\[\^[^\]]+\]:/m.test(content);

  let lines = content.split("\n");
  lines = stripLeadH1(lines);

  const summary = extractSummarySection(lines);
  if (summary) {
    lines = [...lines.slice(0, summary.start), ...lines.slice(summary.end)];
  }

  if (hasFootnotes) {
    lines = stripReferencesHeading(lines);
  }

  return {
    markdown: lines.join("\n"),
    infobox: summary ? summary.rows : null,
    generated,
    hasFootnotes,
  };
}

/**
 * Remove the first `# Title` line when it appears before any other heading.
 * The article chrome renders the title as the page heading, so keeping the
 * body H1 would print the title twice — Wikipedia shows it once.
 */
function stripLeadH1(lines: string[]): string[] {
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const trimmed = line.trim();
    // Skip blanks, comment lines, and frontmatter-ish noise before the H1.
    if (trimmed === "" || trimmed.startsWith("<!--")) continue;
    if (/^#\s+/.test(trimmed)) {
      return [...lines.slice(0, i), ...lines.slice(i + 1)];
    }
    // First substantive line is not an H1 — leave the body alone.
    return lines;
  }
  return lines;
}

interface SummarySection {
  start: number;
  end: number;
  rows: InfoboxRow[];
}

/**
 * Find a `## Summary` section whose body is purely a markdown definition
 * list (`Term` line followed by a `: value` line). Returns the line range
 * to remove plus the parsed rows, or null when the section is absent or
 * carries non-definition prose (conservative: such articles keep their
 * Summary section in the body and get no infobox).
 */
function extractSummarySection(lines: string[]): SummarySection | null {
  const start = lines.findIndex((l) => SUMMARY_HEADING_RE.test(l.trim()));
  if (start < 0) return null;
  let end = lines.length;
  for (let i = start + 1; i < lines.length; i++) {
    if (HEADING_2_RE.test(lines[i])) {
      end = i;
      break;
    }
  }
  const rows: InfoboxRow[] = [];
  let pendingTerm: string | null = null;
  for (let i = start + 1; i < end; i++) {
    const line = lines[i].trim();
    if (line === "") continue;
    if (line.startsWith(":")) {
      if (pendingTerm === null) return null;
      rows.push({ term: pendingTerm, value: line.replace(/^:\s*/, "") });
      pendingTerm = null;
      continue;
    }
    if (pendingTerm !== null) {
      // Two consecutive non-definition lines — this is prose, not a
      // definition list. Leave the section in the body.
      return null;
    }
    pendingTerm = line;
  }
  if (pendingTerm !== null || rows.length === 0) return null;
  return { start, end, rows };
}

/**
 * Drop a `## References` heading whose section contains only footnote
 * definitions and blank lines. remark-gfm hoists `[^n]: …` definitions into
 * a generated end-of-document section which the read view labels
 * "References", so the literal heading would render as an empty duplicate.
 */
function stripReferencesHeading(lines: string[]): string[] {
  const idx = lines.findIndex((l) => REFERENCES_HEADING_RE.test(l.trim()));
  if (idx < 0) return lines;
  let end = lines.length;
  for (let i = idx + 1; i < lines.length; i++) {
    if (HEADING_2_RE.test(lines[i])) {
      end = i;
      break;
    }
  }
  for (let i = idx + 1; i < end; i++) {
    const line = lines[i].trim();
    if (line === "") continue;
    if (!FOOTNOTE_DEF_RE.test(line)) return lines;
  }
  return [...lines.slice(0, idx), ...lines.slice(idx + 1)];
}

/**
 * Build a wikilink existence resolver from the catalog's article paths.
 *
 * Catalog paths are canonical (`team/people/eng.md`) while wikilinks use
 * the short form B2 emits (`[[people/eng]]`) or a bare slug (`[[eng]]`).
 * A naive `Set.has` comparison marks every valid link as a redlink, so we
 * normalize both sides: strip the `team/` prefix and `.md` suffix, then
 * match bare slugs against any path's leaf segment (mirroring the
 * fetchArticle candidate-path fan-out).
 */
export function makeWikilinkResolver(
  paths: readonly string[],
): (slug: string) => boolean {
  const keys = new Set<string>();
  const leaves = new Set<string>();
  for (const path of paths) {
    const key = normalizeWikiKey(path);
    if (!key) continue;
    keys.add(key);
    const leaf = key.split("/").pop();
    if (leaf) leaves.add(leaf);
  }
  return (slug: string) => {
    const key = normalizeWikiKey(slug);
    if (!key) return false;
    if (keys.has(key)) return true;
    return !key.includes("/") && leaves.has(key);
  };
}

function normalizeWikiKey(pathOrSlug: string): string {
  let key = pathOrSlug.trim().replace(/^\/+/, "").replace(/\/+$/, "");
  if (key.startsWith("team/")) key = key.slice("team/".length);
  if (key.endsWith(".md")) key = key.slice(0, -".md".length);
  return key.toLowerCase();
}

/**
 * Plain-text excerpt of a markdown body — the first `maxWords` words of
 * prose with markdown syntax flattened. Drives the wikilink hover preview
 * card (Wikipedia's page-preview behavior: title + the first ~40 words).
 */
export function excerptFromMarkdown(content: string, maxWords = 40): string {
  const words: string[] = [];
  for (const rawLine of content.split("\n")) {
    const line = rawLine.trim();
    if (line === "") continue;
    if (line.startsWith("<!--") || line.endsWith("-->")) continue;
    if (/^#{1,6}\s/.test(line)) continue;
    if (line.startsWith(":")) continue;
    if (FOOTNOTE_DEF_RE.test(line)) continue;
    if (line === "---") continue;
    const flattened = flattenInlineMarkdown(line);
    for (const word of flattened.split(/\s+/)) {
      if (word === "") continue;
      words.push(word);
      if (words.length >= maxWords) {
        return `${words.join(" ")}…`;
      }
    }
  }
  return words.join(" ");
}

/** Flatten inline markdown: wikilinks, links, emphasis, footnote refs. */
function flattenInlineMarkdown(line: string): string {
  return line
    .replace(/\[\[([^\]|]+)\|([^\]]+)\]\]/g, "$2")
    .replace(/\[\[([^\]]+)\]\]/g, "$1")
    .replace(/\[\^[^\]]+\]/g, "")
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/^[-*+]\s+/, "")
    .replace(/[*_`]/g, "");
}
