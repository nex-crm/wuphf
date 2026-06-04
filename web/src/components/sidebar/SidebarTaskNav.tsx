/**
 * SidebarTaskNav — the primary sidebar surface in the task-scoped model.
 *
 * One "Tasks" section: a single flat list of the operator's ACTIVE tasks
 * (Backlog / In progress / Needs input / Blocked), most-actionable first.
 * Done, Archive, and Scheduled deliberately do NOT get their own sidebar
 * sections — they live on the full board, reached by clicking the "Tasks"
 * header. Each task links to its detail surface (/tasks/$taskId, which
 * carries its channel).
 *
 * This replaces the earlier stage-grouped nav (separate All tasks /
 * Scheduled / Done sections) per the operator's call: "we just need our
 * tasks section."
 */

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";

import { getOfficeTasks, type Task } from "../../api/tasks";
import { router } from "../../lib/router";
import { formatTaskTitleForDisplay } from "../../lib/taskTitle";
import {
  type LifecycleStage,
  STAGE_ORDER,
  stageForState,
} from "../../lib/types/lifecycle";
import { useCurrentRoute } from "../../routes/useCurrentRoute";
import { TaskStatusDot } from "../lifecycle/TaskActivityStream";
import { isTaskSpecTask, taskToLifecycleState } from "../lifecycle/TasksList";

// Stages that count as live, actionable work shown in the sidebar. Done,
// Archive, and Scheduled live on the board (reached via the Tasks header).
const ACTIVE_STAGES: ReadonlySet<LifecycleStage> = new Set<LifecycleStage>([
  "in_progress",
  "needs_human",
  "blocked",
  "backlog",
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

export function SidebarTaskNav() {
  const route = useCurrentRoute();
  const activeTaskId = route.kind === "task-detail" ? route.taskId : null;

  const tasksResult = useQuery({
    queryKey: ["issues", "list"],
    queryFn: () => getOfficeTasks({ includeDone: true }),
    staleTime: 5_000,
    refetchInterval: 10_000,
  });

  // Active tasks only, ordered by stage (in progress → needs input →
  // blocked → backlog) so the most actionable sit at the top. Done /
  // archive / scheduled are filtered out — they belong on the board.
  const activeTasks = useMemo(() => {
    const stageRank = (task: Task): number =>
      STAGE_ORDER.indexOf(stageForState(taskToLifecycleState(task)));
    return (tasksResult.data?.tasks ?? [])
      .filter(isTaskSpecTask)
      .filter((task) =>
        ACTIVE_STAGES.has(stageForState(taskToLifecycleState(task))),
      )
      .sort((a, b) => stageRank(a) - stageRank(b));
  }, [tasksResult.data]);

  return (
    <div className="task-nav" data-testid="sidebar-task-nav">
      <div className="task-nav-toolbar">
        <button
          type="button"
          className="task-nav-board-link"
          onClick={openBoard}
          title="Open the full task board (incl. done, archive, scheduled)"
          data-testid="task-nav-board-link"
        >
          Tasks
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
      ) : activeTasks.length === 0 ? (
        <div className="task-nav-hint">
          No tasks yet. Start one with “+ New”.
        </div>
      ) : (
        <ul className="task-nav-items" data-testid="task-nav-list">
          {activeTasks.map((task) => (
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
                <TaskStatusDot lifecycleState={taskToLifecycleState(task)} />
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
