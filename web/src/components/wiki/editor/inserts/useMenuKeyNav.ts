/**
 * Shared keyboard navigation hook for the slash and mention menus.
 *
 * Wires ArrowUp / ArrowDown / Enter / Escape to a stable item list. The
 * caller passes the current items, the active index state, and the
 * commit/close handlers; the hook attaches/removes a global keydown
 * listener that respects React's stale-closure rules without forcing
 * either menu to memoise its derived list.
 */

import { useEffect, useRef } from "react";

export interface MenuKeyNavArgs<T> {
  items: T[];
  activeIdx: number;
  setActiveIdx: (next: number | ((prev: number) => number)) => void;
  onCommit: (item: T) => void;
  onClose: () => void;
}

export function useMenuKeyNav<T>(args: MenuKeyNavArgs<T>): void {
  // The keydown handler reads the latest values from refs so we don't
  // need every menu to wrap its derived `items` array in `useMemo` just
  // to satisfy useEffect's dependency rule.
  const ref = useRef(args);
  useEffect(() => {
    ref.current = args;
  });

  useEffect(() => {
    function handle(e: KeyboardEvent): void {
      const next = dispatchKey(e.key, ref.current);
      if (next === null) return;
      e.preventDefault();
      next();
    }
    window.addEventListener("keydown", handle, true);
    return () => window.removeEventListener("keydown", handle, true);
  }, []);
}

/**
 * Returns a thunk that performs the action for `key`, or null if the key
 * is not bound. Splitting the dispatch table out of `handle` keeps the
 * keydown branch low-complexity and easy to extend with new shortcuts.
 */
function dispatchKey<T>(
  key: string,
  state: MenuKeyNavArgs<T>,
): (() => void) | null {
  // Tab is treated as Escape so focusing-out of the editor closes the
  // floating menu instead of leaving it mounted with a live keydown
  // listener.
  if (key === "Escape" || key === "Tab") return () => state.onClose();
  const len = state.items.length;
  if (len === 0) return null;
  if (key === "ArrowDown") {
    return () => state.setActiveIdx((i) => (i + 1) % len);
  }
  if (key === "ArrowUp") {
    return () => state.setActiveIdx((i) => (i - 1 + len) % len);
  }
  if (key === "Enter") {
    const picked = state.items[state.activeIdx];
    return picked ? () => state.onCommit(picked) : null;
  }
  return null;
}
