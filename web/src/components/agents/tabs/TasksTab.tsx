/**
 * Tasks tab — owner-filtered board showing tasks assigned to this agent.
 * Groups tasks by lifecycle stage using the same stage grouping as TasksList.
 */

import { memo } from "react";

import type { Task } from "../../../api/tasks";
import { taskToLifecycleState } from "../../../api/tasks";
import { useOfficeTasks } from "../../../hooks/useOfficeTasks";
import { router } from "../../../lib/router";
import { formatTaskTitleForDisplay } from "../../../lib/taskTitle";
import {
  type LifecycleStage,
  STAGE_LABELS,
  STAGE_ORDER,
  stageForState,
} from "../../../lib/types/lifecycle";
import { LifecycleStatePill } from "../../lifecycle/LifecycleStatePill";

interface TasksTabProps {
  agentSlug: string;
}

const AgentTaskCard = memo(function TaskCard({ task }: { task: Task }) {
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
      data-testid="agent-task-card"
      aria-label={`Task: ${formatTaskTitleForDisplay(task.title)}, state: ${state}`}
    >
      <div className="issues-kanban-card-title">
        {formatTaskTitleForDisplay(task.title) || "Untitled"}
      </div>
      <div className="issues-kanban-card-meta">
        <LifecycleStatePill state={state} />
      </div>
    </button>
  );
});

interface StageColumn {
  stage: LifecycleStage;
  tasks: Task[];
}

export function TasksTab({ agentSlug }: TasksTabProps) {
  const { data: allTasks = [], isLoading } = useOfficeTasks();

  const agentTasks = allTasks.filter((t) => t.owner === agentSlug);

  const columns: StageColumn[] = STAGE_ORDER.filter(
    (stage) => stage !== "scheduled",
  ).map((stage) => ({
    stage,
    tasks: agentTasks.filter(
      (t) => stageForState(taskToLifecycleState(t)) === stage,
    ),
  }));

  const nonEmptyColumns = columns.filter((col) => col.tasks.length > 0);

  if (isLoading) {
    return (
      <div className="agent-tasks-tab agent-tasks-tab--loading">
        <p className="agent-tasks-empty">Loading tasks…</p>
      </div>
    );
  }

  if (agentTasks.length === 0) {
    return (
      <div className="agent-tasks-tab">
        <p className="agent-tasks-empty">No tasks owned by @{agentSlug} yet.</p>
      </div>
    );
  }

  return (
    <div className="agent-tasks-tab">
      <div className="agent-tasks-board">
        {nonEmptyColumns.map(({ stage, tasks }) => (
          <section
            key={stage}
            className="agent-tasks-column"
            aria-label={`${STAGE_LABELS[stage]} tasks`}
          >
            <header className="agent-tasks-column-header">
              <span className="agent-tasks-column-label">
                {STAGE_LABELS[stage]}
              </span>
              <span className="agent-tasks-column-count">{tasks.length}</span>
            </header>
            <div className="agent-tasks-column-body">
              {tasks.map((task) => (
                <AgentTaskCard key={task.id} task={task} />
              ))}
            </div>
          </section>
        ))}
      </div>
    </div>
  );
}
