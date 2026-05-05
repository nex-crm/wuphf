/**
 * ProseMirror plugin that watches the editor for trigger characters
 * (`/` for slash menu, `@` for mention picker) and exposes the active
 * trigger state to the React layer.
 *
 * Why a custom plugin instead of `@milkdown/plugin-slash`'s SlashProvider?
 * The provider mounts its own floating element and computes positions via
 * `@floating-ui/dom`. We want React to own the menu DOM (so we can render
 * dialogs, manage keyboard nav, and lazy-load forms) — the provider's
 * lifecycle would fight that. This plugin only emits state; positioning
 * happens by reading `EditorView.coordsAtPos` synchronously when the
 * caller renders the menu.
 *
 * A single plugin instance handles both triggers because the state shapes
 * are identical (start position + query string + viewport rect). The
 * caller passes a list of trigger characters and gets the matched one
 * back so the React menu can branch on it.
 */

import { Plugin, PluginKey } from "@milkdown/prose/state";
import type { EditorView } from "@milkdown/prose/view";

export type TriggerChar = "/" | "@";

export interface TriggerState {
  trigger: TriggerChar;
  /** Document position immediately after the trigger character. */
  from: number;
  /** Document position at the caret (where the user is currently typing). */
  to: number;
  /** The substring between the trigger and the caret, used as the menu's
   *  filter query. */
  query: string;
  /** Viewport-relative bounding rect for the trigger character, used to
   *  position the React menu. */
  rect: { top: number; left: number; bottom: number; right: number };
}

export interface TriggerPluginOptions {
  triggers: readonly TriggerChar[];
  /** Called every time the trigger state changes. Pass `null` to clear. */
  onChange: (state: TriggerState | null) => void;
  /** Called once when the editor view mounts and again with `null` on
   *  destroy. Lets the React layer dispatch insert transactions without
   *  polling for the view. */
  onViewReady?: (view: EditorView | null) => void;
}

const triggerPluginKey = new PluginKey("wuphf-insert-trigger");

/**
 * Build the ProseMirror plugin. State lives on the React side via the
 * `onChange` callback — ProseMirror only needs to observe view updates
 * and dispatch the latest derived `TriggerState` to React.
 *
 * The plugin checks the text immediately preceding the caret on every
 * transaction. A trigger activates when:
 *   - the previous character is one of the trigger chars, AND
 *   - that trigger is at start-of-line OR follows whitespace
 *
 * Once active, it tracks the query string (everything typed after the
 * trigger up to the caret) and deactivates when the caret moves out of
 * the trigger's text run, the user types whitespace, or backspaces past
 * the trigger.
 */
export function buildTriggerPlugin(options: TriggerPluginOptions): Plugin {
  const triggerSet = new Set<string>(options.triggers);
  let lastEmitted: TriggerState | null = null;

  return new Plugin({
    key: triggerPluginKey,
    view: (view: EditorView) => {
      options.onViewReady?.(view);
      const update = (): void => {
        const next = computeTriggerState(view, triggerSet);
        if (!triggerStateEquals(lastEmitted, next)) {
          lastEmitted = next;
          options.onChange(next);
        }
      };
      update();
      return {
        update: () => {
          update();
        },
        destroy: () => {
          if (lastEmitted !== null) {
            lastEmitted = null;
            options.onChange(null);
          }
          options.onViewReady?.(null);
        },
      };
    },
  });
}

function triggerStateEquals(
  a: TriggerState | null,
  b: TriggerState | null,
): boolean {
  if (a === null && b === null) return true;
  if (a === null || b === null) return false;
  return (
    a.trigger === b.trigger &&
    a.from === b.from &&
    a.to === b.to &&
    a.query === b.query &&
    a.rect.top === b.rect.top &&
    a.rect.left === b.rect.left
  );
}

function computeTriggerState(
  view: EditorView,
  triggerSet: Set<string>,
): TriggerState | null {
  const { state } = view;
  const { selection } = state;
  if (!selection.empty) return null;
  const $pos = selection.$from;
  // Walk backwards from the caret looking for the most recent trigger
  // character. We bound the scan to 200 chars to keep the work O(1) on
  // large paragraphs.
  const parentOffset = $pos.parentOffset;
  if (parentOffset === 0) return null;
  const text = $pos.parent.textBetween(
    Math.max(0, parentOffset - 200),
    parentOffset,
    undefined,
    "￼",
  );
  let triggerIdx = -1;
  let trigger: TriggerChar | null = null;
  // Scan backwards for the trigger. Reject if any whitespace is between
  // the caret and the trigger — that means the user moved to a new word.
  for (let i = text.length - 1; i >= 0; i--) {
    const ch = text[i];
    if (ch === " " || ch === "\t" || ch === "\n") return null;
    if (triggerSet.has(ch)) {
      triggerIdx = i;
      trigger = ch as TriggerChar;
      break;
    }
  }
  if (triggerIdx === -1 || !trigger) return null;
  // The trigger must be at the start of the paragraph or preceded by
  // whitespace, so a `/` inside `http://` or an email's `@` does not
  // pop the menu. When `triggerIdx === 0`, the scan reached the start of
  // the 200-char window — that's "start of paragraph" since the window
  // is anchored at `parentOffset - text.length` and we never look past
  // a non-trigger non-whitespace character (the loop breaks first).
  if (triggerIdx > 0) {
    const charBeforeTrigger = text[triggerIdx - 1];
    if (charBeforeTrigger !== " " && charBeforeTrigger !== "\t") {
      return null;
    }
  }
  const query = text.slice(triggerIdx + 1);
  // Position state in document coordinates. `from` is the position of
  // the trigger character itself; `to` is the caret. Replacing the range
  // `[from, to]` deletes the trigger + query when an action commits.
  const from = $pos.pos - (text.length - triggerIdx);
  const to = $pos.pos;
  // Rect of the trigger character — used to anchor the React menu.
  const rect = view.coordsAtPos(from);
  return {
    trigger,
    from,
    to,
    query,
    rect: {
      top: rect.top,
      left: rect.left,
      bottom: rect.bottom,
      right: rect.right,
    },
  };
}

/**
 * Replace the text between `from` and `to` with `replacement` and dispatch
 * the resulting transaction. Used by the React menu to commit an action
 * (delete the trigger + query, then either insert content or open a
 * dialog).
 */
export function replaceRange(
  view: EditorView,
  from: number,
  to: number,
  replacement: string,
): void {
  const { state } = view;
  let tr = state.tr.delete(from, to);
  if (replacement.length > 0) {
    tr = tr.insertText(replacement, from);
  }
  view.dispatch(tr);
  view.focus();
}

/**
 * Insert markdown text at the current selection without consuming any
 * range. Used after a dialog closes — by then the trigger range was
 * already deleted, so we just append at the caret.
 */
export function insertAtSelection(view: EditorView, replacement: string): void {
  const { state } = view;
  const tr = state.tr.insertText(replacement, state.selection.from);
  view.dispatch(tr);
  view.focus();
}
