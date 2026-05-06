/**
 * Scans markdown for `[[slug]]` wikilinks and reports any that fail the
 * catalog resolver. Used by the rich editor's broken-link banner so an
 * unresolved insert is visible before the user saves.
 *
 * Skips fenced code blocks and inline code spans so an article that
 * documents wikilink syntax verbatim does not light up the warning.
 */

import { parseWikiLinkInner } from "../../../../lib/wikilink";

// Matches fenced blocks (``` or ~~~ of length 3+) and inline code spans
// (any run of one or more backticks, balanced on both sides). Without the
// tilde-fence and multi-backtick branches, valid Markdown code that wraps
// `[[wikilink]]` examples would still be scanned and produce false
// unresolved-link warnings.
const CODE_SEGMENT_RE = /(`{3,}[\s\S]*?`{3,}|~{3,}[\s\S]*?~{3,}|`+[^`\n]*`+)/g;
const WIKILINK_RE = /\[\[([^\]\n]+)\]\]/g;

export interface BrokenLink {
  slug: string;
  display: string;
}

export function findBrokenWikilinks(
  markdown: string,
  resolver: (slug: string) => boolean,
): BrokenLink[] {
  const broken: BrokenLink[] = [];
  const seen = new Set<string>();
  const parts = markdown.split(CODE_SEGMENT_RE);
  for (let i = 0; i < parts.length; i += 2) {
    const segment = parts[i];
    let match = WIKILINK_RE.exec(segment);
    while (match !== null) {
      const parsed = parseWikiLinkInner(match[1]);
      if (parsed && !resolver(parsed.slug) && !seen.has(parsed.slug)) {
        seen.add(parsed.slug);
        broken.push(parsed);
      }
      match = WIKILINK_RE.exec(segment);
    }
    WIKILINK_RE.lastIndex = 0;
  }
  return broken;
}
