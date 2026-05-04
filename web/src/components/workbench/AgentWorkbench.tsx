import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { getOfficeMembers } from "../../api/client";
import {
  getOfficeTasks,
  listAgentLogTasks,
  type Task,
  type TaskLogSummary,
  type TaskMemoryWorkflow,
  type TaskMemoryWorkflowArtifact,
  type TaskMemoryWorkflowCitation,
  type TaskMemoryWorkflowPartialError,
  type TaskMemoryWorkflowStepState,
} from "../../api/tasks";
import { formatRelativeTime } from "../../lib/format";
import { AgentTerminal } from "../agents/AgentTerminal";
import "./AgentWorkbench.css";

export interface AgentWorkbenchProps {
  agentSlug?: string | null;
  taskId?: string | null;
  onClose?: () => void;
}

type LoadState = "loading" | "error" | "empty" | "ready";

const WORKFLOW_STEPS = ["lookup", "capture", "promote"] as const;

export function AgentWorkbench({
  agentSlug = null,
  taskId = null,
  onClose,
}: AgentWorkbenchProps) {
  const model = useAgentWorkbenchModel(agentSlug, taskId);

  return (
    <section className="awb-shell" aria-label="Agent workbench">
      <WorkbenchHeader
        agentName={model.agent?.name}
        agentSlug={model.selectedAgentSlug}
        taskId={model.selectedTask?.id ?? null}
        agentStatus={model.agent?.status}
        loadState={model.loadState}
        onClose={onClose}
      />
      <WorkbenchBody model={model} onSelectTask={model.setActiveTaskId} />
    </section>
  );
}

interface WorkbenchModel {
  setActiveTaskId: (taskId: string | null) => void;
  selectedAgentSlug: string | null;
  selectedTask: Task | null;
  selectedRun?: TaskLogSummary;
  visibleTasks: Task[];
  relevantRuns: TaskLogSummary[];
  agent?: { name?: string; status?: string } | null;
  loadState: LoadState;
  errorMessage: string | null;
}

function useAgentWorkbenchModel(
  agentSlug: string | null,
  taskId: string | null,
): WorkbenchModel {
  const [activeTaskId, setActiveTaskId] = useState<string | null>(
    taskId ?? null,
  );
  const previousAgentSlug = useRef(agentSlug);

  const logsQuery = useQuery({
    queryKey: ["agent-workbench", "agent-log-tasks"],
    queryFn: () => listAgentLogTasks({ limit: 80 }),
    refetchInterval: 8_000,
  });
  const tasksQuery = useQuery({
    queryKey: ["agent-workbench", "office-tasks"],
    queryFn: () => getOfficeTasks({ includeDone: true }),
    refetchInterval: 10_000,
  });
  const membersQuery = useQuery({
    queryKey: ["agent-workbench", "office-members"],
    queryFn: getOfficeMembers,
    staleTime: 30_000,
  });

  const tasks = useMemo(() => tasksQuery.data?.tasks ?? [], [tasksQuery.data]);
  const runs = useMemo(() => logsQuery.data?.tasks ?? [], [logsQuery.data]);
  const members = membersQuery.data?.members ?? [];

  const selectedAgentSlug = useMemo(
    () => resolveAgentSlug(agentSlug, taskId, activeTaskId, tasks, runs),
    [agentSlug, taskId, activeTaskId, tasks, runs],
  );

  const relevantRuns = useMemo(
    () => filterRuns(runs, selectedAgentSlug, taskId ?? null),
    [runs, selectedAgentSlug, taskId],
  );

  const visibleTasks = useMemo(
    () => filterTasks(tasks, selectedAgentSlug, taskId),
    [tasks, selectedAgentSlug, taskId],
  );

  const selectedTask = useMemo(
    () => resolveTask(taskId, activeTaskId, visibleTasks, tasks, relevantRuns),
    [taskId, activeTaskId, visibleTasks, tasks, relevantRuns],
  );

  useEffect(() => {
    setActiveTaskId((current) => {
      const nextTaskId = taskId ?? null;
      if (previousAgentSlug.current === agentSlug && current === nextTaskId) {
        return current;
      }
      previousAgentSlug.current = agentSlug;
      return nextTaskId;
    });
  }, [agentSlug, taskId]);

  useEffect(() => {
    if (taskId || activeTaskId || visibleTasks.length === 0) return;
    const visibleTaskIds = new Set(visibleTasks.map((task) => task.id));
    const nextRunTaskId = relevantRuns.find((run) =>
      visibleTaskIds.has(run.taskId),
    )?.taskId;
    setActiveTaskId(nextRunTaskId ?? visibleTasks[0].id);
  }, [activeTaskId, relevantRuns, taskId, visibleTasks]);

  const agent = selectedAgentSlug
    ? members.find((member) => member.slug === selectedAgentSlug)
    : null;
  const requestedTaskId = taskId ?? activeTaskId;
  const selectedRun = requestedTaskId
    ? relevantRuns.find((run) => run.taskId === requestedTaskId)
    : ((selectedTask
        ? relevantRuns.find((run) => run.taskId === selectedTask.id)
        : undefined) ?? relevantRuns[0]);
  const loadState = getLoadState(
    logsQuery.isLoading || tasksQuery.isLoading,
    logsQuery.isError || tasksQuery.isError,
    selectedAgentSlug,
    visibleTasks,
    relevantRuns,
  );
  const errorMessage =
    errorText(logsQuery.error) || errorText(tasksQuery.error);

  return {
    setActiveTaskId,
    selectedAgentSlug,
    selectedTask,
    selectedRun,
    visibleTasks,
    relevantRuns,
    agent,
    loadState,
    errorMessage,
  };
}

