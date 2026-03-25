/**
 * TUI service layer for multi-agent orchestration.
 * Wraps BudgetTracker, TaskRouter, OrchestratorExecutor, and workflow
 * templates into a single facade the orchestration view can consume.
 */

import { randomUUID } from 'node:crypto';
import { BudgetTracker } from '../../orchestration/budget.js';
import { TaskRouter } from '../../orchestration/router.js';
import { OrchestratorExecutor } from '../../orchestration/executor.js';
import { workflows } from '../../orchestration/templates.js';
import type {
  GoalDefinition,
  TaskDefinition,
  OrchestratorConfig,
  BudgetSnapshot,
  SkillDeclaration,
} from '../../orchestration/types.js';

const DEFAULT_CONFIG: OrchestratorConfig = {
  maxConcurrentAgents: 3,
  globalBudget: { maxTokens: 100_000, maxCostUsd: 0.5 },
  taskTimeout: 5 * 60 * 1000, // 5 minutes
  autoRetry: true,
  maxRetries: 2,
};

export class OrchestrationService {
  private executor: OrchestratorExecutor;
  private budget: BudgetTracker;
  private router: TaskRouter;
  private goals: Map<string, GoalDefinition> = new Map();
  private tasks: Map<string, TaskDefinition> = new Map();
  private listeners: Array<() => void> = [];

  constructor(config?: Partial<OrchestratorConfig>) {
    const merged: OrchestratorConfig = { ...DEFAULT_CONFIG, ...config };
    this.executor = new OrchestratorExecutor(merged);
    this.budget = new BudgetTracker(merged.globalBudget);
    this.router = new TaskRouter();
  }

  // ── Goal management ──

  createGoal(title: string, description: string): GoalDefinition {
    const goal: GoalDefinition = {
      id: randomUUID(),
      title,
      description,
      tasks: [],
      status: 'active',
      createdAt: Date.now(),
    };
    this.goals.set(goal.id, goal);
    this.notify();
    return goal;
  }

  getGoals(): GoalDefinition[] {
    return [...this.goals.values()];
  }

  // ── Task management ──

  createTask(
    task: Omit<TaskDefinition, 'id' | 'createdAt' | 'status'>,
  ): TaskDefinition {
    const full: TaskDefinition = {
      ...task,
      id: randomUUID(),
      status: 'pending',
      createdAt: Date.now(),
    };
    this.tasks.set(full.id, full);

    // Link to parent goal if specified
    if (full.parentGoalId) {
      const goal = this.goals.get(full.parentGoalId);
      if (goal) {
        goal.tasks.push(full.id);
      }
    }

    this.notify();
    return full;
  }

  getTasks(goalId?: string): TaskDefinition[] {
    const all = [...this.tasks.values()];
    if (goalId) {
      const goal = this.goals.get(goalId);
      if (!goal) return [];
      const taskIdSet = new Set(goal.tasks);
      return all.filter((t) => taskIdSet.has(t.id));
    }
    return all;
  }

  getActiveTasks(): TaskDefinition[] {
    return [...this.tasks.values()].filter(
      (t) => t.status === 'in_progress' || t.status === 'locked',
    );
  }

  // ── Workflow templates ──

  instantiateWorkflow(
    templateName: string,
  ): { goal: GoalDefinition; tasks: TaskDefinition[] } {
    const template = workflows[templateName];
    if (!template) {
      throw new Error(`Unknown workflow template: ${templateName}`);
    }

    // Create goal from first template goal (templates define one)
    const goalDef = template.goals[0];
    const goal = this.createGoal(goalDef.title, goalDef.description);

    // Create tasks linked to the goal
    const tasks = template.tasks.map((t) =>
      this.createTask({
        ...t,
        parentGoalId: goal.id,
      }),
    );

    return { goal, tasks };
  }

  getTemplateNames(): string[] {
    return Object.keys(workflows);
  }

  // ── Agent routing ──

  registerAgentSkills(agentSlug: string, skills: SkillDeclaration[]): void {
    this.router.registerAgent(agentSlug, skills);
    this.notify();
  }

  findBestAgentForTask(
    taskId: string,
  ): { agentSlug: string; score: number } | null {
    const task = this.tasks.get(taskId);
    if (!task) return null;
    return this.router.findBestAgent(task);
  }

  // ── Budget ──

  getBudgetSnapshots(): BudgetSnapshot[] {
    return this.budget.getAllSnapshots();
  }

  getGlobalBudget(): {
    tokens: number;
    cost: number;
    percentTokens: number;
    percentCost: number;
  } {
    return this.budget.getGlobalUsage();
  }

  // ── Execution ──

  async submitTask(taskId: string): Promise<void> {
    const task = this.tasks.get(taskId);
    if (!task) throw new Error(`Task not found: ${taskId}`);
    await this.executor.submit(task);
    this.notify();
  }

  async stopAll(): Promise<void> {
    await this.executor.stopAll();
    this.notify();
  }

  // ── Subscribe for TUI re-renders ──

  subscribe(listener: () => void): () => void {
    this.listeners.push(listener);
    return () => {
      const idx = this.listeners.indexOf(listener);
      if (idx >= 0) this.listeners.splice(idx, 1);
    };
  }

  private notify(): void {
    for (const listener of this.listeners) {
      listener();
    }
  }
}

// ── Singleton accessor ──

let instance: OrchestrationService | undefined;

export function getOrchestrationService(): OrchestrationService {
  if (!instance) {
    instance = new OrchestrationService();
  }
  return instance;
}
