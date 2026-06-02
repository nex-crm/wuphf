/**
 * `@`-mention suggestion for the wiki editor.
 *
 * Mentions resolve through the *same* `[[slug]]` wikilink path as everything
 * else in the wiki, so this extension does NOT introduce a separate `mention`
 * node (which would need its own markdown serializer). Instead, selecting a
 * mention inserts a `wikiLink`-marked text node — byte-identical to what the
 * `[[…]]` input rule produces — so the document serializes back to `[[slug]]`
 * markdown that the preview pane already understands.
 *
 * This module is JSX-free. The React popup that actually renders the
 * suggestion list is owned by the editor component (a later agent); it is
 * supplied here as the injectable `render` factory. `buildWikiMention` is a
 * pure config factory so it can be unit-tested without mounting React.
 *
 * Adapted from cabinet's `mention-extension.ts`; the trigger + suggestion
 * mechanics are cabinet's, the insert target (a WUPHF wikilink mark) is ours.
 */

import type { Node as TiptapNode } from "@tiptap/core";
import { Mention, type MentionNodeAttrs } from "@tiptap/extension-mention";
import { PluginKey } from "@tiptap/pm/state";
import type { SuggestionOptions } from "@tiptap/suggestion";

import { parseWikiLinkInner } from "../../../../lib/wikilink";
import type { MentionItem } from "../inserts/mentionCatalog";

/**
 * The attrs the suggestion's `command` receives for a chosen item.
 *
 * It is exactly Tiptap's `MentionNodeAttrs` so the value stays assignable to
 * the `Mention` extension's suggestion generic (which is contravariant in its
 * `command` props). The wiki slug is carried in the standard `id` field (its
 * canonical identifier) and the display text in `label`; the editor's picker
 * builds these from a `MentionItem` (`{ id: item.slug, label: item.title }`).
 * Both fields are nullable to match `MentionNodeAttrs`; `command` narrows them
 * at runtime.
 */
export type WikiMentionProps = MentionNodeAttrs;

/** Tiptap's suggestion shape specialised to this editor's item + props. */
export type WikiMentionSuggestion = Omit<
  SuggestionOptions<MentionItem, WikiMentionProps>,
  "editor"
>;

export interface WikiMentionConfig {
  /**
   * Returns the candidate items for the current query. Supplied by the editor
   * component, which holds the wiki catalog. Kept synchronous to match
   * Tiptap's suggestion contract; callers debounce/cache upstream.
   */
  getItems: (query: string) => MentionItem[];
  /**
   * The suggestion popup renderer. Injected by the editor component so this
   * module stays free of React/JSX. Maps directly onto Tiptap's
   * `SuggestionOptions["render"]`.
   */
  render: WikiMentionSuggestion["render"];
}

/** Stable plugin key so the suggestion state is addressable. */
export const wikiMentionPluginKey = new PluginKey("wiki-mention");

/**
 * Build the configured `@`-mention extension. The selected item is committed
 * as a `wikiLink` mark (not a mention node) so markdown export stays
 * `[[slug|Display]]`.
 */
export function buildWikiMention(config: WikiMentionConfig): TiptapNode {
  const suggestion: WikiMentionSuggestion = {
    pluginKey: wikiMentionPluginKey,
    char: "@",
    allowSpaces: false,
    items: ({ query }) => config.getItems(query),
    // Insert a wikiLink-marked text node, then a trailing space, replacing the
    // typed `@query` range. A defensive parse guards against a slug that fails
    // the grammar between selection and commit.
    command: ({ editor, range, props }) => {
      const parsed = props.id ? parseWikiLinkInner(props.id) : null;
      if (!parsed) {
        editor.chain().focus().deleteRange(range).run();
        return;
      }
      const { slug } = parsed;
      const display = (props.label ?? "").trim() || parsed.display;
      editor
        .chain()
        .focus()
        .deleteRange(range)
        .insertContent([
          {
            type: "text",
            text: display,
            marks: [{ type: "wikiLink", attrs: { slug } }],
          },
          { type: "text", text: " " },
        ])
        .run();
    },
    render: config.render,
  };

  return Mention.configure({ suggestion });
}
