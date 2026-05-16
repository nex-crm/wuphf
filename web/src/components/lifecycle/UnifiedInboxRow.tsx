import type { InboxItem } from "../../lib/types/inbox";
import { renderInboxItemKey } from "../../lib/types/inbox";
import { InboxRow } from "./InboxRow";

interface UnifiedInboxRowProps {
  item: InboxItem;
  isSelected: boolean;
  tabIndex?: number;
  onOpen: (item: InboxItem) => void;
  onSelect: (item: InboxItem) => void;
}

/**
 * Dispatcher for one row of the unified Decision Inbox. Switches on
 * the InboxItem discriminator and routes to the right per-kind
 * renderer. Adding a new kind without extending this switch is a
 * compile error (the default branch's `_exhaustive: never`
 * assignment catches the missing case).
 *
 * Task rows reuse the existing InboxRow component so the visual
 * rhythm and a11y semantics stay identical to the legacy
 * task-only inbox. Request and review rows are simpler — no
 * severity chip, just title + meta — so they render inline rather
 * than fan out to their own components.
 */
export function UnifiedInboxRow({
  item,
  isSelected,
  tabIndex = 0,
  onOpen,
  onSelect,
}: UnifiedInboxRowProps) {
  switch (item.kind) {
    case "task":
      return (
        <InboxRow
          row={item.task}
          isSelected={isSelected}
          tabIndex={tabIndex}
          onOpen={() => onOpen(item)}
          onSelect={() => onSelect(item)}
        />
      );
    case "request":
      return (
        <RequestRow
          item={item}
          isSelected={isSelected}
          tabIndex={tabIndex}
          onOpen={onOpen}
          onSelect={onSelect}
        />
      );
    case "review":
      return (
        <ReviewRow
          item={item}
          isSelected={isSelected}
          tabIndex={tabIndex}
          onOpen={onOpen}
          onSelect={onSelect}
        />
      );
    default: {
      const _exhaustive: never = item;
      return _exhaustive;
    }
  }
}

function RequestRow({
  item,
  isSelected,
  tabIndex,
  onOpen,
  onSelect,
}: {
  item: Extract<InboxItem, { kind: "request" }>;
  isSelected: boolean;
  tabIndex: number;
  onOpen: (item: InboxItem) => void;
  onSelect: (item: InboxItem) => void;
}) {
  return (
    <button
      type="button"
      className="inbox-row inbox-row--request"
      data-kind="request"
      data-selected={isSelected ? "true" : "false"}
      data-request-id={item.requestId}
      tabIndex={tabIndex}
      onClick={() => onOpen(item)}
      onFocus={() => onSelect(item)}
      aria-label={`Open request ${item.requestId}: ${item.title}`}
    >
      <span className="inbox-row-main">
        <span className="inbox-row-title">
          <span className="inbox-row-kind-pill">request</span> {item.title}
        </span>
        <span className="inbox-row-assign">{item.request.question || "—"}</span>
      </span>
      <span className="inbox-row-meta">
        <span className="inbox-row-from">{item.request.from || "owner"}</span>
        {item.request.blocking ? (
          <span className="inbox-row-blocking">blocking</span>
        ) : null}
      </span>
    </button>
  );
}

function ReviewRow({
  item,
  isSelected,
  tabIndex,
  onOpen,
  onSelect,
}: {
  item: Extract<InboxItem, { kind: "review" }>;
  isSelected: boolean;
  tabIndex: number;
  onOpen: (item: InboxItem) => void;
  onSelect: (item: InboxItem) => void;
}) {
  return (
    <button
      type="button"
      className="inbox-row inbox-row--review"
      data-kind="review"
      data-selected={isSelected ? "true" : "false"}
      data-review-id={item.reviewId}
      tabIndex={tabIndex}
      onClick={() => onOpen(item)}
      onFocus={() => onSelect(item)}
      aria-label={`Open review ${item.reviewId}: ${item.title}`}
    >
      <span className="inbox-row-main">
        <span className="inbox-row-title">
          <span className="inbox-row-kind-pill">review</span> {item.title}
        </span>
        <span className="inbox-row-assign">
          {item.review.sourceSlug} → {item.review.targetPath}
        </span>
      </span>
      <span className="inbox-row-meta">
        <span className="inbox-row-reviewer">
          {item.review.reviewerSlug || "—"}
        </span>
        <span className="inbox-row-state">{item.review.state}</span>
      </span>
    </button>
  );
}

/**
 * Re-export for tests that want to assert on row keys without
 * pulling in the renderer.
 */
export { renderInboxItemKey };
