/**
 * Wiki-link post-processing for the rich editor.
 *
 * Milkdown's commonmark serializer emits wikilinks as standard markdown
 * links — `[Display](#/wiki/slug)` — because that's the URL `wikiLinkRemarkPlugin`
 * gave the parser when it ingested `[[slug]]`. Without a fix-up step every
 * save through the rich editor would silently corrupt wikilink syntax.
 *
 * This module rewrites those standard links back to `[[slug]]` /
 * `[[slug|Display]]` form. It runs on the markdown string after Milkdown
 * serialization. Code spans and fenced code blocks are skipped so an
 * article that documents wikilink syntax verbatim (e.g. a how-to page
 * teaching `[foo](`#/wiki/bar`)`) does not get its examples silently
 * rewritten on every save.
 *
 * Detection is by URL prefix `#/wiki/`, which `wikiLinkRemarkPlugin`
 * deterministically assigns. Standard external links survive untouched.
 *
 * The constructed `[[slug|display]]` candidate is validated with
 * `parseWikiLinkInner` before emission so labels containing pipe chars,
 * `..`, leading `/`, or control bytes (which the wikilink grammar
 * rejects) fall back to the original markdown link instead of being
 * rewritten into syntax the next parse would discard as literal text.
 */

import { parseWikiLinkInner } from "../../../lib/wikilink";

const WIKI_URL_RE = /\[([^\]\n]+)\]\(#\/wiki\/([^\s)]+?)(?:\s+"[^"]*")?\)/g;

/**
 * Matches fenced code blocks (``` … ```) and inline code spans (` … `).
 * Used to split markdown so the wikilink rewrite skips verbatim regions.
 * Fenced blocks are matched first (longer alternative) to avoid an inline
 * span from greedily consuming a fenced opener.
 */
const CODE_SEGMENT_RE = /(`{3,}[\s\S]*?`{3,}|`[^`\n]+`)/g;

function rewriteWikilinks(segment: string): string {
  return segment.replace(
    WIKI_URL_RE,
    (match, display: string, encoded: string) => {
      let slug: string;
      try {
        slug = decodeURI(encoded);
      } catch {
        // Malformed escape — leave the standard link in place rather than
        // emit corrupt wikilink syntax.
        return match;
      }
      // Validate against the wikilink grammar before rewriting. A label
      // containing pipes (e.g. `[A | B](#/wiki/foo)`) or a slug with
      // path traversal would otherwise emit `[[foo|A | B]]`, which
      // `parseWikiLinkInner` rejects on the next read — so the link
      // would degrade into literal text after one save. Falling back to
      // the standard markdown link keeps the round-trip lossless.
      const inner = slug === display ? slug : `${slug}|${display}`;
      if (!parseWikiLinkInner(inner)) return match;
      return `[[${inner}]]`;
    },
  );
}

export function postProcessWikilinks(markdown: string): string {
  // String.split with a capturing group preserves the matched code regions
  // at odd indices. Even indices are non-code prose where rewrite is safe.
  const parts = markdown.split(CODE_SEGMENT_RE);
  for (let i = 0; i < parts.length; i += 2) {
    parts[i] = rewriteWikilinks(parts[i]);
  }
  return parts.join("");
}