function WorkbenchHeader({
  agentName,
  agentSlug,
  taskId,
  agentStatus,
  loadState,
  onClose,
}: {
  agentName?: string;
  agentSlug: string | null;
  taskId: string | null;
  agentStatus?: string;
  loadState: LoadState;
  onClose?: () => void;
}) {
  return (
    <header className="awb-header">
      <div>
        <p className="awb-kicker">Agent workbench</p>
        <h2>{agentName || agentSlug || "Unassigned agent"}</h2>
        <div className="awb-header-meta">
          {agentSlug ? <span>@{agentSlug}</span> : null}
          {taskId ? <span>#{taskId}</span> : null}
          {agentStatus ? <span>{agentStatus}</span> : null}
        </div>
      </div>
      <div className="awb-header-actions">
        <StatusPill state={loadState} />
        {onClose ? (
          <button
            type="button"
            className="awb-close"
            onClick={onClose}
            aria-label="Close workbench"
          >
            Close
          </button>
        ) : null}
      </div>
    </header>
  );
}

function WorkbenchBody({
  model,
  onSelectTask,
}: {
  model: WorkbenchModel;
  onSelectTask: (taskId: string | null) => void;
}) {
  if (model.loadState === "loading") {
    return (
      <WorkbenchState
        title="Loading workbench"
        body="Gathering runs, tasks, and context."
      />
    );
  }
  if (model.loadState === "error") {
    return (
      <WorkbenchState
        title="Could not load workbench"
        body={model.errorMessage || "The broker did not return workbench data."}
      />
    );
  }
  if (model.loadState === "empty") {
    return (
      <WorkbenchState
        title="No workbench data"
        body="Pick an agent or task with recent activity to populate this view."
      />
    );
  }
  return (
    <div className="awb-grid">
      <main className="awb-main">
        <ContextPanel
          agentSlug={model.selectedAgentSlug}
          agentName={model.agent?.name}
          task={model.selectedTask}
          run={model.selectedRun}
        />
        <EvidencePanel task={model.selectedTask} />
        <div className="awb-terminal-wrap">
          <AgentTerminal
            slug={model.selectedAgentSlug}
            title="Live terminal"
            emptyLabel="No live output for this agent yet"
          />
        </div>
      </main>
      <aside className="awb-side">
        <RunList
          runs={model.relevantRuns}
          selectedTaskId={model.selectedTask?.id ?? null}
          onSelectTask={onSelectTask}
        />
        <TaskList
          tasks={model.visibleTasks}
          selectedTaskId={model.selectedTask?.id ?? null}
          onSelectTask={onSelectTask}
        />
      </aside>
    </div>
  );
}

function StatusPill({ state }: { state: LoadState }) {
  const label =
    state === "ready"
      ? "ready"
      : state === "loading"
        ? "loading"
        : state === "error"
          ? "needs attention"
          : "empty";
  return <span className={`awb-status awb-status-${state}`}>{label}</span>;
}

function WorkbenchState({ title, body }: { title: string; body: string }) {
  return (
    <div className="awb-state">
      <div className="awb-state-mark" />
      <h3>{title}</h3>
      <p>{body}</p>
    </div>
  );
}

function ContextPanel({
  agentSlug,
  agentName,
  task,
  run,
}: {
  agentSlug: string | null;
  agentName?: string;
  task?: Task | null;
  run?: TaskLogSummary;
}) {
  const rows: Array<[string, string | null | undefined]> = [
    ["Agent", agentName || agentSlug],
    ["Task", task?.title],
    ["Owner", task?.owner ? `@${task.owner}` : null],
    ["Status", task?.status ? displayStatus(task.status) : null],
    ["Channel", task?.channel ? `#${task.channel}` : null],
    ["Updated", task?.updated_at ? formatRelativeTime(task.updated_at) : null],
    [
      "Latest run",
      run?.lastToolAt ? formatEpochTime(run.lastToolAt) : run?.taskId,
    ],
  ];

  return (
    <section className="awb-panel">
      <div className="awb-panel-heading">
        <h3>Context</h3>
        {task?.memory_workflow ? (
          <span className={memoryBadgeClass(task.memory_workflow)}>
            {memoryBadgeLabel(task.memory_workflow)}
          </span>
        ) : null}
      </div>
      {task?.description || task?.details ? (
        <p className="awb-summary">{task.description || task.details}</p>
      ) : null}
      <dl className="awb-meta">
        {rows
          .filter(([, value]) => value)
          .map(([label, value]) => (
            <div key={label}>
              <dt>{label}</dt>
              <dd>{value}</dd>
            </div>
          ))}
      </dl>
    </section>
  );
}

function EvidencePanel({ task }: { task?: Task | null }) {
  const workflow = task?.memory_workflow;
  const citations = workflow?.citations ?? [];
  const captures = workflow?.captures ?? [];
  const promotions = workflow?.promotions ?? [];
  const errors = workflow?.partial_errors ?? [];
  const hasEvidence =
    citations.length > 0 ||
    captures.length > 0 ||
    promotions.length > 0 ||
    errors.length > 0 ||
    hasWorkflowSteps(workflow);

  return (
    <section className="awb-panel">
      <div className="awb-panel-heading">
        <h3>Evidence and artifacts</h3>
        {workflow?.updated_at ? (
          <span className="awb-muted">
            updated {formatRelativeTime(workflow.updated_at)}
          </span>
        ) : null}
      </div>
      {!hasEvidence ? (
        <div className="awb-mini-empty">
          No linked evidence or artifacts for this task yet.
        </div>
      ) : (
        <div className="awb-evidence-grid">
          <WorkflowSteps workflow={workflow} />
          <EvidenceGroup
            title="Citations"
            items={citations.map(formatCitation)}
          />
          <EvidenceGroup
            title="Captures"
            items={captures.map(formatArtifact)}
          />
          <EvidenceGroup
            title="Promotions"
            items={promotions.map(formatArtifact)}
          />
          <EvidenceGroup
            title="Partial errors"
            items={errors.map(formatError)}
          />
        </div>
      )}
    </section>
  );
}

function WorkflowSteps({ workflow }: { workflow?: TaskMemoryWorkflow }) {
  if (!hasWorkflowSteps(workflow)) return null;
  return (
    <div className="awb-evidence-group awb-evidence-group-wide">
      <h4>Workflow</h4>
      <div className="awb-step-row">
        {WORKFLOW_STEPS.map((step) => {
          const state = workflow?.[step];
          const satisfied = isStepSatisfied(state);
          return (
            <span
              key={step}
              className={`awb-step ${satisfied ? "awb-step-done" : ""}`}
            >
              {step}
              {state?.count ? ` ${state.count}` : ""}
            </span>
          );
        })}
      </div>
      {workflow?.requirement_reason ? (
        <p>{workflow.requirement_reason}</p>
      ) : null}
    </div>
  );
}

function EvidenceGroup({ title, items }: { title: string; items: string[] }) {
  if (items.length === 0) return null;
  return (
    <div className="awb-evidence-group">
      <h4>{title}</h4>
      <ul>
        {items.slice(0, 4).map((item) => (
          <li key={`${title}-${item}`}>{item}</li>
        ))}
      </ul>
    </div>
  );
}

function RunList({
  runs,
  selectedTaskId,
  onSelectTask,
}: {
  runs: TaskLogSummary[];
  selectedTaskId: string | null;
  onSelectTask: (taskId: string) => void;
}) {
  return (
    <section className="awb-panel awb-side-panel">
      <div className="awb-panel-heading">
        <h3>Recent runs</h3>
        <span className="awb-count">{runs.length}</span>
      </div>
      {runs.length === 0 ? (
        <div className="awb-mini-empty">No recent runs match this view.</div>
      ) : (
        <div className="awb-run-list">
          {runs.slice(0, 12).map((run) => (
            <button
              type="button"
              key={`${run.agentSlug}-${run.taskId}`}
              className={`awb-run ${selectedTaskId === run.taskId ? "active" : ""}`}
              onClick={() => onSelectTask(run.taskId)}
            >
              <span>
                <strong>{run.taskId}</strong>
                <small>@{run.agentSlug}</small>
              </span>
              <span className="awb-run-meta">
                {run.toolCallCount} tools
                {run.hasError ? " · error" : ""}
              </span>
            </button>
          ))}
        </div>
      )}
    </section>
  );
}

function TaskList({
  tasks,
  selectedTaskId,
  onSelectTask,
}: {
  tasks: Task[];
  selectedTaskId: string | null;
  onSelectTask: (taskId: string) => void;
}) {
  return (
    <section className="awb-panel awb-side-panel">
      <div className="awb-panel-heading">
        <h3>Tasks</h3>
        <span className="awb-count">{tasks.length}</span>
      </div>
      {tasks.length === 0 ? (
        <div className="awb-mini-empty">No tasks match this workbench.</div>
      ) : (
        <div className="awb-task-list">
          {tasks.slice(0, 12).map((task) => (
            <button
              type="button"
              key={task.id}
              className={`awb-task ${selectedTaskId === task.id ? "active" : ""}`}
              onClick={() => onSelectTask(task.id)}
            >
              <span>{task.title || task.id}</span>
              <small>
                {displayStatus(task.status)}
                {task.owner ? ` · @${task.owner}` : ""}
              </small>
            </button>
          ))}
        </div>
      )}
    </section>
  );
}

function resolveAgentSlug(
  agentSlug: string | null | undefined,
  taskId: string | null | undefined,
  activeTaskId: string | null,
  tasks: Task[],
  runs: TaskLogSummary[],
): string | null {
  if (agentSlug) return agentSlug;
  const selectedTaskId = taskId || activeTaskId;
  if (selectedTaskId) {
    const run = runs.find((candidate) => candidate.taskId === selectedTaskId);
    if (run?.agentSlug) return run.agentSlug;
    const task = tasks.find((candidate) => candidate.id === selectedTaskId);
    if (task?.owner) return task.owner;
    return null;
  }
  return runs[0]?.agentSlug || tasks.find((task) => task.owner)?.owner || null;
}

function filterRuns(
  runs: TaskLogSummary[],
  agentSlug: string | null,
  taskId: string | null,
): TaskLogSummary[] {
  return runs
    .filter((run) => {
      if (agentSlug && run.agentSlug !== agentSlug) return false;
      if (taskId && run.taskId !== taskId) return false;
      return true;
    })
    .sort((a, b) => (b.lastToolAt ?? 0) - (a.lastToolAt ?? 0));
}

function filterTasks(
  tasks: Task[],
  agentSlug: string | null,
  taskId: string | null | undefined,
): Task[] {
  return tasks.filter((task) => {
    if (taskId && task.id !== taskId) return false;
    if (agentSlug && !taskId && task.owner !== agentSlug) return false;
    if (agentSlug && task.owner && task.owner !== agentSlug) return false;
    return true;
  });
}

function resolveTask(
  propTaskId: string | null | undefined,
  activeTaskId: string | null,
  visibleTasks: Task[],
  allTasks: Task[],
  runs: TaskLogSummary[],
): Task | null {
  const selectedTaskId = propTaskId || activeTaskId || runs[0]?.taskId || null;
  if (!selectedTaskId) return visibleTasks[0] ?? null;
  return (
    visibleTasks.find((task) => task.id === selectedTaskId) ||
    allTasks.find((task) => task.id === selectedTaskId) ||
    null
  );
}

function getLoadState(
  loading: boolean,
  error: boolean,
  agentSlug: string | null,
  tasks: Task[],
  runs: TaskLogSummary[],
): LoadState {
  if (loading) return "loading";
  if (error) return "error";
  if (!agentSlug && tasks.length === 0 && runs.length === 0) return "empty";
  return "ready";
}

function errorText(error: unknown): string | null {
  return error instanceof Error ? error.message : null;
}

function displayStatus(status: string): string {
  return status.replace(/[_-]+/g, " ");
}

function formatEpochTime(value: number): string {
  return new Date(value).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
  });
}

