/**
 * Wiki-link post-processing for the rich editor.
 *
 * Milkdown's commonmark serializer emits wikilinks as standard markdown
 * links — `[Display](#/wiki/slug)` — because that's the URL `wikiLinkRemarkPlugin`
 * gave the parser when it ingested `[[slug]]`. Without a fix-up step every
 * save through the rich editor would silently corrupt wikilink syntax.
 *
 * This module rewrites those standard links back to `[[slug]]` /
 * `[[slug|Display]]` form. It works on the markdown string after Milkdown
 * serialization, so it cannot be confused by surrounding whitespace, lists,
 * or table cells — the regex matches the exact `[text](#/wiki/...)` shape
 * in any context.
 *
 * Detection is by URL prefix `#/wiki/`, which `wikiLinkRemarkPlugin`
 * deterministically assigns. Standard external links survive untouched.
 */

const WIKI_URL_RE = /\[([^\]\n]+)\]\(#\/wiki\/([^\s)]+?)(?:\s+"[^"]*")?\)/g;

export function postProcessWikilinks(markdown: string): string {
  return markdown.replace(
    WIKI_URL_RE,
    (_match, display: string, encoded: string) => {
      let slug: string;
      try {
        slug = decodeURI(encoded);
      } catch {
        // Malformed escape — leave the standard link in place rather than
        // emit corrupt wikilink syntax.
        return _match;
      }
      return slug === display ? `[[${slug}]]` : `[[${slug}|${display}]]`;
    },
  );
}
