/**
 * IssuesList — Phase 3 /issues list surface.
 *
 * Lists all existing tasks rendered as Issues (back-compat read).
 * Each row shows the task title, status pill, and a link to the issue
 * detail view. No filters in Phase 3 — that is Phase 4+ scope.
 *
 * Data source: GET /tasks?all_channels=true (the existing getOfficeTasks
 * endpoint). No new write endpoints or new primitives.
 */

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
  // Use lifecycle_state if the broker has set it.
  const raw = (task as unknown as Record<string, unknown>).lifecycle_state;
  if (typeof raw === "string" && raw) return raw as LifecycleState;
  // Fall back to legacy status mapping.
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

// ── Sub-components ─────────────────────────────────────────────────────

function IssueRow({ task }: { task: Task }) {
  const state = taskToLifecycleState(task);

  function navigate() {
    void router.navigate({
      to: "/issues/$issueId",
      params: { issueId: task.id },
    });
  }

  return (
    <button
      type="button"
      className="issues-list-row"
      onClick={navigate}
      data-testid="issue-row"
      aria-label={`Issue: ${task.title}, state: ${state}`}
    >
      <span className="issues-list-row-pill">
        <LifecycleStatePill state={state} />
      </span>
      <span className="issues-list-row-title">{task.title}</span>
      {task.owner && (
        <span
          className="issues-list-row-owner"
          aria-label={`Owner: ${task.owner}`}
        >
          {task.owner}
        </span>
      )}
    </button>
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
  const query = useQuery({
    queryKey: ["issues", "list"],
    queryFn: () => getOfficeTasks({ includeDone: true }),
    initialData: initialTasks ? { tasks: initialTasks } : undefined,
    staleTime: 5_000,
    enabled: !initialTasks,
  });

  if (query.isPending && !initialTasks) {
    return <IssuesListSkeleton />;
  }

  if (query.isError && !query.data) {
    return (
      <IssuesListError
        message={
          query.error instanceof Error
            ? query.error.message
            : "Network or broker error."
        }
        onRetry={() => void query.refetch()}
      />
    );
  }

  const tasks = query.data?.tasks ?? [];

  if (tasks.length === 0) {
    return <IssuesEmptyState />;
  }

  return (
    <div className="issues-list" data-testid="issues-list">
      <header className="issues-list-header">
        <h2 className="issues-list-heading">Issues</h2>
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
        className="issues-list-rows"
        role="list"
        aria-label="Issues"
        data-testid="issues-list-rows"
      >
        {tasks.map((task) => (
          <div role="listitem" key={task.id}>
            <IssueRow task={task} />
          </div>
        ))}
      </div>
    </div>
  );
}
