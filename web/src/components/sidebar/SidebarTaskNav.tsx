/**
 * SidebarTaskNav — the primary sidebar surface in the task-scoped model.
 *
 * Replaces the old Agents + Channels nav sections. Channels are now per
 * task, so the sidebar lists the operator's Tasks grouped by the same
 * seven stages as the /tasks board (see stageForState). Each task links
 * to its detail surface (/tasks/$taskId, which carries its channel);
 * Scheduled entries are routines and link to the routine detail page.
 *
 * It is navigation, not the board: stages render as collapsible groups,
 * with the active stages (Backlog / In progress / Requires human input)
 * open by default and the quieter ones (Scheduled / Blocked / Done /
 * Archive) collapsed.
 */

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { getScheduler, type SchedulerJob } from "../../api/scheduler";
import { getOfficeTasks, type Task } from "../../api/tasks";
import { router } from "../../lib/router";
import { formatTaskTitleForDisplay } from "../../lib/taskTitle";
import {
  type LifecycleStage,
  STAGE_LABELS,
  STAGE_ORDER,
  stageForState,
} from "../../lib/types/lifecycle";
import { useCurrentRoute } from "../../routes/useCurrentRoute";
import { routineKey, routineLabel } from "../apps/routines/routineModel";
import { isCadenceSchedulerJob } from "../apps/schedulerJobClassification";
import { TaskStatusDot } from "../lifecycle/TaskActivityStream";
import { isTaskSpecTask, taskToLifecycleState } from "../lifecycle/TasksList";

const DEFAULT_OPEN: ReadonlySet<LifecycleStage> = new Set<LifecycleStage>([
  "backlog",
  "in_progress",
  "needs_human",
]);

function openBoard(): void {
  void router.navigate({ to: "/tasks" });
}

function openNewTask(): void {
  // The home composer (index `/`) is the primary new-task surface; the
  // /tasks/new form remains as a fallback for direct links.
  void router.navigate({ to: "/" });
}

function openTask(taskId: string): void {
  void router.navigate({ to: "/tasks/$taskId", params: { taskId } });
}

function openRoutine(slug: string | undefined): void {
  if (slug) {
    void router.navigate({
      to: "/routines/$routineSlug",
      params: { routineSlug: slug },
    });
    return;
  }
  void router.navigate({ to: "/apps/$appId", params: { appId: "routines" } });
}

// ── Per-stage group ────────────────────────────────────────────────────

interface StageGroupProps {
  stage: LifecycleStage;
  count: number;
  isCollapsed: boolean;
  onToggle: (stage: LifecycleStage) => void;
  tasks: Task[];
  scheduledJobs: SchedulerJob[];
  activeTaskId: string | null;
}

function StageGroup({
  stage,
  count,
  isCollapsed,
  onToggle,
  tasks,
  scheduledJobs,
  activeTaskId,
}: StageGroupProps) {
  return (
    <div className="task-nav-group" data-stage={stage}>
      <button
        type="button"
        className="task-nav-group-header"
        aria-expanded={!isCollapsed}
        onClick={() => onToggle(stage)}
        data-testid={`task-nav-group-${stage}`}
      >
        <span className="task-nav-caret" aria-hidden="true">
          {isCollapsed ? "▸" : "▾"}
        </span>
        <span className="task-nav-group-label">{STAGE_LABELS[stage]}</span>
        <span className="task-nav-group-count">{count}</span>
      </button>
      {isCollapsed ? null : (
        <ul className="task-nav-items">
          {stage === "scheduled"
            ? scheduledJobs.map((job) => (
                <li key={routineKey(job)}>
                  <button
                    type="button"
                    className="task-nav-item"
                    onClick={() => openRoutine(job.slug)}
                    data-testid="task-nav-scheduled-item"
                  >
                    <span className="task-nav-item-title">
                      {routineLabel(job) || "Untitled routine"}
                    </span>
                  </button>
                </li>
              ))
            : tasks.map((task) => (
                <li key={task.id}>
                  <button
                    type="button"
                    className={`task-nav-item${
                      task.id === activeTaskId ? " active" : ""
                    }`}
                    onClick={() => openTask(task.id)}
                    data-testid="task-nav-item"
                    aria-current={task.id === activeTaskId ? "page" : undefined}
                  >
                    <TaskStatusDot
                      lifecycleState={taskToLifecycleState(task)}
                    />
                    <span className="task-nav-item-title">
                      {formatTaskTitleForDisplay(task.title) || "Untitled"}
                    </span>
                  </button>
                </li>
              ))}
        </ul>
      )}
    </div>
  );
}

// ── Main component ─────────────────────────────────────────────────────

export function SidebarTaskNav() {
  const route = useCurrentRoute();
  const activeTaskId = route.kind === "task-detail" ? route.taskId : null;

  const tasksResult = useQuery({
    queryKey: ["issues", "list"],
    queryFn: () => getOfficeTasks({ includeDone: true }),
    staleTime: 5_000,
    refetchInterval: 10_000,
  });
  const schedulerResult = useQuery({
    queryKey: ["scheduler"],
    queryFn: () => getScheduler(),
    refetchInterval: 15_000,
  });

  const tasks = useMemo(
    () => (tasksResult.data?.tasks ?? []).filter(isTaskSpecTask),
    [tasksResult.data],
  );
  const scheduledJobs = useMemo<SchedulerJob[]>(
    () => (schedulerResult.data?.jobs ?? []).filter(isCadenceSchedulerJob),
    [schedulerResult.data],
  );

  const byStage = useMemo(() => {
    const buckets: Record<LifecycleStage, Task[]> = {
      scheduled: [],
      backlog: [],
      in_progress: [],
      blocked: [],
      needs_human: [],
      done: [],
      archive: [],
    };
    for (const task of tasks) {
      buckets[stageForState(taskToLifecycleState(task))].push(task);
    }
    return buckets;
  }, [tasks]);

  const [collapsed, setCollapsed] = useState<Set<LifecycleStage>>(
    () => new Set(STAGE_ORDER.filter((s) => !DEFAULT_OPEN.has(s))),
  );

  function toggle(stage: LifecycleStage) {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(stage)) {
        next.delete(stage);
      } else {
        next.add(stage);
      }
      return next;
    });
  }

  function countFor(stage: LifecycleStage): number {
    return stage === "scheduled" ? scheduledJobs.length : byStage[stage].length;
  }

  const totalTasks = tasks.length + scheduledJobs.length;

  return (
    <div className="task-nav" data-testid="sidebar-task-nav">
      <div className="task-nav-toolbar">
        <button
          type="button"
          className="task-nav-board-link"
          onClick={openBoard}
          data-testid="task-nav-board-link"
        >
          All tasks
        </button>
        <button
          type="button"
          className="task-nav-new-btn"
          onClick={openNewTask}
          title="Create a new task"
          data-testid="task-nav-new-btn"
        >
          + New
        </button>
      </div>

      {tasksResult.isPending ? (
        <div className="task-nav-hint">Loading tasks…</div>
      ) : totalTasks === 0 ? (
        <div className="task-nav-hint">
          No tasks yet. Start one with “+ New”.
        </div>
      ) : (
        STAGE_ORDER.map((stage) => {
          const count = countFor(stage);
          if (count === 0) return null;
          return (
            <StageGroup
              key={stage}
              stage={stage}
              count={count}
              isCollapsed={collapsed.has(stage)}
              onToggle={toggle}
              tasks={byStage[stage]}
              scheduledJobs={scheduledJobs}
              activeTaskId={activeTaskId}
            />
          );
        })
      )}
    </div>
  );
}
