import type { InboxRow as InboxRowType } from "../../lib/types/lifecycle";
import { LifecycleStatePill } from "./LifecycleStatePill";
import { SeveritySummaryChip } from "./SeveritySummaryChip";

interface InboxRowProps {
  row: InboxRowType;
  isSelected: boolean;
  onOpen: (taskId: string) => void;
  onSelect: (taskId: string) => void;
}

/**
 * One row of the Decision Inbox. Information-dense, scannable, follows
 * the existing wiki article reading rhythm. Hard rule: row-based list,
 * NOT card grid.
 *
 * Touch target ≥44px (min-height enforced in lifecycle.css). Uses a
 * `<button>` element so Enter / Space already trigger onClick natively
 * — no extra onKeyDown handler is needed and adding one for Enter would
 * double-fire onOpen. Selection is tracked via onFocus so ↑/↓ on the
 * parent list moves selection without firing onOpen.
 */
export function InboxRow({ row, isSelected, onOpen, onSelect }: InboxRowProps) {
  return (
    <button
      type="button"
      className="inbox-row"
      data-selected={isSelected ? "true" : "false"}
      data-task-id={row.taskId}
      onClick={() => onOpen(row.taskId)}
      onFocus={() => onSelect(row.taskId)}
      aria-label={`Open task ${row.taskId}: ${row.title}`}
    >
      <span className="inbox-row-main">
        <span className="inbox-row-title">{row.title}</span>
        <span className="inbox-row-assign">{row.assignment}</span>
      </span>
      <SeveritySummaryChip counts={row.severityCounts} />
      <span className="inbox-row-meta">
        <LifecycleStatePill state={row.state} />
        <time
          className={`inbox-row-elapsed${row.isUrgent ? " urgent" : ""}`}
          dateTime={row.lastChangedAt}
          title={`Last changed ${row.elapsed} ago`}
        >
          {row.elapsed}
        </time>
      </span>
    </button>
  );
}
