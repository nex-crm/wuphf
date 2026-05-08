/**
 * Floating slash menu shown when the user types `/` in the rich editor.
 *
 * Renders the list of available WUPHF-specific actions filtered by the
 * trigger query. Keyboard navigation: ArrowUp/ArrowDown moves the active
 * row, Enter commits, Escape closes. The menu mounts via portal so it
 * floats above the editor surface without disturbing the document flow.
 */

import { useCallback, useEffect, useState } from "react";
import { createPortal } from "react-dom";

import {
  filterSlashActions,
  type SlashAction,
  type SlashActionDef,
} from "./types";
import { useMenuKeyNav } from "./useMenuKeyNav";

export interface SlashMenuProps {
  query: string;
  position: { top: number; left: number };
  onSelect: (action: SlashAction) => void;
  onClose: () => void;
}

export function SlashMenu({
  query,
  position,
  onSelect,
  onClose,
}: SlashMenuProps): React.ReactElement | null {
  const [activeIdx, setActiveIdx] = useState(0);
  const items: SlashActionDef[] = filterSlashActions(query);

  // Clamp the active index when filtering shrinks the list past it,
  // otherwise Enter would commit a stale selection or out-of-bounds row.
  useEffect(() => {
    if (activeIdx >= items.length) setActiveIdx(Math.max(0, items.length - 1));
  }, [items.length, activeIdx]);

  const onCommit = useCallback(
    (item: SlashActionDef) => onSelect(item.id),
    [onSelect],
  );
  useMenuKeyNav<SlashActionDef>({
    items,
    activeIdx,
    setActiveIdx,
    onCommit,
    onClose,
  });

  if (items.length === 0) {
    return createPortal(
      <div
        className="wk-insert-menu wk-insert-menu--empty"
        data-testid="wk-slash-menu-empty"
        style={floatingStyle(position)}
        role="listbox"
        aria-label="Insert action"
      >
        <div className="wk-insert-menu__empty">No matching actions</div>
      </div>,
      document.body,
    );
  }

  return createPortal(
    <div
      className="wk-insert-menu"
      data-testid="wk-slash-menu"
      style={floatingStyle(position)}
      role="listbox"
      aria-label="Insert action"
    >
      <ul className="wk-insert-menu__list">
        {items.map((item, idx) => (
          <li
            key={item.id}
            className={`wk-insert-menu__item${idx === activeIdx ? " is-active" : ""}`}
          >
            <button
              type="button"
              className="wk-insert-menu__btn"
              role="option"
              aria-selected={idx === activeIdx}
              data-testid={`wk-slash-action-${item.id}`}
              onMouseEnter={() => setActiveIdx(idx)}
              onMouseDown={(e) => {
                // mousedown so the editor doesn't blur first and lose
                // the caret position we need to insert at.
                e.preventDefault();
                onSelect(item.id);
              }}
            >
              <span className="wk-insert-menu__title">{item.title}</span>
              <span className="wk-insert-menu__desc">{item.description}</span>
            </button>
          </li>
        ))}
      </ul>
    </div>,
    document.body,
  );
}

export function floatingStyle(position: {
  top: number;
  left: number;
}): React.CSSProperties {
  return {
    position: "fixed",
    top: `${position.top}px`,
    left: `${position.left}px`,
    zIndex: 50,
  };
}
