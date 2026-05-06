/**
 * Modal multi-picker for the "Insert related pages" action.
 *
 * Shows the catalog grouped by category with a checkbox per entry. The
 * "Insert" button stitches the selected entries into a `## Related`
 * markdown block and hands it back to the controller.
 */

import { useEffect, useMemo, useRef, useState } from "react";

import { buildRelatedBlock } from "./markdownShapes";
import {
  categoryLabel,
  groupMentionItems,
  type MentionItem,
  searchMentionItems,
} from "./mentionCatalog";

export interface RelatedDialogProps {
  items: MentionItem[];
  onConfirm: (block: string) => void;
  onCancel: () => void;
}

export function RelatedDialog({
  items,
  onConfirm,
  onCancel,
}: RelatedDialogProps): React.ReactElement {
  const [query, setQuery] = useState("");
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const queryRef = useRef<HTMLInputElement | null>(null);

  // Auto-focus the search input on mount, matching CitationDialog,
  // DecisionDialog, and FactDialog so all four dialogs share the same
  // first-input focus behavior.
  useEffect(() => {
    queryRef.current?.focus();
  }, []);

  const filtered = useMemo(
    () => searchMentionItems(items, query, 200),
    [items, query],
  );
  const grouped = useMemo(() => groupMentionItems(filtered), [filtered]);

  function togglePick(slug: string): void {
    setPicked((prev) => {
      const next = new Set(prev);
      if (next.has(slug)) next.delete(slug);
      else next.add(slug);
      return next;
    });
  }

  function handleConfirm(): void {
    const entries = Array.from(picked).map((slug) => {
      const item = items.find((i) => i.slug === slug);
      return { slug, display: item?.title };
    });
    const block = buildRelatedBlock(entries);
    if (block.length === 0) return;
    onConfirm(block);
  }

  return (
    <div
      className="wk-modal-backdrop"
      data-testid="wk-related-dialog-backdrop"
      role="dialog"
      aria-modal="true"
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.stopPropagation();
          onCancel();
        }
      }}
    >
      <div
        className="wk-modal wk-insert-dialog"
        data-testid="wk-related-dialog"
      >
        <h2>Insert related pages</h2>
        <label htmlFor="wk-related-search" className="wk-editor-label">
          Filter
        </label>
        <input
          id="wk-related-search"
          ref={queryRef}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search wiki pages"
          data-testid="wk-related-search"
        />
        <div className="wk-insert-dialog__pickers">
          {grouped.length === 0 ? (
            <div className="wk-insert-menu__empty">No matching pages</div>
          ) : (
            grouped.map((g) => (
              <div key={g.category} className="wk-insert-menu__group">
                <div className="wk-insert-menu__group-label">
                  {categoryLabel(g.category)}
                </div>
                <ul className="wk-related-list">
                  {g.items.map((item) => (
                    <li key={item.slug}>
                      <label className="wk-related-row">
                        <input
                          type="checkbox"
                          checked={picked.has(item.slug)}
                          onChange={() => togglePick(item.slug)}
                          data-testid={`wk-related-check-${item.slug}`}
                        />
                        <span className="wk-related-row__title">
                          {item.title}
                        </span>
                        <span className="wk-related-row__slug">
                          {item.slug}
                        </span>
                      </label>
                    </li>
                  ))}
                </ul>
              </div>
            ))
          )}
        </div>
        <div className="wk-insert-dialog__actions">
          <button
            type="button"
            className="wk-editor-save"
            disabled={picked.size === 0}
            onClick={handleConfirm}
            data-testid="wk-related-confirm"
          >
            Insert ({picked.size})
          </button>
          <button
            type="button"
            className="wk-editor-cancel"
            onClick={onCancel}
            data-testid="wk-related-cancel"
          >
            Cancel
          </button>
        </div>
      </div>
    </div>
  );
}
