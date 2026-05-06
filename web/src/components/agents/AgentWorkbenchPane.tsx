import { useEffect, useMemo, useRef } from "react";
import { useQuery } from "@tanstack/react-query";

import {
  getOfficeTasks,
  listAgentLogTasks,
  type Task,
  type TaskLogSummary,
} from "../../api/tasks";
import { useAgentStream } from "../../hooks/useAgentStream";
import { formatRelativeTime } from "../../lib/format";
import { router } from "../../lib/router";
import { StreamLineView } from "../messages/StreamLineView";
import { CollapsibleSection } from "../ui/CollapsibleSection";

interface AgentWorkbenchPaneProps {
  agentSlug: string;
}

const ACTIVE_STATUSES = new Set(["in_progress", "open", "review", "blocked"]);

function isActive(task: Task): boolean {
  const status = (task.status || "").trim().toLowerCase();
  return ACTIVE_STATUSES.has(status);
}

function compareByRecency(a: Task, b: Task): number {
  const aTime = Date.parse(a.updated_at ?? a.created_at ?? "") || 0;
  const bTime = Date.parse(b.updated_at ?? b.created_at ?? "") || 0;
  return bTime - aTime;
}

function formatRunTime(ms?: number): string {
  if (!ms) return "";
  try {
    return formatRelativeTime(new Date(ms).toISOString());
  } catch {
    return "";
  }
}

function openTaskDetail(taskId: string): void {
  void router.navigate({ to: "/tasks/$taskId", params: { taskId } });
}

export function AgentWorkbenchPane({ agentSlug }: AgentWorkbenchPaneProps) {
  const { data: tasksData } = useQuery({
    queryKey: ["office-tasks"],
    queryFn: () => getOfficeTasks({ includeDone: true }),
    refetchInterval: 10_000,
  });
  // Office-wide run log is not server-side filterable by agent, so we fetch a
  // wide window and trim client-side. 80 was small enough that an active
  // neighbour could starve this agent's recent runs out of the response;
  // 240 gives ~10 visible per agent across a 24-agent office before truncation.
  const { data: runsData } = useQuery({
    queryKey: ["agent-workbench-runs"],
    queryFn: () => listAgentLogTasks({ limit: 240 }),
    refetchInterval: 8_000,
  });

  const { activeTasks, recentTasks } = useMemo(() => {
    const allTasks = tasksData?.tasks ?? [];
    const owned = allTasks.filter((task) => task.owner === agentSlug);
    const active: Task[] = [];
    const recent: Task[] = [];
    for (const task of owned) {
      if (isActive(task)) active.push(task);
      else recent.push(task);
    }
    active.sort(compareByRecency);
    recent.sort(compareByRecency);
    return { activeTasks: active, recentTasks: recent.slice(0, 8) };
  }, [tasksData, agentSlug]);

  const recentRuns = useMemo(() => {
    const runs = runsData?.tasks ?? [];
    return runs.filter((run) => run.agentSlug === agentSlug).slice(0, 10);
  }, [runsData, agentSlug]);

  return (
    <div className="agent-workbench-pane" data-testid="agent-workbench-pane">
      <CollapsibleSection
        id="active-tasks"
        title="Active tasks"
        meta={<span className="collapsible-count">{activeTasks.length}</span>}
      >
        {activeTasks.length === 0 ? (
          <div className="agent-workbench-empty">
            No active tasks for @{agentSlug}.
          </div>
        ) : (
          <ul className="agent-workbench-task-list">
            {activeTasks.map((task) => (
              <TaskRow
                key={task.id}
                task={task}
                onOpen={() => openTaskDetail(task.id)}
              />
            ))}
          </ul>
        )}
      </CollapsibleSection>

      {/* keepMounted: collapsing this section must not tear down the
          EventSource owned by useAgentStream — the live subscription and
          accumulated lines should survive a hide/show cycle so reopening
          doesn't drop output the user briefly tucked away. */}
      <CollapsibleSection
        id="live-stream"
        title="Live stream"
        meta={<span className="collapsible-meta-muted">@{agentSlug}</span>}
        keepMounted={true}
      >
        <AgentStreamSection slug={agentSlug} />
      </CollapsibleSection>

      <CollapsibleSection
        id="recent-activity"
        title="Recent activity"
        defaultOpen={false}
        meta={<span className="collapsible-count">{recentRuns.length}</span>}
      >
        {recentRuns.length === 0 ? (
          <div className="agent-workbench-empty">
            No tool calls recorded for @{agentSlug}.
          </div>
        ) : (
          <ul className="agent-workbench-run-list">
            {recentRuns.map((run) => (
              <RunRow
                key={run.taskId}
                run={run}
                onOpen={() => openTaskDetail(run.taskId)}
              />
            ))}
          </ul>
        )}
      </CollapsibleSection>

      <CollapsibleSection
        id="recent-tasks"
        title="Recent tasks"
        defaultOpen={false}
        meta={<span className="collapsible-count">{recentTasks.length}</span>}
      >
        {recentTasks.length === 0 ? (
          <div className="agent-workbench-empty">
            No completed or canceled tasks for @{agentSlug}.
          </div>
        ) : (
          <ul className="agent-workbench-task-list">
            {recentTasks.map((task) => (
              <TaskRow
                key={task.id}
                task={task}
                onOpen={() => openTaskDetail(task.id)}
              />
            ))}
          </ul>
        )}
      </CollapsibleSection>
    </div>
  );
}

