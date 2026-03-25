/**
 * Core types for the multi-agent orchestration engine.
 * Follows Paperclip patterns: flat task pool, expertise-based routing,
 * atomic checkout, budget tracking.
 */

export interface SkillDeclaration {
  name: string;
  description: string;
  proficiency: number; // 0-1
}

export interface TaskDefinition {
  id: string;
  title: string;
  description: string;
  requiredSkills: string[];
  parentGoalId?: string;    // goal ancestry -- why this task exists
  priority: 'low' | 'medium' | 'high' | 'critical';
  status: 'pending' | 'locked' | 'in_progress' | 'completed' | 'failed';
  assignedAgent?: string;   // agent slug
  budget?: { maxTokens: number; maxCostUsd: number };
  createdAt: number;
  completedAt?: number;
  result?: string;
}

export interface GoalDefinition {
  id: string;
  title: string;
  description: string;
  projectId?: string;
  tasks: string[];          // task IDs
  status: 'active' | 'completed' | 'paused';
  createdAt: number;
}

export interface OrchestratorConfig {
  maxConcurrentAgents: number;  // default 3
  globalBudget: { maxTokens: number; maxCostUsd: number };
  taskTimeout: number;          // ms, default 5 minutes
  autoRetry: boolean;
  maxRetries: number;           // default 2
}

export interface BudgetSnapshot {
  agentSlug: string;
  tokensUsed: number;
  costUsd: number;
  budgetLimit: { maxTokens: number; maxCostUsd: number };
  percentUsed: number;
  warning: boolean;    // true if > 80%
  exceeded: boolean;   // true if > 100%
}
