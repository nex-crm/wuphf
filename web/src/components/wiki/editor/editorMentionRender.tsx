/**
 * React popup renderer for the `@`-mention suggestion in the Tiptap wiki
 * editor.
 *
 * `tiptap/mention.ts` is JSX-free and asks the editor component to inject the
 * `render` factory. This module supplies it: a `ReactRenderer`-backed popup
 * that reuses the existing `MentionMenu` chrome and the shared catalog search.
 *
 * Tiptap's suggestion `render` is imperative (onStart / onUpdate / onKeyDown /
 * onExit). We bridge it to React by mounting `MentionMenu` through Tiptap's
 * `ReactRenderer` and positioning it under the trigger via `clientRect`.
 */

import { ReactRenderer } from "@tiptap/react";
import type { SuggestionProps } from "@tiptap/suggestion";

import { MentionMenu } from "./inserts/MentionMenu";
import type { MentionItem } from "./inserts/mentionCatalog";
import type { WikiMentionProps } from "./tiptap/mention";

type Render = NonNullable<
  import("./tiptap/mention").WikiMentionSuggestion["render"]
>;

/** Reads the suggestion's anchor rect, falling back to the viewport origin. */
function rectToPosition(
  clientRect: (() => DOMRect | null) | null | undefined,
): { top: number; left: number } {
  const rect = clientRect?.();
  if (!rect) return { top: 0, left: 0 };
  return { top: rect.bottom + 4, left: rect.left };
}

interface MentionPopupProps {
  items: MentionItem[];
  query: string;
  position: { top: number; left: number };
  /** Editor contenteditable that keeps focus while the popup is open, so the
   *  active option id can be mirrored onto it for assistive tech. */
  editorDom: HTMLElement | null;
  onSelect: (item: MentionItem) => void;
  onClose: () => void;
}

function MentionPopup({
  items,
  query,
  position,
  editorDom,
  onSelect,
  onClose,
}: MentionPopupProps): React.ReactElement {
  return (
    <MentionMenu
      items={items}
      query={query}
      position={position}
      activeDescendantTarget={editorDom}
      onSelect={onSelect}
      onClose={onClose}
      heading="Mention a page"
    />
  );
}

/**
 * Build the imperative `render` for Tiptap's mention suggestion. `MentionMenu`
 * already owns its own keyboard navigation via `useMenuKeyNav`, so `onKeyDown`
 * here only intercepts Escape (to dismiss) and lets the menu's global listener
 * handle Arrow/Enter — returning `false` so Tiptap does not also consume them.
 */
export function buildMentionRender(): Render {
  return () => {
    // `MentionPopup` exposes no imperative handle, so the renderer's ref type
    // is the empty object rather than `unknown`.
    let renderer: ReactRenderer<
      Record<string, never>,
      MentionPopupProps
    > | null = null;
    let latest: SuggestionProps<MentionItem, WikiMentionProps> | null = null;

    const select = (item: MentionItem): void => {
      latest?.command({ id: item.slug, label: item.title });
    };

    const close = (): void => {
      latest?.command({ id: null, label: null });
    };

    const propsFor = (
      p: SuggestionProps<MentionItem, WikiMentionProps>,
    ): MentionPopupProps => ({
      items: p.items,
      query: p.query,
      position: rectToPosition(p.clientRect),
      editorDom: (p.editor.view.dom as HTMLElement) ?? null,
      onSelect: select,
      onClose: close,
    });

    return {
      onStart: (props) => {
        latest = props;
        renderer = new ReactRenderer(MentionPopup, {
          props: propsFor(props),
          editor: props.editor,
        });
      },
      onUpdate: (props) => {
        latest = props;
        renderer?.updateProps(propsFor(props));
      },
      onKeyDown: (props) => {
        if (props.event.key === "Escape") {
          close();
          return true;
        }
        // MentionMenu's own global keydown listener handles Arrow/Enter.
        return false;
      },
      onExit: () => {
        renderer?.destroy();
        renderer = null;
        latest = null;
      },
    };
  };
}
