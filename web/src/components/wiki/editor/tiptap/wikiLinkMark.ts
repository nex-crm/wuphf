/**
 * Tiptap mark for WUPHF wikilinks.
 *
 * Renders `[[slug]]` / `[[slug|Display]]` as an anchor that matches the live
 * preview's wikilink shape (`src/lib/wikilink.ts` -> `wikiLinkRemarkPlugin`):
 *
 *   <a data-wikilink="true" data-slug="<slug>" href="#/wiki/<slug>"
 *      class="wk-wikilink">Display</a>
 *
 * A broken target (resolver returns false) adds the `wk-broken` class so the
 * editor renders it identically to the preview pane.
 *
 * Slug semantics follow `parseWikiLinkInner`: the mark stores the *slug*
 * (article path) separately from the *display* text it wraps. A bare
 * `[[slug]]` link wraps `slug` as its text; a piped `[[slug|Display]]` wraps
 * `Display`. The input rule reuses `parseWikiLinkInner` so a malformed inner
 * (path traversal, absolute path, control bytes) never becomes a mark — it is
 * left as literal text, exactly as the preview would treat it.
 *
 * The slug grammar and the anchor attributes are WUPHF's own, distinct from
 * any upstream `#page:` / `data-page-name` convention.
 */

import { InputRule, Mark, mergeAttributes } from "@tiptap/core";

import { parseWikiLinkInner } from "../../../../lib/wikilink";

/** Resolver decides whether a slug points at an existing article. */
export type WikiLinkResolver = (slug: string) => boolean;

export interface WikiLinkOptions {
  /**
   * Returns true when the target slug exists. Defaults to treating every
   * slug as resolvable (no broken styling) so the mark works standalone.
   */
  resolver: WikiLinkResolver;
}

export interface WikiLinkAttributes {
  /** Canonical wiki path used by `[[…]]` resolution. */
  slug: string | null;
}

/**
 * Build the anchor attributes shared by `renderHTML` and the input rule's
 * mark creation. Keeping this in one place guarantees the editor anchor and
 * the preview anchor stay byte-for-byte aligned.
 */
function anchorAttributes(
  slug: string,
  resolver: WikiLinkResolver,
): Record<string, string> {
  const broken = !resolver(slug);
  return {
    "data-wikilink": "true",
    "data-slug": slug,
    href: `#/wiki/${encodeURI(slug)}`,
    class: broken ? "wk-wikilink wk-broken" : "wk-wikilink",
  };
}

export const WikiLink = Mark.create<WikiLinkOptions>({
  name: "wikiLink",
  priority: 1000,
  keepOnSplit: false,
  inclusive: false,
  excludes: "_",

  addOptions() {
    return {
      resolver: () => true,
    };
  },

  addAttributes() {
    return {
      slug: {
        default: null,
        parseHTML: (element) => element.getAttribute("data-slug"),
        // The slug is the only persisted attribute; the rendered href/class
        // are derived from it in renderHTML so they cannot drift.
        renderHTML: () => ({}),
      },
    };
  },

  parseHTML() {
    return [
      {
        tag: 'a[data-wikilink="true"]',
      },
    ];
  },

  renderHTML({ mark }) {
    // The slug is the mark's only persisted attribute. It must be read from
    // `mark.attrs` (not `HTMLAttributes`) because the attribute's own
    // `renderHTML: () => ({})` deliberately keeps `data-slug` out of the
    // merged HTMLAttributes — the anchor's data-slug/href/class are all
    // derived from this single value so they cannot drift.
    const rawSlug = mark.attrs.slug;
    const slug = typeof rawSlug === "string" ? rawSlug : "";
    const parsed = parseWikiLinkInner(slug);
    // A mark that somehow carries an invalid slug still renders as an anchor
    // so the user can fix it, but it always resolves "broken".
    const safeSlug = parsed ? parsed.slug : slug;
    return [
      "a",
      mergeAttributes(anchorAttributes(safeSlug, this.options.resolver)),
      0,
    ];
  },

  addInputRules() {
    return [
      new InputRule({
        // Match a completed `[[…]]` token ending at the caret. The inner is
        // validated by parseWikiLinkInner before any mark is created.
        find: /\[\[([^[\]\n]+)\]\]$/,
        handler: ({ state, range, match }) => {
          const parsed = parseWikiLinkInner(match[1]);
          if (!parsed) return; // leave malformed input as literal text
          const { slug, display } = parsed;
          const mark = state.schema.marks.wikiLink.create({ slug });
          state.tr.replaceWith(
            range.from,
            range.to,
            state.schema.text(display, [mark]),
          );
        },
      }),
    ];
  },
});
