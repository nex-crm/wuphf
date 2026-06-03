/**
 * TasksList — /tasks surface.
 *
 * Renders spec-level tasks (back-compat read of GET /tasks?all_channels=true)
 * as a 7-stage board. The data substrate keeps the broker's granular
 * `lifecycle_state` values; the board groups them into the seven
 * user-facing STAGES it derives in TypeScript (see `stageForState` in
 * lib/types/lifecycle.ts).
 *
 *   Scheduled Tasks · Backlog · In progress · Blocked ·
 *   Needs human input · Done · Archive
 *
 * The Scheduled column is the one exception to the lifecycle grouping —
 * it is fed by routines (the scheduler), not by any lifecycle_state, so
 * each card there is a SchedulerJob that links to its routine detail.
 * Every other card opens the TaskDocument detail surface at /tasks/$taskId.
 */

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { getScheduler, type SchedulerJob } from "../../api/scheduler";
import { getOfficeTasks, type Task } from "../../api/tasks";
import { router } from "../../lib/router";
import { formatTaskTitleForDisplay } from "../../lib/taskTitle";
import {
  type LifecycleStage,
  type LifecycleState,
  STAGE_LABELS,
  STAGE_ORDER,
  stageForState,
} from "../../lib/types/lifecycle";
import {
  routineKey,
  routineLabel,
  routineSchedule,
} from "../apps/routines/routineModel";
import { isCadenceSchedulerJob } from "../apps/schedulerJobClassification";
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
    "archived",
  ]);

function isLifecycleState(value: unknown): value is LifecycleState {
  return (
    typeof value === "string" &&
    KNOWN_LIFECYCLE_STATES.has(value as LifecycleState)
  );
}

export function isTaskSpecTask(task: Task): boolean {
  // Sub-tasks live on the parent Task's detail surface, not on the
  // top-level board. Filtering them out here keeps the board scoped
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
export function taskToLifecycleState(task: Task): LifecycleState {
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
    case "archived":
      return "archived";
    default:
      return "intake";
  }
}

/** Per-stage hint copy shown under the column header. The `scheduled`
 *  column is fed by routines, not lifecycle_state, so its hint reflects
 *  that. */
const STAGE_HINT: Record<LifecycleStage, string> = {
  scheduled: "Recurring routines on a schedule",
  backlog: "Filed, awaiting pickup",
  in_progress: "Owner agent working — includes revising",
  blocked: "Blocked on an upstream merge",
  needs_human: "Awaiting your decision",
  done: "Landed",
  archive: "Filed away — archived or rejected",
};

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

/** Human-readable "next run" sub-line for a scheduled-task card. Prefers
 *  the routine's stored next_run timestamp, falling back to the cadence
 *  summary so the card always carries a schedule signal. */
function scheduledSubLine(job: SchedulerJob): string {
  if (job.next_run) {
    const next = new Date(job.next_run);
    if (!Number.isNaN(next.getTime())) {
      return `Next run ${next.toLocaleString(undefined, {
        month: "short",
        day: "numeric",
        hour: "numeric",
        minute: "2-digit",
      })}`;
    }
  }
  return routineSchedule(job).text;
}

/** Card for the Scheduled Tasks column. Reuses the task-card visual
 *  shell (title + meta sub-line) so the column reads consistently with
 *  the lifecycle columns, but navigates to the routine detail surface
 *  instead of a task detail. */
function ScheduledTaskCard({ job }: { job: SchedulerJob }) {
  const title = routineLabel(job);
  const { slug } = job;

  function navigate() {
    if (slug) {
      void router.navigate({
        to: "/routines/$routineSlug",
        params: { routineSlug: slug },
      });
      return;
    }
    // No slug to deep-link with — fall back to the Routines workspace.
    void router.navigate({ to: "/apps/$appId", params: { appId: "routines" } });
  }

  return (
    <button
      type="button"
      className="issues-kanban-card"
      onClick={navigate}
      data-testid="scheduled-task-row"
      aria-label={`Scheduled task: ${title}`}
    >
      <div className="issues-kanban-card-title">{title || "Untitled"}</div>
      <div className="issues-kanban-card-meta">
        <span className="issues-kanban-card-schedule">
          {scheduledSubLine(job)}
        </span>
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
    // includeDone so the Done + Archive columns populate — the broker
    // otherwise drops landed/closed tasks from the default list.
    queryFn: () => getOfficeTasks({ includeDone: true }),
    initialData: initialTasks ? { tasks: initialTasks } : undefined,
    staleTime: 5_000,
    refetchInterval: 10_000,
    enabled: !initialTasks,
  });

  // Routines feed the Scheduled column. Skipped in test renders (when
  // initialTasks is provided) so unit tests stay free of scheduler mocks.
  const schedulerResult = useQuery({
    queryKey: ["scheduler"],
    queryFn: () => getScheduler(),
    refetchInterval: 15_000,
    enabled: !initialTasks,
  });

  const allTasks = result.data?.tasks ?? [];
  const tasks = useMemo(() => allTasks.filter(isTaskSpecTask), [allTasks]);

  const scheduledJobs = useMemo<SchedulerJob[]>(() => {
    const jobs = schedulerResult.data?.jobs ?? [];
    return jobs.filter(isCadenceSchedulerJob);
  }, [schedulerResult.data]);

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return tasks;
    return tasks.filter((t) => {
      const hay = `${t.title ?? ""} ${t.description ?? ""} ${t.owner ?? ""} ${t.channel ?? ""}`;
      return hay.toLowerCase().includes(needle);
    });
  }, [tasks, query]);

  const filteredScheduled = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return scheduledJobs;
    return scheduledJobs.filter((job) =>
      routineLabel(job).toLowerCase().includes(needle),
    );
  }, [scheduledJobs, query]);

  // Bucket lifecycle tasks by stage. The scheduled stage is filled
  // separately from routines, so it stays empty here.
  const columns = useMemo(() => {
    const buckets: Record<LifecycleStage, Task[]> = {
      scheduled: [],
      backlog: [],
      in_progress: [],
      blocked: [],
      needs_human: [],
      done: [],
      archive: [],
    };
    for (const task of filtered) {
      const stage = stageForState(taskToLifecycleState(task));
      buckets[stage].push(task);
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

  function columnCount(stage: LifecycleStage): number {
    return stage === "scheduled"
      ? filteredScheduled.length
      : columns[stage].length;
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
        {STAGE_ORDER.map((stage) => (
          <section
            key={stage}
            className="issues-kanban-column"
            data-column={stage}
            data-testid={`issues-kanban-column-${stage}`}
          >
            <header className="issues-kanban-column-header">
              <span className="issues-kanban-column-title">
                {STAGE_LABELS[stage]}
              </span>
              <span className="issues-kanban-column-count">
                {columnCount(stage)}
              </span>
            </header>
            <p className="issues-kanban-column-hint">{STAGE_HINT[stage]}</p>
            <ul className="issues-kanban-column-cards">
              {columnCount(stage) === 0 ? (
                <li
                  className="issues-kanban-column-empty"
                  aria-label={`No tasks in ${STAGE_LABELS[stage]}`}
                >
                  —
                </li>
              ) : stage === "scheduled" ? (
                filteredScheduled.map((job) => (
                  <li key={routineKey(job)}>
                    <ScheduledTaskCard job={job} />
                  </li>
                ))
              ) : (
                columns[stage].map((task) => (
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
