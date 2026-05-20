import type { ThreadView } from "@wuphf/protocol/browser";
import { RefreshDouble, WarningTriangle } from "iconoir-react";

import { Button } from "../ui/Button.tsx";
import { useThreadList } from "./useThreadList.ts";
import { WORK_BOARD_COLUMN_ORDER, WorkBoardColumn } from "./WorkBoardColumn.tsx";

export interface WorkBoardProps {
  readonly onSelectThread?: ((thread: ThreadView) => void) | undefined;
}

// Top-level kanban surface. Reads `useThreadList` for the partitioned
// view and renders one column per `boardColumn` value. Loading and
// error states are first-class; query refetch on retry is the only
// imperative path — SSE invalidations from `useBrokerEvents` handle
// the live update path.
export function WorkBoard({ onSelectThread }: WorkBoardProps) {
  const { query, threadsByColumn, totalCount } = useThreadList();

  if (query.isPending && !query.isFetched) {
    return (
      <WorkBoardSkeleton aria-label="Loading work board">
        <p className="text-sm text-muted-foreground">Loading threads…</p>
      </WorkBoardSkeleton>
    );
  }
  if (query.isError) {
    return (
      <WorkBoardSkeleton aria-label="Work board error">
        <div className="flex items-start gap-3 rounded-md border border-red-200 bg-red-50 p-4 text-red-900">
          <WarningTriangle aria-hidden="true" height={18} width={18} />
          <div className="flex-1">
            <p className="text-sm font-medium">Could not load threads</p>
            <p className="mt-1 text-xs">{query.error.message}</p>
          </div>
          <Button
            onClick={() => {
              void query.refetch();
            }}
            variant="secondary"
          >
            <RefreshDouble aria-hidden="true" height={14} width={14} />
            Retry
          </Button>
        </div>
      </WorkBoardSkeleton>
    );
  }

  return (
    <div className="flex flex-col gap-5" data-testid="work-board">
      <header className="flex items-baseline justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-normal">Work</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {totalCount === 1 ? "1 thread" : `${totalCount} threads`}
          </p>
        </div>
      </header>
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4">
        {WORK_BOARD_COLUMN_ORDER.map((column) => (
          <WorkBoardColumn
            key={column}
            column={column}
            threads={threadsByColumn[column]}
            onSelectThread={onSelectThread}
          />
        ))}
      </div>
    </div>
  );
}

interface WorkBoardSkeletonProps {
  readonly "aria-label": string;
  readonly children: React.ReactNode;
}

function WorkBoardSkeleton({ "aria-label": ariaLabel, children }: WorkBoardSkeletonProps) {
  return (
    <div className="flex flex-col gap-5" aria-label={ariaLabel} data-testid="work-board-skeleton">
      <header>
        <h1 className="text-2xl font-semibold tracking-normal">Work</h1>
      </header>
      {children}
    </div>
  );
}
