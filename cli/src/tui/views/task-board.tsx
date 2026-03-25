/**
 * Task board — 3-column kanban view.
 * Columns: To Do (muted), In Progress (blue), Done (green).
 */

import React from "react";
import { Box, Text } from "ink";

// ── Types ───────────────────────────────────────────────────────────

export type TaskPriority = "urgent" | "high" | "medium" | "low";
export type TaskStatus = "todo" | "in_progress" | "done";

export interface TaskCard {
  id: string;
  title: string;
  priority: TaskPriority;
  status: TaskStatus;
  record?: string;
  due?: string;
}

export interface TaskBoardViewProps {
  tasks: TaskCard[];
  onBack?: () => void;
}

// ── Priority badge ──────────────────────────────────────────────────

const PRIORITY_CONFIG: Record<TaskPriority, { badge: string; color: string }> = {
  urgent: { badge: "!!!", color: "red" },
  high: { badge: "!!", color: "red" },
  medium: { badge: "!", color: "yellow" },
  low: { badge: "\u00B7", color: "gray" },
};

function PriorityBadge({ priority }: { priority: TaskPriority }) {
  const cfg = PRIORITY_CONFIG[priority];
  return <Text color={cfg.color}>{cfg.badge}</Text>;
}

// ── Column config ───────────────────────────────────────────────────

interface ColumnDef {
  status: TaskStatus;
  title: string;
  icon: string;
  color: string;
}

const COLUMNS: ColumnDef[] = [
  { status: "todo", title: "To Do", icon: "\u25CB", color: "gray" },
  { status: "in_progress", title: "In Progress", icon: "\u25D4", color: "blue" },
  { status: "done", title: "Done", icon: "\u25CF", color: "green" },
];

// ── Card component ──────────────────────────────────────────────────

function Card({ task }: { task: TaskCard }) {
  return (
    <Box
      flexDirection="column"
      borderStyle="round"
      paddingX={1}
      marginBottom={1}
    >
      <Box gap={1}>
        <PriorityBadge priority={task.priority} />
        <Text>{task.title}</Text>
      </Box>
      {task.record && (
        <Text dimColor>{task.record}</Text>
      )}
      {task.due && (
        <Text color="yellow">{task.due}</Text>
      )}
    </Box>
  );
}

// ── Column component ────────────────────────────────────────────────

function Column({ def, tasks }: { def: ColumnDef; tasks: TaskCard[] }) {
  return (
    <Box flexDirection="column" flexGrow={1} paddingX={1}>
      {/* Header */}
      <Box gap={1} marginBottom={1}>
        <Text color={def.color}>{def.icon}</Text>
        <Text color={def.color} bold>
          {def.title}
        </Text>
        <Text dimColor>{`(${tasks.length})`}</Text>
      </Box>

      {/* Cards */}
      {tasks.length === 0 ? (
        <Text dimColor>{"No tasks"}</Text>
      ) : (
        tasks.map((task) => <Card key={task.id} task={task} />)
      )}
    </Box>
  );
}

// ── Main view ───────────────────────────────────────────────────────

export function TaskBoardView({
  tasks,
  onBack: _onBack,
}: TaskBoardViewProps): React.JSX.Element {
  const byStatus = (status: TaskStatus) =>
    tasks.filter((t) => t.status === status);

  return (
    <Box flexDirection="column" width="100%">
      {/* Title */}
      <Box paddingX={2} marginBottom={1}>
        <Text bold color="cyan">
          {"Task Board"}
        </Text>
        <Text dimColor>{`  (${tasks.length} tasks)`}</Text>
      </Box>

      {/* Columns */}
      <Box paddingX={1}>
        {COLUMNS.map((col) => (
          <Column key={col.status} def={col} tasks={byStatus(col.status)} />
        ))}
      </Box>

      {/* Navigation hint */}
      <Box marginTop={1} paddingX={2}>
        <Text dimColor>{"[Esc=back]"}</Text>
      </Box>
    </Box>
  );
}

export default TaskBoardView;
