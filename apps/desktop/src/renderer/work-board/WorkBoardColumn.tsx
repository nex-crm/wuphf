import type { ThreadBoardColumn, ThreadView } from "@wuphf/protocol/browser";

import { ThreadCard } from "./ThreadCard.tsx";

export interface WorkBoardColumnProps {
  readonly column: ThreadBoardColumn;
  readonly threads: readonly ThreadView[];
  readonly onSelectThread?: ((thread: ThreadView) => void) | undefined;
}

// One kanban column. The column label + ordering is owned here;
// `boardColumn` is the server-derived bucket so the renderer never has
// to invent its own status taxonomy.
export function WorkBoardColumn({ column, threads, onSelectThread }: WorkBoardColumnProps) {
  return (
    <section
      aria-label={columnHeading(column)}
      className="flex min-h-0 flex-col gap-3"
      data-testid="work-board-column"
      data-column={column}
    >
      <header className="flex items-center justify-between">
        <h2 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
          {columnHeading(column)}
        </h2>
        <span
          aria-label={`${threads.length} threads in ${columnHeading(column)}`}
          className="inline-flex h-5 min-w-[1.25rem] items-center justify-center rounded-full bg-muted px-1.5 text-[11px] font-medium text-muted-foreground"
        >
          {threads.length}
        </span>
      </header>
      <div className="flex min-h-0 flex-col gap-2">
        {threads.length === 0 ? (
          <p className="rounded-md border border-dashed border-border px-3 py-6 text-center text-xs text-muted-foreground">
            {emptyHint(column)}
          </p>
        ) : (
          threads.map((thread) => (
            <ThreadCard key={thread.id} thread={thread} onSelect={onSelectThread} />
          ))
        )}
      </div>
    </section>
  );
}

// Column labels — kept renderer-owned because the server vocabulary is
// internal (`needs_me`, etc.) and the UI surfaces a friendlier form.
export function columnHeading(column: ThreadBoardColumn): string {
  switch (column) {
    case "needs_me":
      return "Needs me";
    case "running":
      return "Running";
    case "review":
      return "Review";
    case "done":
      return "Done";
  }
}

function emptyHint(column: ThreadBoardColumn): string {
  switch (column) {
    case "needs_me":
      return "Nothing waiting on you.";
    case "running":
      return "No threads in flight.";
    case "review":
      return "Nothing waiting on review.";
    case "done":
      return "Nothing closed yet.";
  }
}

// Left-to-right ordering of the board. Human-attention-first so
// `needs_me` is what the eye lands on when the window opens. Pure
// constant; exported for tests to assert it doesn't drift.
export const WORK_BOARD_COLUMN_ORDER = ["needs_me", "running", "review", "done"] as const satisfies
  readonly ThreadBoardColumn[];

export const __testing__ = { columnHeading, emptyHint };
