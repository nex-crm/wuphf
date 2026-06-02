/**
 * Slash-trigger watcher for the Tiptap wiki editor.
 *
 * Ported from cabinet's `slash-commands.tsx` keydown approach: a `/` at the
 * start of a node (or after whitespace) opens a coordinate-positioned menu;
 * subsequent typing builds the query; Backspace past the trigger or Space /
 * Escape closes it. The menu's own keyboard nav (Arrow/Enter) is owned by the
 * rendered `EditorSlashMenu` via the shared `useMenuKeyNav` hook — this watcher
 * only manages open/close/query/position and active-index reset.
 *
 * Returns the live menu state plus a `reset` callback the parent calls after
 * committing a command (so the menu closes and the query clears).
 */

import { useCallback, useEffect, useRef, useState } from "react";
import type { Editor } from "@tiptap/react";

export interface SlashMenuState {
  open: boolean;
  query: string;
  position: { top: number; left: number };
  activeIdx: number;
  setActiveIdx: (next: number | ((prev: number) => number)) => void;
  /** Caret position of the `/` trigger, so the parent can delete `/query`
   *  before running the command. */
  triggerFrom: number;
  reset: () => void;
}

export function useSlashMenu(editor: Editor | null): SlashMenuState {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [activeIdx, setActiveIdx] = useState(0);
  const [position, setPosition] = useState({ top: 0, left: 0 });
  const triggerFromRef = useRef(0);

  // Mirror the live state into a ref so the global keydown handler (attached
  // once) reads current values without re-binding on every keystroke.
  const stateRef = useRef({ open, query });
  useEffect(() => {
    stateRef.current = { open, query };
  }, [open, query]);

  const reset = useCallback(() => {
    setOpen(false);
    setQuery("");
    setActiveIdx(0);
  }, []);

  const openAt = useCallback((ed: Editor) => {
    const { from } = ed.state.selection;
    const coords = ed.view.coordsAtPos(from);
    triggerFromRef.current = from;
    setPosition({ top: coords.bottom + 4, left: coords.left });
    setOpen(true);
    setQuery("");
    setActiveIdx(0);
  }, []);

  useEffect(() => {
    if (!editor) return;

    const canTrigger = (ed: Editor): boolean => {
      const { from } = ed.state.selection;
      const before = ed.state.doc.textBetween(Math.max(0, from - 1), from);
      return from === 1 || before === "" || before === "\n" || before === " ";
    };

    const backspace = (): void => {
      if (stateRef.current.query.length === 0) {
        reset();
        return;
      }
      setQuery((prev) => prev.slice(0, -1));
      setActiveIdx(0);
    };

    // While open: only intercept the keys that mutate the query or close.
    // Arrow/Enter are owned by the rendered menu's own keydown listener.
    const handleOpen = (event: KeyboardEvent): void => {
      if (event.key === "Escape" || event.key === " ") {
        reset();
        return;
      }
      if (event.key === "Backspace") {
        backspace();
        return;
      }
      if (event.key.length === 1 && !event.metaKey && !event.ctrlKey) {
        setQuery((prev) => prev + event.key);
        setActiveIdx(0);
      }
    };

    const handle = (event: KeyboardEvent): void => {
      if (!stateRef.current.open) {
        if (event.key === "/" && canTrigger(editor)) openAt(editor);
        return;
      }
      handleOpen(event);
    };

    window.addEventListener("keydown", handle, true);
    return () => window.removeEventListener("keydown", handle, true);
  }, [editor, openAt, reset]);

  return {
    open,
    query,
    position,
    activeIdx,
    setActiveIdx,
    triggerFrom: triggerFromRef.current,
    reset,
  };
}