function hasWorkflowSteps(
  workflow?: TaskMemoryWorkflow | null,
): workflow is TaskMemoryWorkflow {
  return Boolean(
    workflow &&
      (workflow.required ||
        workflow.status ||
        WORKFLOW_STEPS.some((step) => workflow[step])),
  );
}

function isStepSatisfied(step?: TaskMemoryWorkflowStepState): boolean {
  const status = normalizeStatus(step?.status);
  return Boolean(
    step?.completed_at ||
      status === "satisfied" ||
      status === "complete" ||
      status === "completed",
  );
}

function memoryBadgeClass(workflow: TaskMemoryWorkflow): string {
  const status = normalizeStatus(workflow.status);
  if (workflow.partial_errors?.length || status.includes("error")) {
    return "awb-memory awb-memory-warn";
  }
  if (workflow.override || status === "overridden") {
    return "awb-memory awb-memory-warn";
  }
  if (status === "satisfied" || status === "complete" || status === "done") {
    return "awb-memory awb-memory-done";
  }
  return "awb-memory";
}

function memoryBadgeLabel(workflow: TaskMemoryWorkflow): string {
  const requiredSteps = WORKFLOW_STEPS.filter(
    (step) => workflow.required_steps?.includes(step) || workflow[step],
  );
  const total = requiredSteps.length;
  const done = requiredSteps.filter((step) =>
    isStepSatisfied(workflow[step]),
  ).length;
  if (workflow.override) return "memory override";
  if (workflow.partial_errors?.length) return "memory issue";
  if (total > 0) return `memory ${done}/${total}`;
  return workflow.status
    ? `memory ${displayStatus(workflow.status)}`
    : "memory";
}

function normalizeStatus(status?: string): string {
  return (status || "")
    .trim()
    .toLowerCase()
    .replace(/[\s-]+/g, "_");
}

function formatCitation(citation: TaskMemoryWorkflowCitation): string {
  const title =
    citation.title ||
    citation.path ||
    citation.source_url ||
    citation.source_id ||
    "citation";
  return [
    title,
    citation.path && citation.path !== title ? citation.path : null,
    citation.source,
    citation.stale ? "stale" : null,
  ]
    .filter(Boolean)
    .join(" · ");
}

function formatArtifact(artifact: TaskMemoryWorkflowArtifact): string {
  const title =
    artifact.title ||
    artifact.path ||
    artifact.page_id ||
    artifact.promotion_id ||
    "artifact";
  return [
    artifact.source,
    title,
    artifact.path && artifact.path !== title ? artifact.path : null,
    artifact.state,
    artifact.missing ? "missing" : null,
  ]
    .filter(Boolean)
    .join(" · ");
}

function formatError(error: string | TaskMemoryWorkflowPartialError): string {
  if (typeof error === "string") return error;
  return (
    [error.step, error.code, error.message || error.detail]
      .filter(Boolean)
      .join(" · ") || "workflow error"
  );
}
