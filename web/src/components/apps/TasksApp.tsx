import { type DragEvent, useCallback, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { getOfficeTasks, post, type Task } from "../../api/client";
import { formatRelativeTime } from "../../lib/format";
import { showNotice } from "../ui/Toast";
import { TaskDetailModal } from "./TaskDetailModal";

const STATUS_ORDER = [
  "in_progress",
  "open",
  "review",
  "pending",
  "blocked",
  "done",
  "canceled",
] as const;

type StatusGroup = (typeof STATUS_ORDER)[number];

const DND_MIME = "application/x-wuphf-task-id";
const HUMAN_SLUG = "human";

const COLUMN_LABEL: Record<StatusGroup, string> = {
  in_progress: "in progress",
  open: "open",
  review: "review",
  pending: "pending",
  blocked: "blocked",
  done: "done",
  canceled: "won't do",
};

function normalizeStatus(raw: string): StatusGroup {
  const s = raw.toLowerCase().replace(/[\s-]+/g, "_");
  if (s === "completed") return "done";
  if (s === "in_review") return "review";
  if (s === "cancelled") return "canceled";
  if ((STATUS_ORDER as readonly string[]).includes(s)) return s as StatusGroup;
  return "open";
}

function statusBadgeClass(status: StatusGroup): string {
  if (status === "done") return "badge badge-green";
  if (status === "in_progress" || status === "review")
    return "badge badge-accent";
  if (status === "blocked") return "badge badge-yellow";
  if (status === "canceled") return "badge badge-muted";
  return "badge badge-accent";
}

function groupTasks(tasks: Task[]): Record<StatusGroup, Task[]> {
  const groups: Record<StatusGroup, Task[]> = {
    in_progress: [],
    open: [],
    review: [],
    pending: [],
    blocked: [],
    done: [],
    canceled: [],
  };
  for (const task of tasks) {
    const status = normalizeStatus(task.status);
    groups[status].push(task);
  }
  return groups;
}

/**
 * Map a target column (StatusGroup) to the backend action payload.
 * Returns null when the transition has no corresponding action (e.g. "pending").
 */
function buildMoveBody(
  task: Task,
  toStatus: StatusGroup,
): Record<string, string> | null {
  const base: Record<string, string> = {
    id: task.id,
    channel: task.channel || "general",
    created_by: HUMAN_SLUG,
  };
  switch (toStatus) {
    case "in_progress":
      return { ...base, action: "claim", owner: HUMAN_SLUG };
    case "open":
      return { ...base, action: "release" };
    case "review":
      return { ...base, action: "review" };
    case "done":
      return { ...base, action: "complete" };
    case "blocked":
      return { ...base, action: "block" };
    case "canceled":
      return { ...base, action: "cancel" };
    case "pending":
      // No direct "pending" action in the broker — punted.
      return null;
  }
}

function useTaskMove() {
  const queryClient = useQueryClient();

  return useCallback(
    async (task: Task, toStatus: StatusGroup) => {
      const fromStatus = normalizeStatus(task.status);
      if (fromStatus === toStatus) return;

      const body = buildMoveBody(task, toStatus);
      if (!body) return;

      try {
        await post("/tasks", body);
      } catch (err: unknown) {
        const message = err instanceof Error ? err.message : "Move failed";
        showNotice(message, "error");
      } finally {
        await queryClient.invalidateQueries({ queryKey: ["office-tasks"] });
      }
    },
    [queryClient],
  );
}

export function TasksApp() {
  const { data, isLoading, error } = useQuery({
    queryKey: ["office-tasks"],
    queryFn: () => getOfficeTasks({ includeDone: true }),
    refetchInterval: 10_000,
  });

  const moveTask = useTaskMove();
  const [draggingId, setDraggingId] = useState<string | null>(null);
  const [dragoverStatus, setDragoverStatus] = useState<StatusGroup | null>(
    null,
  );
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);

  if (isLoading) {
    return (
      <div
        style={{
          padding: "40px 20px",
          textAlign: "center",
          color: "var(--text-tertiary)",
          fontSize: 14,
        }}
      >
        Loading tasks...
      </div>
    );
  }

  if (error) {
    return (
      <div
        style={{
          padding: "40px 20px",
          textAlign: "center",
          color: "var(--text-tertiary)",
          fontSize: 14,
        }}
      >
        Could not load tasks.
      </div>
    );
  }

  const tasks = data?.tasks ?? [];

  if (tasks.length === 0) {
    return (
      <div
        style={{
          padding: "40px 20px",
          textAlign: "center",
          color: "var(--text-tertiary)",
          fontSize: 14,
        }}
      >
        No tasks yet.
      </div>
    );
  }

  const grouped = groupTasks(tasks);
  const tasksById = new Map(tasks.map((t) => [t.id, t]));
  const isDragging = draggingId !== null;
  const selectedTask = selectedTaskId
    ? (tasksById.get(selectedTaskId) ?? null)
    : null;

  const handleDragStart =
    (taskId: string) => (event: DragEvent<HTMLDivElement>) => {
      event.dataTransfer.effectAllowed = "move";
      event.dataTransfer.setData(DND_MIME, taskId);
      // Fallback for browsers that restrict custom MIME reads during dragover.
      event.dataTransfer.setData("text/plain", taskId);
      setDraggingId(taskId);
    };

  const handleDragEnd = () => {
    setDraggingId(null);
    setDragoverStatus(null);
  };

  const handleColumnDragOver =
    (status: StatusGroup) => (event: DragEvent<HTMLDivElement>) => {
      event.preventDefault();
      event.dataTransfer.dropEffect = "move";
      if (dragoverStatus !== status) setDragoverStatus(status);
    };

  const handleColumnDragLeave =
    (status: StatusGroup) => (event: DragEvent<HTMLDivElement>) => {
      // Only clear when leaving the column itself, not a nested child.
      if (event.currentTarget.contains(event.relatedTarget as Node | null))
        return;
      if (dragoverStatus === status) setDragoverStatus(null);
    };

  const handleColumnDrop =
    (status: StatusGroup) => (event: DragEvent<HTMLDivElement>) => {
      event.preventDefault();
      const taskId =
        event.dataTransfer.getData(DND_MIME) ||
        event.dataTransfer.getData("text/plain");
      setDraggingId(null);
      setDragoverStatus(null);
      if (!taskId) return;
      const task = tasksById.get(taskId);
      if (!task) return;
      void moveTask(task, status);
    };

  return (
    <>
      <div
        style={{
          padding: "16px 20px 0",
          borderBottom: "1px solid var(--border)",
        }}
      >
        <h3 style={{ fontSize: 16, fontWeight: 600 }}>Office tasks</h3>
        <div
          style={{
            fontSize: 12,
            color: "var(--text-tertiary)",
            marginTop: 4,
            marginBottom: 12,
          }}
        >
          All active lanes across the office. Drag a card to move it.
        </div>
      </div>

      <div className="task-board">
        {STATUS_ORDER.map((status) => {
          const column = grouped[status];
          // Hide empty pending/blocked/canceled columns only when nothing is being dragged.
          // While dragging, keep all columns visible as drop targets.
          if (
            !isDragging &&
            column.length === 0 &&
            (status === "pending" ||
              status === "blocked" ||
              status === "canceled")
          ) {
            return null;
          }
          const columnClass = `task-column${dragoverStatus === status ? " dragover" : ""}`;
          return (
            <div
              className={columnClass}
              key={status}
              onDragOver={handleColumnDragOver(status)}
              onDragLeave={handleColumnDragLeave(status)}
              onDrop={handleColumnDrop(status)}
            >
              <div className="task-column-header">
                <span>{COLUMN_LABEL[status]}</span>
                <span className="task-column-count">{column.length}</span>
              </div>
              {column.map((task) => (
                <TaskCard
                  key={task.id}
                  task={task}
                  isDragging={draggingId === task.id}
                  onDragStart={handleDragStart(task.id)}
                  onDragEnd={handleDragEnd}
                  onOpen={() => setSelectedTaskId(task.id)}
                />
              ))}
            </div>
          );
        })}
      </div>
      {selectedTask && (
        <TaskDetailModal
          task={selectedTask}
          onClose={() => setSelectedTaskId(null)}
        />
      )}
    </>
  );
}