interface AgentStreamSectionProps {
  slug: string;
}

function AgentStreamSection({ slug }: AgentStreamSectionProps) {
  const { lines, connected } = useAgentStream(slug);
  const scrollRef = useRef<HTMLDivElement>(null);
  // appendStreamLine merges consecutive raw chunks into the last line's
  // `data` without growing the array, so depending on length alone would
  // freeze the scroll while a model is still streaming text. Track the
  // last line's id+data so coalesced updates retrigger the effect too.
  const lastLine = lines[lines.length - 1];

  // Stick to bottom only when the user is already near it, so scrolling
  // back through history isn't disrupted by every new line.
  // biome-ignore lint/correctness/useExhaustiveDependencies: re-run on every new line so the log auto-scrolls.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    if (distanceFromBottom < 32) {
      el.scrollTop = el.scrollHeight;
    }
  }, [lines.length, lastLine?.id, lastLine?.data]);

  return (
    <div className="agent-workbench-stream">
      <div className="agent-stream-status" role="status" aria-live="polite">
        <span
          className={`status-dot ${connected ? "active pulse" : "lurking"}`}
          aria-hidden="true"
        />
        {connected ? "Connected" : "Disconnected"}
      </div>
      <div
        className="agent-stream-log agent-workbench-stream-log"
        ref={scrollRef}
      >
        {lines.length === 0 ? (
          <div className="agent-stream-empty">
            {connected ? "Waiting for output..." : "Stream idle"}
          </div>
        ) : (
          lines.map((line) => (
            <StreamLineView key={line.id} line={line} compact={true} />
          ))
        )}
      </div>
    </div>
  );
}

interface TaskRowProps {
  task: Task;
  onOpen: () => void;
}

function TaskRow({ task, onOpen }: TaskRowProps) {
  const status = (task.status || "open").trim().toLowerCase();
  const updated = task.updated_at ?? task.created_at;
  return (
    <li>
      <button
        type="button"
        className="agent-workbench-task-row"
        onClick={onOpen}
      >
        <span className="agent-workbench-task-title">
          {task.title || "Untitled"}
        </span>
        <span className="agent-workbench-task-meta">
          <span className={`badge status-${status}`}>
            {status.replace(/_/g, " ")}
          </span>
          {task.channel ? <span>#{task.channel}</span> : null}
          {updated ? <span>{formatRelativeTime(updated)}</span> : null}
        </span>
      </button>
    </li>
  );
}

interface RunRowProps {
  run: TaskLogSummary;
  onOpen: () => void;
}

function RunRow({ run, onOpen }: RunRowProps) {
  return (
    <li>
      <button
        type="button"
        className="agent-workbench-run-row"
        onClick={onOpen}
      >
        <span className="agent-workbench-run-id">
          {run.taskId}
          {run.hasError ? " ⚠" : ""}
        </span>
        <span className="agent-workbench-run-meta">
          <span>
            {run.toolCallCount} tool call{run.toolCallCount === 1 ? "" : "s"}
          </span>
          {run.lastToolAt ? <span>{formatRunTime(run.lastToolAt)}</span> : null}
        </span>
      </button>
    </li>
  );
}
