/**
 * TasksList — /tasks surface.
 *
 * Renders spec-level tasks (back-compat read of GET /tasks?all_channels=true)
 * as a lifecycle kanban. Six columns: Draft / Intake / Running / Review /
 * Approved / Rejected. Each card opens the TaskDocument detail surface at
 * /tasks/$taskId.
 *
 * Replaces the previous flat list view — the kanban surfaces movement across
 * the agent workflow that the old office board used to show.
 */

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { getOfficeTasks, type Task } from "../../api/tasks";
import { router } from "../../lib/router";
import { formatTaskTitleForDisplay } from "../../lib/taskTitle";
import type { LifecycleState } from "../../lib/types/lifecycle";
import { TaskCreateDialog } from "../tasks/TaskCreateDialog";
import { LifecycleStatePill } from "./LifecycleStatePill";

// ── Helpers ────────────────────────────────────────────────────────────

const KNOWN_LIFECYCLE_STATES: ReadonlySet<LifecycleState> =
  new Set<LifecycleState>([
    "drafting",
    "intake",
    "ready",
    "running",
    "review",
    "decision",
    "blocked_on_pr_merge",
    "changes_requested",
    "approved",
    "rejected",
  ]);

function isLifecycleState(value: unknown): value is LifecycleState {
  return (
    typeof value === "string" &&
    KNOWN_LIFECYCLE_STATES.has(value as LifecycleState)
  );
}

function isTaskSpecTask(task: Task): boolean {
  // Sub-tasks live on the parent Task's detail surface, not on the
  // top-level kanban. Filtering them out here keeps the board scoped
  // to "real" Tasks; children stay reachable via the parent Task.
  if (task.parent_issue_id && task.parent_issue_id.length > 0) {
    return false;
  }
  return (
    task.task_type === "issue" ||
    task.pipeline_id === "issue" ||
    Boolean(task.issue_draft_spec)
  );
}

/**
 * Map a Task's raw status/lifecycle_state fields to a LifecycleState.
 * Prefers `lifecycle_state` (set by the broker post Lane-A); falls back
 * to the legacy `status` string so pre-Lane-A tasks still render a pill.
 * Unknown wire-shape strings (legacy broker rows) fall through to the
 * status-driven mapping so `LifecycleStatePill` never receives a value
 * outside the typed union.
 */
function taskToLifecycleState(task: Task): LifecycleState {
  if (task.pipeline_stage === "draft") return "drafting";
  const raw = (task as unknown as Record<string, unknown>).lifecycle_state;
  if (isLifecycleState(raw)) return raw;
  switch (task.status) {
    case "open":
      return "intake";
    case "in_progress":
      return "running";
    case "done":
      return "approved";
    case "blocked":
      return "blocked_on_pr_merge";
    case "review":
      return "review";
    case "rejected":
      return "rejected";
    default:
      return "intake";
  }
}

type ColumnId =
  | "drafting"
  | "todo"
  | "in_progress"
  | "in_review"
  | "done"
  | "cancelled";

// Linear-style 6-column layout. The broker's underlying 11-state machine
// keeps its current shape (touched in Slice 4 follow-up); this is the
// presentation projection that humans see on the board.
const COLUMN_ORDER: readonly ColumnId[] = [
  "drafting",
  "todo",
  "in_progress",
  "in_review",
  "done",
  "cancelled",
];

const COLUMN_LABEL: Record<ColumnId, string> = {
  drafting: "Backlog",
  todo: "Todo",
  in_progress: "In Progress",
  in_review: "In Review",
  done: "Done",
  cancelled: "Cancelled",
};

const COLUMN_HINT: Record<ColumnId, string> = {
  drafting: "Filed, awaiting your approval",
  todo: "Approved, not yet picked up",
  in_progress: "Owner agent working — includes blocked / revising",
  in_review: "Awaiting reviewer grades or human decision",
  done: "Landed",
  cancelled: "Will not land",
};

/** Project a broker lifecycle state onto the Linear-style column id.
 *  The broker keeps 11 internal states; this maps them onto the 6
 *  presentation columns. Wire shape from the broker can drift (legacy
 *  "blocked", "in_progress" strings, unknown future states), so the
 *  default lands those in Backlog instead of crashing. */
function lifecycleToColumn(state: LifecycleState | string): ColumnId {
  switch (state) {
    case "drafting":
      return "drafting";
    case "intake":
    case "ready":
    case "queued_behind_owner":
      return "todo";
    case "running":
    case "changes_requested":
    case "blocked_on_pr_merge":
    case "in_progress":
      return "in_progress";
    case "review":
    case "decision":
      return "in_review";
    case "approved":
      return "done";
    case "rejected":
      return "cancelled";
    default:
      return "drafting";
  }
}

// ── Sub-components ─────────────────────────────────────────────────────

function TaskCard({ task }: { task: Task }) {
  const state = taskToLifecycleState(task);

  function navigate() {
    void router.navigate({
      to: "/tasks/$taskId",
      params: { taskId: task.id },
    });
  }

  return (
    <button
      type="button"
      className="issues-kanban-card"
      onClick={navigate}
      data-testid="issue-row"
      aria-label={`Task: ${formatTaskTitleForDisplay(task.title)}, state: ${state}`}
    >
      <div className="issues-kanban-card-title">
        {formatTaskTitleForDisplay(task.title) || "Untitled"}
      </div>
      <div className="issues-kanban-card-meta">
        <LifecycleStatePill state={state} />
        {task.owner ? (
          <span className="issues-kanban-card-owner">@{task.owner}</span>
        ) : null}
        {task.channel ? (
          <span className="issues-kanban-card-channel">#{task.channel}</span>
        ) : null}
      </div>
    </button>
  );
}

