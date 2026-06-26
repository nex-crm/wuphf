/**
 * Selection bubble menu for the Tiptap wiki editor.
 *
 * Built for WUPHF's stack — `@tiptap/react/menus` BubbleMenu, design-token
 * CSS classes (no Tailwind / shadcn / oklch), and lucide-free text-glyph
 * buttons so the editor pulls in no icon dependency. Appears on a non-empty
 * text selection.
 *
 * Mark toggles (bold/italic/underline/strike/code/highlight) run inline. The
 * link button delegates to `onRequestLink` so the parent editor owns the link
 * popover and the selection-capture/restore dance (the popover steals focus,
 * which would otherwise collapse the selection).
 *
 * Keyboard: the menu is a `role="toolbar"` with a single tab stop and roving
 * `tabindex` — Left/Right (and Home/End) move focus between actions, matching
 * the WAI-ARIA toolbar pattern. Every action is also reachable from the editor
 * via a documented shortcut: link is Mod-e (owned by the Link extension),
 * highlight is Mod-Shift-h (owned by the Highlight extension), and
 * bold/italic/underline/strike/code keep their StarterKit defaults.
 */

import { useRef } from "react";
import type { Editor } from "@tiptap/react";
import { BubbleMenu } from "@tiptap/react/menus";

/** Highlight swatches keyed to wiki tokens so they read in every theme. */
const HIGHLIGHT_COLORS: { label: string; value: string }[] = [
  { label: "Amber", value: "var(--wk-amber-bg)" },
  { label: "Olive", value: "var(--olive-200)" },
  { label: "Clear", value: "" },
];

export interface EditorBubbleMenuProps {
  editor: Editor | null;
  /** Opens the link popover for the current selection. The parent captures
   *  the selection rect + existing href before the popover steals focus. */
  onRequestLink: () => void;
}

