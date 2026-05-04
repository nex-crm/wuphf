/**
 * Wiki-link aware markdown serialization.
 *
 * Our `wikilinkRemarkPlugin` (in `wikilink.ts`) parses `[[slug]]` and
 * `[[slug|Display]]` into standard mdast `link` nodes with
 * `data-wikilink="true"` on `hProperties`. That is correct for rendering
 * but lossy for serialization: plain `remark-stringify` writes those nodes
 * back as `[Display](#/wiki/slug)`, silently corrupting wiki-link syntax
 * on every save.
 *
 * The handler below detects wiki-link nodes and emits `[[slug]]` (or
 * `[[slug|Display]]` when display text differs) instead. The
 * `remarkWikilinkStringify` plugin attaches the handler so any rich editor
 * built on `unified` + `mdast-util-to-markdown` (Milkdown, custom
 * pipelines, etc.) round-trips wiki-links cleanly.
 */
import type { Link } from "mdast";
import type { Options as StringifyOptions } from "mdast-util-to-markdown";
import type { Plugin } from "unified";

/**
 * Opinionated defaults for `remark-stringify` so wiki article output stays
 * consistent regardless of which editor produced it.
 */
export const STRINGIFY_DEFAULTS: StringifyOptions = {
  bullet: "-",
  emphasis: "_",
  strong: "*",
  fence: "`",
  fences: true,
  listItemIndent: "one",
};

/**
 * `mdast-util-to-markdown` handler that emits `[[slug]]` for link nodes
 * tagged with `data-wikilink="true"`. Falls through to the default link
 * handler for ordinary links.
 */
export const wikilinkStringifyHandler = {
  link(
    node: Link,
    _: unknown,
    state: {
      containerPhrasing: (
        node: Link,
        options: { before: string; after: string },
      ) => string;
    },
  ): string {
    const isWikilink =
      node.data?.hProperties &&
      (node.data.hProperties as Record<string, unknown>)["data-wikilink"] ===
        "true";

    if (!isWikilink) {
      return state.containerPhrasing(node, { before: "[", after: "]" });
    }

    const slug = (node.data?.hProperties as Record<string, unknown>)[
      "data-slug"
    ] as string | undefined;

    if (!slug) {
      // Degenerate case — should not happen with valid wikilinks emitted
      // by `wikilinkRemarkPlugin`. Fall through rather than silently drop.
      return state.containerPhrasing(node, { before: "[", after: "]" });
    }

    const childText = node.children
      .map((c) => ("value" in c ? (c as { value: string }).value : ""))
      .join("");

    return slug === childText ? `[[${slug}]]` : `[[${slug}|${childText}]]`;
  },
};

/**
 * Unified plugin that registers `wikilinkStringifyHandler` so any
 * `remark-stringify` pipeline downstream emits `[[slug]]` syntax instead
 * of standard markdown links for wiki-link nodes.
 */
export const remarkWikilinkStringify: Plugin<[]> = function () {
  const data = this.data() as {
    toMarkdownExtensions?: Array<{ handlers?: Record<string, unknown> }>;
  };
  if (!data.toMarkdownExtensions) data.toMarkdownExtensions = [];
  data.toMarkdownExtensions.push({
    handlers: { link: wikilinkStringifyHandler.link },
  });
};
