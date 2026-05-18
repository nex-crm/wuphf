/**
 * IssuesList — /issues surface.
 *
 * Renders all office tasks (back-compat read of GET /tasks?all_channels=true)
 * as a lifecycle kanban. Six columns: Draft / Intake / Running / Review /
 * Approved / Rejected. Each card opens the IssueDocument detail surface at
 * /issues/$issueId.
 *
 * Replaces the previous flat list view — the kanban surfaces movement across
 * the agent workflow that the old TasksApp used to show.
 */

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { getOfficeTasks, type Task } from "../../api/tasks";
import { router } from "../../lib/router";
import type { LifecycleState } from "../../lib/types/lifecycle";
import { LifecycleStatePill } from "./LifecycleStatePill";

// ── Helpers ────────────────────────────────────────────────────────────

/**
 * Map a Task's raw status/lifecycle_state fields to a LifecycleState.
 * Prefers `lifecycle_state` (set by the broker post Lane-A); falls back
 * to the legacy `status` string so pre-Lane-A tasks still render a pill.
 */
function taskToLifecycleState(task: Task): LifecycleState {
  if (task.pipeline_stage === "draft") return "drafting";
  const raw = (task as unknown as Record<string, unknown>).lifecycle_state;
  if (typeof raw === "string" && raw) return raw as LifecycleState;
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
  | "intake"
  | "running"
  | "review"
  | "approved"
  | "rejected";

const COLUMN_ORDER: readonly ColumnId[] = [
  "drafting",
  "intake",
  "running",
  "review",
  "approved",
  "rejected",
];

const COLUMN_LABEL: Record<ColumnId, string> = {
  drafting: "Draft",
  intake: "Intake",
  running: "Running",
  review: "Review",
  approved: "Approved",
  rejected: "Rejected",
};

const COLUMN_HINT: Record<ColumnId, string> = {
  drafting: "Filed but not picked up",
  intake: "Owner agent gathering spec",
  running: "Active work + blocked items",
  review: "Awaiting reviewer grades or human decision",
  approved: "Landed",
  rejected: "Will not land",
};

/** Map a lifecycle state to its kanban column. */
function lifecycleToColumn(state: LifecycleState): ColumnId {
  switch (state) {
    case "drafting":
      return "drafting";
    case "intake":
    case "ready":
      return "intake";
    case "running":
    case "changes_requested":
    case "blocked_on_pr_merge":
      return "running";
    case "review":
    case "decision":
      return "review";
    case "approved":
      return "approved";
    case "rejected":
      return "rejected";
  }
}

// ── Sub-components ─────────────────────────────────────────────────────

function IssueCard({ task }: { task: Task }) {
  const state = taskToLifecycleState(task);

  function navigate() {
    void router.navigate({
      to: "/issues/$issueId",
      params: { issueId: task.id },
    });
  }

  function handleKey(e: React.KeyboardEvent<HTMLDivElement>) {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      navigate();
    }
  }

  return (
    <div
      className="issues-kanban-card"
      role="button"
      tabIndex={0}
      onClick={navigate}
      onKeyDown={handleKey}
      data-testid="issue-row"
      aria-label={`Issue: ${task.title}, state: ${state}`}
    >
      <div className="issues-kanban-card-title">{task.title || "Untitled"}</div>
      <div className="issues-kanban-card-meta">
        <LifecycleStatePill state={state} />
        {task.owner ? (
          <span className="issues-kanban-card-owner">@{task.owner}</span>
        ) : null}
        {task.channel ? (
          <span className="issues-kanban-card-channel">#{task.channel}</span>
        ) : null}
      </div>
    </div>
  );
}

function IssuesListSkeleton() {
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

function IssuesListError({
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
        <strong>Could not load issues</strong>
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

function IssuesEmptyState() {
  return (
    <div
      className="issues-list issues-list--empty"
      data-testid="issues-list-empty"
    >
      <p className="issues-empty-copy">
        No issues yet. When you are ready, type what you want done.
      </p>
      <button
        type="button"
        className="issues-new-btn"
        onClick={() => void router.navigate({ to: "/issues/new" })}
        data-testid="issues-new-btn"
      >
        + New issue
      </button>
    </div>
  );
}

// ── Main component ─────────────────────────────────────────────────────

interface IssuesListProps {
  /** Used in tests to skip the fetch. */
  initialTasks?: Task[];
}

export function IssuesList({ initialTasks }: IssuesListProps = {}) {
  const [query, setQuery] = useState("");

  const result = useQuery({
    queryKey: ["issues", "list"],
    queryFn: () => getOfficeTasks({ includeDone: true }),
    initialData: initialTasks ? { tasks: initialTasks } : undefined,
    staleTime: 5_000,
    refetchInterval: 10_000,
    enabled: !initialTasks,
  });

  const tasks = result.data?.tasks ?? [];

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
      intake: [],
      running: [],
      review: [],
      approved: [],
      rejected: [],
    };
    for (const task of filtered) {
      const col = lifecycleToColumn(taskToLifecycleState(task));
      buckets[col].push(task);
    }
    return buckets;
  }, [filtered]);

  if (result.isPending && !initialTasks) {
    return <IssuesListSkeleton />;
  }

  if (result.isError && !result.data) {
    return (
      <IssuesListError
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
    return <IssuesEmptyState />;
  }

  return (
    <div className="issues-list issues-list--kanban" data-testid="issues-list">
      <header className="issues-list-header">
        <h2 className="issues-list-heading">Issues</h2>
        <input
          type="search"
          className="issues-list-search"
          placeholder="Filter…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          aria-label="Filter issues"
          data-testid="issues-list-search"
        />
        <button
          type="button"
          className="issues-new-btn issues-new-btn--header"
          onClick={() => void router.navigate({ to: "/issues/new" })}
          data-testid="issues-new-btn"
          title="Create a new issue"
        >
          + New issue
        </button>
      </header>
      <div
        className="issues-kanban"
        role="list"
        aria-label="Issues kanban"
        data-testid="issues-list-rows"
      >
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
            <div className="issues-kanban-column-cards" role="list">
              {columns[col].length === 0 ? (
                <p className="issues-kanban-column-empty">—</p>
              ) : (
                columns[col].map((task) => (
                  <div role="listitem" key={task.id}>
                    <IssueCard task={task} />
                  </div>
                ))
              )}
            </div>
          </section>
        ))}
      </div>
    </div>
  );
}
