import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import React from 'react';
import { render } from 'ink-testing-library';
import { OrchestrationView } from '../../../src/tui/views/orchestration.js';
import type { GoalDefinition, TaskDefinition, BudgetSnapshot } from '../../../src/orchestration/types.js';

// Strip ANSI escape sequences for assertion matching
function strip(s: string): string {
  return s.replace(/\x1b\[[0-9;]*m/g, '');
}

describe('OrchestrationView', () => {
  const now = Date.now();

  const sampleGoals: GoalDefinition[] = [
    {
      id: 'g1',
      title: 'SEO Audit',
      description: 'Analyze SEO',
      tasks: ['t1', 't2', 't3'],
      status: 'completed',
      createdAt: now,
    },
    {
      id: 'g2',
      title: 'Lead Generation',
      description: 'Find leads',
      tasks: ['t4', 't5'],
      status: 'active',
      createdAt: now,
    },
  ];

  const sampleTasks: TaskDefinition[] = [
    {
      id: 't1',
      title: 'Keyword analysis',
      description: 'Research keywords',
      requiredSkills: ['seo'],
      parentGoalId: 'g1',
      priority: 'high',
      status: 'completed',
      createdAt: now,
      completedAt: now + 1000,
    },
    {
      id: 't2',
      title: 'Content gap',
      description: 'Find gaps',
      requiredSkills: ['seo'],
      parentGoalId: 'g1',
      priority: 'medium',
      status: 'completed',
      createdAt: now,
      completedAt: now + 2000,
    },
    {
      id: 't3',
      title: 'Competitor review',
      description: 'Analyze competitors',
      requiredSkills: ['seo'],
      parentGoalId: 'g1',
      priority: 'medium',
      status: 'completed',
      createdAt: now,
      completedAt: now + 3000,
    },
    {
      id: 't4',
      title: 'Prospect discovery',
      description: 'Find leads',
      requiredSkills: ['prospecting'],
      parentGoalId: 'g2',
      priority: 'high',
      status: 'in_progress',
      assignedAgent: 'lead-gen',
      createdAt: now,
    },
    {
      id: 't5',
      title: 'Lead enrichment',
      description: 'Enrich leads',
      requiredSkills: ['data-enrichment'],
      parentGoalId: 'g2',
      priority: 'medium',
      status: 'pending',
      createdAt: now,
    },
  ];

  const sampleBudgets: BudgetSnapshot[] = [
    {
      agentSlug: 'seo-agent',
      tokensUsed: 7800,
      costUsd: 0.12,
      budgetLimit: { maxTokens: 10_000, maxCostUsd: 0.5 },
      percentUsed: 78,
      warning: false,
      exceeded: false,
    },
    {
      agentSlug: 'lead-gen',
      tokensUsed: 2300,
      costUsd: 0.04,
      budgetLimit: { maxTokens: 10_000, maxCostUsd: 0.5 },
      percentUsed: 23,
      warning: false,
      exceeded: false,
    },
  ];

  const sampleGlobalBudget = {
    tokens: 10100,
    cost: 0.16,
    percentTokens: 45,
    percentCost: 32,
  };

  it('renders goal list with status', () => {
    const { lastFrame } = render(
      <OrchestrationView
        goals={sampleGoals}
        tasks={sampleTasks}
        budgets={sampleBudgets}
        globalBudget={sampleGlobalBudget}
      />,
    );
    const frame = strip(lastFrame() ?? '');
    assert.ok(frame.includes('SEO Audit'), 'should show first goal title');
    assert.ok(frame.includes('Lead Generation'), 'should show second goal title');
    assert.ok(frame.includes('completed'), 'should show completed status');
    assert.ok(frame.includes('active'), 'should show active status');
  });

  it('renders task pool counts', () => {
    const { lastFrame } = render(
      <OrchestrationView
        goals={sampleGoals}
        tasks={sampleTasks}
        budgets={sampleBudgets}
        globalBudget={sampleGlobalBudget}
      />,
    );
    const frame = strip(lastFrame() ?? '');
    assert.ok(frame.includes('3 completed'), 'should show completed count');
    assert.ok(frame.includes('1 in progress'), 'should show in-progress count');
    assert.ok(frame.includes('1 pending'), 'should show pending count');
  });

  it('renders budget bars', () => {
    const { lastFrame } = render(
      <OrchestrationView
        goals={sampleGoals}
        tasks={sampleTasks}
        budgets={sampleBudgets}
        globalBudget={sampleGlobalBudget}
      />,
    );
    const frame = strip(lastFrame() ?? '');
    assert.ok(frame.includes('seo-agent'), 'should show seo-agent budget');
    assert.ok(frame.includes('lead-gen'), 'should show lead-gen budget');
    assert.ok(frame.includes('Global'), 'should show global budget');
    assert.ok(frame.includes('78%'), 'should show seo-agent percentage');
    assert.ok(frame.includes('23%'), 'should show lead-gen percentage');
    assert.ok(frame.includes('$0.12'), 'should show seo-agent cost');
    assert.ok(frame.includes('$0.04'), 'should show lead-gen cost');
  });

  it('renders task counts in goal rows', () => {
    const { lastFrame } = render(
      <OrchestrationView
        goals={sampleGoals}
        tasks={sampleTasks}
        budgets={sampleBudgets}
        globalBudget={sampleGlobalBudget}
      />,
    );
    const frame = strip(lastFrame() ?? '');
    assert.ok(frame.includes('[3/3 tasks]'), 'should show SEO Audit task counts');
    assert.ok(frame.includes('[0/2 tasks]'), 'should show Lead Gen task counts');
  });

  it('shows empty state when no goals', () => {
    const { lastFrame } = render(
      <OrchestrationView
        goals={[]}
        tasks={[]}
        budgets={[]}
        globalBudget={{ tokens: 0, cost: 0, percentTokens: 0, percentCost: 0 }}
      />,
    );
    const frame = strip(lastFrame() ?? '');
    assert.ok(frame.includes('No goals defined yet'), 'should show empty state text');
  });

  it('renders keyboard hints', () => {
    const { lastFrame } = render(
      <OrchestrationView
        goals={[]}
        tasks={[]}
        budgets={[]}
        globalBudget={{ tokens: 0, cost: 0, percentTokens: 0, percentCost: 0 }}
      />,
    );
    const frame = strip(lastFrame() ?? '');
    assert.ok(frame.includes('Esc=back'), 'should show escape hint');
    assert.ok(frame.includes('t=tasks'), 'should show tasks hint');
    assert.ok(frame.includes('g=goals'), 'should show goals hint');
    assert.ok(frame.includes('w=workflows'), 'should show workflows hint');
  });

  it('renders the Orchestration title', () => {
    const { lastFrame } = render(
      <OrchestrationView
        goals={[]}
        tasks={[]}
        budgets={[]}
        globalBudget={{ tokens: 0, cost: 0, percentTokens: 0, percentCost: 0 }}
      />,
    );
    const frame = strip(lastFrame() ?? '');
    assert.ok(frame.includes('Orchestration'), 'should show Orchestration title');
  });
});