interface TaskCardProps {
  task: Task;
  isDragging: boolean;
  onDragStart: (event: DragEvent<HTMLDivElement>) => void;
  onDragEnd: (event: DragEvent<HTMLDivElement>) => void;
  onOpen: () => void;
}

function TaskCard({
  task,
  isDragging,
  onDragStart,
  onDragEnd,
  onOpen,
}: TaskCardProps) {
  const status = normalizeStatus(task.status);
  const timestamp = task.updated_at ?? task.created_at;
  const className = `app-card task-card${isDragging ? " dragging" : ""}`;

  function handleKeyDown(event: React.KeyboardEvent<HTMLDivElement>) {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      onOpen();
    }
  }

  return (
    <div
      className={className}
      draggable={true}
      onDragStart={onDragStart}
      onDragEnd={onDragEnd}
      onClick={onOpen}
      onKeyDown={handleKeyDown}
      role="button"
      tabIndex={0}
      style={{ marginBottom: 8, cursor: "pointer" }}
    >
      <div className="app-card-title">{task.title || "Untitled"}</div>
      {task.description && (
        <div
          style={{
            fontSize: 12,
            color: "var(--text-secondary)",
            marginBottom: 8,
            lineHeight: 1.45,
          }}
        >
          {task.description.slice(0, 160)}
        </div>
      )}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 8,
          flexWrap: "wrap",
        }}
      >
        <span className={statusBadgeClass(status)}>{COLUMN_LABEL[status]}</span>
        {task.owner && <span className="app-card-meta">@{task.owner}</span>}
        {task.channel && <span className="app-card-meta">#{task.channel}</span>}
        {timestamp && (
          <span className="app-card-meta">{formatRelativeTime(timestamp)}</span>
        )}
      </div>
    </div>
  );
}