export function EditorBubbleMenu({
  editor,
  onRequestLink,
}: EditorBubbleMenuProps): React.ReactElement | null {
  // The toolbar implements roving tabindex: one button is in the tab order
  // (tabindex 0), the rest are -1, and Arrow/Home/End move focus + the tab
  // stop. We read/write focus straight off the DOM so the active index never
  // drifts from what the user sees.
  const toolbarRef = useRef<HTMLDivElement | null>(null);

  if (!editor) return null;

  const btnClass = (active: boolean): string =>
    `wk-bubble-menu__btn${active ? " is-active" : ""}`;

  // mousedown preventDefault keeps the editor selection alive while the
  // button is pressed — otherwise the click blurs the editor first.
  const guard = (e: React.MouseEvent): void => {
    e.preventDefault();
  };

  const buttons = (): HTMLButtonElement[] => {
    const root = toolbarRef.current;
    if (!root) return [];
    return Array.from(root.querySelectorAll<HTMLButtonElement>("button"));
  };

  const moveFocus = (delta: number): void => {
    const all = buttons();
    if (all.length === 0) return;
    const active =
      document.activeElement instanceof HTMLButtonElement
        ? document.activeElement
        : null;
    const current = active ? all.indexOf(active) : -1;
    const base = current === -1 ? 0 : current;
    const next = (base + delta + all.length) % all.length;
    focusAt(all, next);
  };

  const focusEdge = (which: "first" | "last"): void => {
    const all = buttons();
    if (all.length === 0) return;
    focusAt(all, which === "first" ? 0 : all.length - 1);
  };

  const focusAt = (all: HTMLButtonElement[], idx: number): void => {
    for (const [i, b] of all.entries()) {
      b.tabIndex = i === idx ? 0 : -1;
    }
    all[idx]?.focus();
  };

  const onToolbarKeyDown = (e: React.KeyboardEvent): void => {
    switch (e.key) {
      case "ArrowRight":
      case "ArrowDown":
        e.preventDefault();
        moveFocus(1);
        break;
      case "ArrowLeft":
      case "ArrowUp":
        e.preventDefault();
        moveFocus(-1);
        break;
      case "Home":
        e.preventDefault();
        focusEdge("first");
        break;
      case "End":
        e.preventDefault();
        focusEdge("last");
        break;
    }
  };

  // First action owns the single tab stop; the rest are reachable via arrows.
  let tabbableAssigned = false;
  const nextTabIndex = (): 0 | -1 => {
    if (tabbableAssigned) return -1;
    tabbableAssigned = true;
    return 0;
  };

  return (
    <BubbleMenu
      editor={editor}
      options={{ placement: "top", offset: 8 }}
      className="wk-bubble-menu"
      data-testid="wk-bubble-menu"
    >
      <div
        ref={toolbarRef}
        className="wk-bubble-menu__toolbar"
        role="toolbar"
        aria-label="Text formatting"
        aria-orientation="horizontal"
        onKeyDown={onToolbarKeyDown}
      >
        <button
          type="button"
          className={btnClass(editor.isActive("bold"))}
          tabIndex={nextTabIndex()}
          onMouseDown={guard}
          onClick={() => editor.chain().focus().toggleBold().run()}
          aria-label="Bold"
          data-testid="wk-bubble-bold"
        >
          <strong>B</strong>
        </button>
        <button
          type="button"
          className={btnClass(editor.isActive("italic"))}
          tabIndex={nextTabIndex()}
          onMouseDown={guard}
          onClick={() => editor.chain().focus().toggleItalic().run()}
          aria-label="Italic"
          data-testid="wk-bubble-italic"
        >
          <em>i</em>
        </button>
        <button
          type="button"
          className={btnClass(editor.isActive("underline"))}
          tabIndex={nextTabIndex()}
          onMouseDown={guard}
          onClick={() => editor.chain().focus().toggleUnderline().run()}
          aria-label="Underline"
          data-testid="wk-bubble-underline"
        >
          <span style={{ textDecoration: "underline" }}>U</span>
        </button>
        <button
          type="button"
          className={btnClass(editor.isActive("strike"))}
          tabIndex={nextTabIndex()}
          onMouseDown={guard}
          onClick={() => editor.chain().focus().toggleStrike().run()}
          aria-label="Strikethrough"
          data-testid="wk-bubble-strike"
        >
          <span style={{ textDecoration: "line-through" }}>S</span>
        </button>
        <button
          type="button"
          className={btnClass(editor.isActive("code"))}
          tabIndex={nextTabIndex()}
          onMouseDown={guard}
          onClick={() => editor.chain().focus().toggleCode().run()}
          aria-label="Inline code"
          data-testid="wk-bubble-code"
        >
          <code>{"<>"}</code>
        </button>
        <span className="wk-bubble-menu__sep" aria-hidden="true" />
        <button
          type="button"
          className={btnClass(editor.isActive("link"))}
          tabIndex={nextTabIndex()}
          onMouseDown={guard}
          onClick={onRequestLink}
          aria-label="Link"
          aria-keyshortcuts="Meta+E Control+E"
          data-testid="wk-bubble-link"
        >
          link
        </button>
        <span className="wk-bubble-menu__sep" aria-hidden="true" />
        {HIGHLIGHT_COLORS.map((c) => (
          <button
            key={c.label}
            type="button"
            className={btnClass(
              c.value !== "" &&
                editor.isActive("highlight", { color: c.value }),
            )}
            tabIndex={nextTabIndex()}
            onMouseDown={guard}
            onClick={() => {
              if (c.value === "") {
                editor.chain().focus().unsetHighlight().run();
              } else {
                editor
                  .chain()
                  .focus()
                  .toggleHighlight({ color: c.value })
                  .run();
              }
            }}
            aria-label={`Highlight ${c.label}`}
            aria-keyshortcuts={
              c.value === "" ? undefined : "Meta+Shift+H Control+Shift+H"
            }
            data-testid={`wk-bubble-highlight-${c.label.toLowerCase()}`}
          >
            {c.value === "" ? (
              "×"
            ) : (
              <span
                className="wk-bubble-menu__swatch"
                style={{ background: c.value }}
              />
            )}
          </button>
        ))}
      </div>
    </BubbleMenu>
  );
}