function TasksListSkeleton() {
  return (
    <div
      className="issues-list issues-list--loading"
      data-testid="issues-list-loading"
      aria-busy="true"
    >
      {[0, 1, 2, 3].map((i) => (
        <div
          key={i}
          className="issues-list-skeleton-row"
          style={{ width: `${50 + i * 10}%` }}
        />
      ))}
    </div>
  );
}

function TasksListError({
  message,
  onRetry,
}: {
  message: string;
  onRetry: () => void;
}) {
  return (
    <div
      className="issues-list issues-list--error"
      data-testid="issues-list-error"
    >
      <div role="alert" className="issues-list-error-card">
        <strong>Could not load tasks</strong>
        <p>{message}</p>
        <button
          type="button"
          onClick={onRetry}
          className="issues-list-retry-btn"
        >
          Retry
        </button>
      </div>
    </div>
  );
}

function TasksEmptyState({ onOpenCreate }: { onOpenCreate: () => void }) {
  return (
    <div
      className="issues-list issues-list--empty"
      data-testid="issues-list-empty"
    >
      <p className="issues-empty-copy">
        No task specs yet. File larger project work here, then cut it into agent
        tasks.
      </p>
      <button
        type="button"
        className="issues-new-btn"
        onClick={onOpenCreate}
        data-testid="issues-new-btn"
      >
        + New task
      </button>
    </div>
  );
}

// ── Main component ─────────────────────────────────────────────────────

interface TasksListProps {
  /** Used in tests to skip the fetch. */
  initialTasks?: Task[];
}

export function TasksList({ initialTasks }: TasksListProps = {}) {
  const [query, setQuery] = useState("");
  // Inline dialog replaces /tasks/new full-page form for the in-app path.
  // The route stays mounted as a fallback for direct URL navigation.
  const [createOpen, setCreateOpen] = useState(false);

  const result = useQuery({
    queryKey: ["issues", "list"],
    queryFn: () => getOfficeTasks({ includeDone: true }),
    initialData: initialTasks ? { tasks: initialTasks } : undefined,
    staleTime: 5_000,
    refetchInterval: 10_000,
    enabled: !initialTasks,
  });

  const allTasks = result.data?.tasks ?? [];
  const tasks = useMemo(() => allTasks.filter(isTaskSpecTask), [allTasks]);

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return tasks;
    return tasks.filter((t) => {
      const hay = `${t.title ?? ""} ${t.description ?? ""} ${t.owner ?? ""} ${t.channel ?? ""}`;
      return hay.toLowerCase().includes(needle);
    });
  }, [tasks, query]);

  const columns = useMemo(() => {
    const buckets: Record<ColumnId, Task[]> = {
      drafting: [],
      todo: [],
      in_progress: [],
      in_review: [],
      done: [],
      cancelled: [],
    };
    for (const task of filtered) {
      const col = lifecycleToColumn(taskToLifecycleState(task));
      buckets[col].push(task);
    }
    return buckets;
  }, [filtered]);

  if (result.isPending && !initialTasks) {
    return <TasksListSkeleton />;
  }

  if (result.isError && !result.data) {
    return (
      <TasksListError
        message={
          result.error instanceof Error
            ? result.error.message
            : "Network or broker error."
        }
        onRetry={() => void result.refetch()}
      />
    );
  }

  if (tasks.length === 0) {
    return (
      <>
        <TasksEmptyState onOpenCreate={() => setCreateOpen(true)} />
        <TaskCreateDialog open={createOpen} onOpenChange={setCreateOpen} />
      </>
    );
  }

  return (
    <div className="issues-list issues-list--kanban" data-testid="issues-list">
      <header className="issues-list-header">
        <h2 className="issues-list-heading">Tasks</h2>
        <input
          type="search"
          className="issues-list-search"
          placeholder="Filter…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          aria-label="Filter tasks"
          data-testid="issues-list-search"
        />
        <button
          type="button"
          className="issues-new-btn issues-new-btn--header"
          onClick={() => setCreateOpen(true)}
          data-testid="issues-new-btn"
          title="Create a new task"
        >
          + New task
        </button>
      </header>
      <TaskCreateDialog open={createOpen} onOpenChange={setCreateOpen} />
      {/*
        The outer wrapper is a layout grid of columns, not a list — each
        column has its own role="list" of cards below. Putting role="list"
        here too with <section> children that aren't role="listitem" would
        be invalid ARIA, so the wrapper is left as a plain <div>.
      */}
      <div className="issues-kanban" data-testid="issues-list-rows">
        {COLUMN_ORDER.map((col) => (
          <section
            key={col}
            className="issues-kanban-column"
            data-column={col}
            data-testid={`issues-kanban-column-${col}`}
          >
            <header className="issues-kanban-column-header">
              <span className="issues-kanban-column-title">
                {COLUMN_LABEL[col]}
              </span>
              <span className="issues-kanban-column-count">
                {columns[col].length}
              </span>
            </header>
            <p className="issues-kanban-column-hint">{COLUMN_HINT[col]}</p>
            <ul className="issues-kanban-column-cards">
              {columns[col].length === 0 ? (
                <li
                  className="issues-kanban-column-empty"
                  aria-label={`No tasks in ${COLUMN_LABEL[col]}`}
                >
                  —
                </li>
              ) : (
                columns[col].map((task) => (
                  <li key={task.id}>
                    <TaskCard task={task} />
                  </li>
                ))
              )}
            </ul>
          </section>
        ))}
      </div>
    </div>
  );
}
