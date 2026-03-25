/**
 * Orchestration dashboard view.
 * Shows active goals, task pool, per-agent budgets, and global budget.
 */

import React from 'react';
import { Box, Text } from 'ink';
import type { GoalDefinition, TaskDefinition, BudgetSnapshot } from '../../orchestration/types.js';

// ── Types ──

export interface OrchestrationViewProps {
  goals: GoalDefinition[];
  tasks: TaskDefinition[];
  budgets: BudgetSnapshot[];
  globalBudget: { tokens: number; cost: number; percentTokens: number; percentCost: number };
}

// ── Helpers ──

function statusIcon(status: string): string {
  switch (status) {
    case 'completed':
      return '\u25CF'; // filled circle
    case 'in_progress':
    case 'locked':
      return '\u25D0'; // half circle
    case 'failed':
      return '\u2716'; // cross
    case 'active':
      return '\u25D0';
    case 'paused':
      return '\u25CB'; // open circle
    default:
      return '\u25CB';
  }
}

function statusColor(status: string): string {
  switch (status) {
    case 'completed':
      return 'green';
    case 'in_progress':
    case 'locked':
    case 'active':
      return 'yellow';
    case 'failed':
      return 'red';
    default:
      return 'gray';
  }
}

function budgetBar(percent: number, width: number = 10): string {
  const clamped = Math.max(0, Math.min(100, percent));
  const filled = Math.round((clamped / 100) * width);
  const empty = width - filled;
  return '\u2588'.repeat(filled) + '\u2591'.repeat(empty);
}

function countByStatus(
  tasks: TaskDefinition[],
  status: TaskDefinition['status'],
): number {
  return tasks.filter((t) => t.status === status).length;
}

// ── Sub-components ──

function GoalRow({
  goal,
  tasks,
}: {
  goal: GoalDefinition;
  tasks: TaskDefinition[];
}): React.JSX.Element {
  const goalTasks = tasks.filter((t) => t.parentGoalId === goal.id);
  const completed = goalTasks.filter((t) => t.status === 'completed').length;
  const total = goalTasks.length;

  return (
    <Box>
      <Text color="gray">{goal === undefined ? '\u2514' : '\u251C'} </Text>
      <Text>{goal.title} </Text>
      <Text color="gray">[{completed}/{total} tasks] </Text>
      <Text color={statusColor(goal.status)}>
        {statusIcon(goal.status)} {goal.status}
      </Text>
    </Box>
  );
}

function GoalSection({
  goals,
  tasks,
}: {
  goals: GoalDefinition[];
  tasks: TaskDefinition[];
}): React.JSX.Element {
  if (goals.length === 0) {
    return (
      <Box flexDirection="column" marginBottom={1}>
        <Text bold>Goals</Text>
        <Text color="gray">  No goals defined yet.</Text>
      </Box>
    );
  }

  const active = goals.filter((g) => g.status === 'active').length;

  return (
    <Box flexDirection="column" marginBottom={1}>
      <Text bold>Goals ({active} active)</Text>
      {goals.map((goal, i) => (
        <GoalRow key={goal.id ?? i} goal={goal} tasks={tasks} />
      ))}
    </Box>
  );
}

function TaskPoolSection({
  tasks,
}: {
  tasks: TaskDefinition[];
}): React.JSX.Element {
  const completed = countByStatus(tasks, 'completed');
  const inProgress =
    countByStatus(tasks, 'in_progress') + countByStatus(tasks, 'locked');
  const pending = countByStatus(tasks, 'pending');
  const failed = countByStatus(tasks, 'failed');

  return (
    <Box flexDirection="column" marginBottom={1}>
      <Text bold>Task Pool</Text>
      <Box gap={2}>
        <Text color="green">{'\u25CF'} {completed} completed</Text>
        <Text color="yellow">{'\u25D0'} {inProgress} in progress</Text>
        <Text color="gray">{'\u25CB'} {pending} pending</Text>
        {failed > 0 && (
          <Text color="red">{'\u2716'} {failed} failed</Text>
        )}
      </Box>
    </Box>
  );
}

function BudgetSection({
  budgets,
  globalBudget,
}: {
  budgets: BudgetSnapshot[];
  globalBudget: OrchestrationViewProps['globalBudget'];
}): React.JSX.Element {
  const globalPercent = Math.max(
    globalBudget.percentTokens,
    globalBudget.percentCost,
  );

  return (
    <Box flexDirection="column" marginBottom={1}>
      <Text bold>Budget</Text>
      {budgets.map((b) => (
        <Box key={b.agentSlug} gap={1}>
          <Text>
            {b.agentSlug.padEnd(14)}
          </Text>
          <Text color={b.warning ? 'red' : b.percentUsed > 50 ? 'yellow' : 'green'}>
            {budgetBar(b.percentUsed)}
          </Text>
          <Text> {Math.round(b.percentUsed)}%</Text>
          <Text color="gray"> ${b.costUsd.toFixed(2)}</Text>
        </Box>
      ))}
      <Box gap={1}>
        <Text>{'Global'.padEnd(14)}</Text>
        <Text color={globalPercent > 80 ? 'red' : globalPercent > 50 ? 'yellow' : 'green'}>
          {budgetBar(globalPercent)}
        </Text>
        <Text> {Math.round(globalPercent)}%</Text>
        <Text color="gray"> ${globalBudget.cost.toFixed(2)}</Text>
      </Box>
    </Box>
  );
}

// ── Main view ──

export function OrchestrationView({
  goals,
  tasks,
  budgets,
  globalBudget,
}: OrchestrationViewProps): React.JSX.Element {
  return (
    <Box
      flexDirection="column"
      borderStyle="round"
      borderColor="cyan"
      paddingX={1}
      paddingY={0}
    >
      <Text bold color="cyan">
        Orchestration
      </Text>
      <Box height={1} />
      <GoalSection goals={goals} tasks={tasks} />
      <TaskPoolSection tasks={tasks} />
      <BudgetSection budgets={budgets} globalBudget={globalBudget} />
      <Text color="gray">
        [Esc=back  t=tasks  g=goals  w=workflows]
      </Text>
    </Box>
  );
}
