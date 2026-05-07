/**
 * Floating mention picker shown when the user types `@` in the rich editor.
 *
 * Renders catalog entries grouped by `MentionCategory` and filters by the
 * substring the user has typed since the trigger. Selecting an item
 * inserts a wikilink at the trigger's position. Keyboard nav matches
 * `SlashMenu` (ArrowUp/Down, Enter, Escape).
 *
 * The picker is also reused for the slash-menu actions that need a wiki
 * page picker (link, task ref, agent mention) — `categoryFilter` narrows
 * the surfaced bucket and lets the slash flow re-use the same UI.
 */

import { useEffect, useState } from "react";
import { createPortal } from "react-dom";

import {
  categoryLabel,
  groupMentionItems,
  type MentionCategory,
  type MentionItem,
  searchMentionItems,
} from "./mentionCatalog";
import { floatingStyle } from "./SlashMenu";
import { useMenuKeyNav } from "./useMenuKeyNav";

export interface MentionMenuProps {
  /** All wiki entries available to mention. */
  items: MentionItem[];
  /** Substring filter typed after the trigger character. */
  query: string;
  /** Top-left corner where the menu should float. */
  position: { top: number; left: number };
  /** Optional category constraint. When set, only items in this bucket
   *  are shown — used by slash actions like "Insert task reference"
   *  that intentionally want a single bucket. */
  categoryFilter?: MentionCategory | null;
  /** Optional title displayed at the top of the menu. */
  heading?: string;
  onSelect: (item: MentionItem) => void;
  onClose: () => void;
}

export function MentionMenu({
  items,
  query,
  position,
  categoryFilter,
  heading,
  onSelect,
  onClose,
}: MentionMenuProps): React.ReactElement | null {
  const filtered = categoryFilter
    ? items.filter((i) => i.category === categoryFilter)
    : items;
  const ranked = searchMentionItems(filtered, query, 50);
  const grouped = groupMentionItems(ranked);
  // Flatten to a single array of [categoryHeader, items...] so keyboard
  // nav iterates predictably across buckets.
  const flat: MentionItem[] = [];
  for (const g of grouped) flat.push(...g.items);
  const [activeIdx, setActiveIdx] = useState(0);

  useEffect(() => {
    setActiveIdx((prev) => Math.min(prev, Math.max(0, flat.length - 1)));
  }, [flat.length]);

  useMenuKeyNav<MentionItem>({
    items: flat,
    activeIdx,
    setActiveIdx,
    onCommit: onSelect,
    onClose,
  });

  if (flat.length === 0) {
    return createPortal(
      <div
        className="wk-insert-menu wk-insert-menu--empty"
        data-testid="wk-mention-menu-empty"
        style={floatingStyle(position)}
        role="listbox"
        aria-label={heading ?? "Insert mention"}
      >
        <div className="wk-insert-menu__empty">No matches</div>
      </div>,
      document.body,
    );
  }

  // Precompute slug -> flat index so the render is pure (no mutable
  // counter walked across the grouped map). Order matches `flat`, which
  // is what `useMenuKeyNav` uses for keyboard navigation.
  const slugToFlatIdx = new Map(flat.map((item, i) => [item.slug, i]));
  return createPortal(
    <div
      className="wk-insert-menu wk-insert-menu--mention"
      data-testid="wk-mention-menu"
      style={floatingStyle(position)}
      role="listbox"
      aria-label={heading ?? "Insert mention"}
    >
      {heading ? (
        <div className="wk-insert-menu__heading">{heading}</div>
      ) : null}
      {grouped.map((g) => (
        <div key={g.category} className="wk-insert-menu__group">
          <div className="wk-insert-menu__group-label">
            {categoryLabel(g.category)}
          </div>
          <ul className="wk-insert-menu__list">
            {g.items.map((item) => {
              const idx = slugToFlatIdx.get(item.slug) ?? 0;
              return (
                <li
                  key={item.slug}
                  className={
                    "wk-insert-menu__item" +
                    (idx === activeIdx ? " is-active" : "")
                  }
                >
                  <button
                    type="button"
                    className="wk-insert-menu__btn"
                    role="option"
                    aria-selected={idx === activeIdx}
                    data-testid={`wk-mention-${item.slug}`}
                    onMouseEnter={() => setActiveIdx(idx)}
                    onMouseDown={(e) => {
                      e.preventDefault();
                      onSelect(item);
                    }}
                  >
                    <span className="wk-insert-menu__title">{item.title}</span>
                    <span className="wk-insert-menu__desc">{item.slug}</span>
                  </button>
                </li>
              );
            })}
          </ul>
        </div>
      ))}
    </div>,
    document.body,
  );
}
