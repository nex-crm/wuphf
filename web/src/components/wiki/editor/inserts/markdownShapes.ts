/**
 * Canonical markdown emitters for the WUPHF-specific editor inserts.
 *
 * Each insert action produces a deterministic markdown fragment that survives
 * the rich editor's parse then serialize then `postProcessWikilinks` round
 * trip without mutation. The functions here are pure so we can unit-test the
 * shapes against the headless Milkdown pipeline used by the existing
 * `RichWikiEditor.roundtrip.test.tsx`.
 *
 * Shape choices:
 *   - Wikilinks (`buildWikilink`): mirror the textarea's `[[slug]]` /
 *     `[[slug|Display]]` syntax so any insert that resolves to a wiki page
 *     reuses the same parser/resolver chain. Validated through
 *     `parseWikiLinkInner` so a malformed slug never reaches the editor.
 *   - Citations (`buildCitation`): GFM footnotes. The reference is
 *     inserted at the caret; the matching definition lives in a
 *     dedicated block we append to the document end. Footnote IDs are
 *     allocated to avoid collisions with existing markers.
 *   - Fact / decision blocks (`buildFactBlock`, `buildDecisionBlock`):
 *     fenced code blocks with the language tag `fact` or `decision`. Both
 *     serialize losslessly through `commonmark` as the existing round-trip
 *     test for fenced code already proves. The body is YAML-ish key/value
 *     so a future broker-side parser can lift the structure without a new
 *     mdast extension.
 *   - Related pages (`buildRelatedBlock`): a `## Related` heading
 *     followed by a bullet list of wikilinks. Plain markdown, no special
 *     parser required.
 *
 * All emitters always finish with a trailing newline so consecutive inserts
 * compose cleanly when concatenated.
 */

import { parseWikiLinkInner } from "../../../../lib/wikilink";

/**
 * Build a `[[slug]]` or `[[slug|Display]]` wikilink. Returns null when the
 * slug fails wikilink-grammar validation (path traversal, empty string,
 * control bytes, etc.) so callers can surface a warning instead of writing
 * a fragment the next parse would discard as literal text.
 */
export function buildWikilink(slug: string, display?: string): string | null {
  const trimmedSlug = slug.trim();
  const trimmedDisplay = display?.trim() ?? "";
  const inner =
    trimmedDisplay && trimmedDisplay !== trimmedSlug
      ? `${trimmedSlug}|${trimmedDisplay}`
      : trimmedSlug;
  if (!parseWikiLinkInner(inner)) return null;
  return `[[${inner}]]`;
}

export interface CitationDraft {
  /** Footnote identifier — must be unique within the document. Lowercased,
   *  ASCII-only, no whitespace. Caller is expected to allocate via
   *  `nextFootnoteId`. */
  id: string;
  /** Human-readable title. Falls back to the URL when empty. */
  title: string;
  /** Source URL. Required. */
  url: string;
}

export interface BuiltCitation {
  /** What goes at the caret — the `[^id]` reference. */
  reference: string;
  /** What gets appended to the doc — the `[^id]: Title - URL` definition.
   *  Always ends with a newline so the appended block stays one footnote
   *  per line. */
  definition: string;
}

/**
 * GFM footnote pair. The reference is inline; the definition belongs at the
 * end of the article. `appendCitationDefinition` below stitches the
 * definition into existing markdown without duplicating an existing entry.
 */
export function buildCitation(draft: CitationDraft): BuiltCitation {
  const id = sanitizeFootnoteId(draft.id);
  const title = draft.title.trim() || draft.url.trim();
  const url = draft.url.trim();
  return {
    reference: `[^${id}]`,
    definition: `[^${id}]: ${title} - ${url}\n`,
  };
}

/**
 * Allocate the next available footnote id given the markdown that already
 * exists in the document. Returns the `id` portion (without the `[^...]`
 * brackets) so callers can format references consistently.
 */
export function nextFootnoteId(existingMarkdown: string): string {
  const used = new Set<string>();
  const re = /\[\^([A-Za-z0-9_-]+)\]/g;
  let match = re.exec(existingMarkdown);
  while (match !== null) {
    used.add(match[1]);
    match = re.exec(existingMarkdown);
  }
  let n = 1;
  while (used.has(String(n))) n += 1;
  return String(n);
}

/**
 * Footnote ids are restricted to `[A-Za-z0-9_-]` because GFM does not
 * tolerate spaces or other punctuation in the bracket label.
 */
function sanitizeFootnoteId(raw: string): string {
  const cleaned = raw.replace(/[^A-Za-z0-9_-]/g, "").slice(0, 64);
  return cleaned.length > 0 ? cleaned : "1";
}

/**
 * Append a citation definition to the existing markdown. If a definition
 * with the same id already exists, returns the markdown unchanged so we do
 * not stack duplicates each time the user re-inserts the same source.
 */
export function appendCitationDefinition(
  markdown: string,
  definition: string,
): string {
  const idMatch = definition.match(/^\[\^([A-Za-z0-9_-]+)\]:/);
  if (!idMatch) return markdown;
  const idRe = new RegExp(`(^|\\n)\\[\\^${escapeRegExp(idMatch[1])}\\]:`);
  if (idRe.test(markdown)) return markdown;
  const trimmed = markdown.replace(/\s+$/, "");
  return `${trimmed}\n\n${definition.replace(/\s+$/, "")}\n`;
}

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

export interface FactDraft {
  subject: string;
  predicate: string;
  object: string;
  /** Optional confidence score (0..1). Omitted from the block when undefined. */
  confidence?: number;
  /** Optional source citation reference (e.g. `[^1]`). */
  source?: string;
}

/**
 * Fenced ```fact block. Empty fields (other than confidence) are kept
 * as blank values so the structure stays parseable even when partially
 * filled out - the preview dialog enforces the required fields before it
 * lets the user confirm.
 */
export function buildFactBlock(draft: FactDraft): string {
  const lines = [
    "```fact",
    `subject: ${escapeBlockValue(draft.subject)}`,
    `predicate: ${escapeBlockValue(draft.predicate)}`,
    `object: ${escapeBlockValue(draft.object)}`,
  ];
  if (
    typeof draft.confidence === "number" &&
    Number.isFinite(draft.confidence)
  ) {
    const clamped = Math.max(0, Math.min(1, draft.confidence));
    lines.push(`confidence: ${clamped}`);
  }
  if (draft.source?.trim()) {
    lines.push(`source: ${escapeBlockValue(draft.source)}`);
  }
  lines.push("```");
  return `${lines.join("\n")}\n`;
}

export interface DecisionDraft {
  title: string;
  rationale: string;
  /** ISO date - `YYYY-MM-DD`. */
  date: string;
  /** Optional list of alternatives that were considered and not chosen. */
  alternatives?: string[];
}

/**
 * Fenced ```decision block. Alternatives render as a single comma-
 * separated value to keep the body line-oriented and avoid a YAML parser
 * dependency. Future maintenance can lift this to proper YAML once a
 * broker-side reader exists.
 */
export function buildDecisionBlock(draft: DecisionDraft): string {
  const lines = [
    "```decision",
    `title: ${escapeBlockValue(draft.title)}`,
    `date: ${escapeBlockValue(draft.date)}`,
    `rationale: ${escapeBlockValue(draft.rationale)}`,
  ];
  if (draft.alternatives && draft.alternatives.length > 0) {
    const filtered = draft.alternatives
      .map((a) => a.trim())
      .filter((a) => a.length > 0);
    if (filtered.length > 0) {
      lines.push(`alternatives: ${filtered.join(", ")}`);
    }
  }
  lines.push("```");
  return `${lines.join("\n")}\n`;
}

/**
 * Block values must not contain newlines or backtick fences (which would
 * break the enclosing code fence). The replacement keeps the value
 * round-trippable through commonmark while preventing accidental fence
 * termination.
 */
function escapeBlockValue(raw: string): string {
  return raw.replace(/```/g, "'''").replace(/\r?\n/g, " ").trim();
}

export interface RelatedBlockEntry {
  slug: string;
  display?: string;
}

/**
 * `## Related` heading + bullet list. Each entry is filtered through
 * `buildWikilink` so a malformed slug is silently dropped from the block
 * rather than corrupting the whole insert.
 */
export function buildRelatedBlock(entries: RelatedBlockEntry[]): string {
  const links = entries
    .map((e) => buildWikilink(e.slug, e.display))
    .filter((s): s is string => s !== null);
  if (links.length === 0) return "";
  const body = links.map((l) => `- ${l}`).join("\n");
  return `## Related\n\n${body}\n`;
}
