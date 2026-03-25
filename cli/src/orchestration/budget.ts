/**
 * Budget tracking for multi-agent orchestration.
 * Tracks per-agent token/cost usage against global and per-agent limits.
 */

import type { BudgetSnapshot } from './types.js';

interface AgentUsage {
  tokensUsed: number;
  costUsd: number;
}

export class BudgetTracker {
  private globalBudget: { maxTokens: number; maxCostUsd: number };
  private usage: Map<string, AgentUsage> = new Map();

  constructor(globalBudget: { maxTokens: number; maxCostUsd: number }) {
    this.globalBudget = globalBudget;
  }

  /** Track usage for an agent. */
  record(agentSlug: string, tokens: number, costUsd: number): void {
    const current = this.usage.get(agentSlug) ?? { tokensUsed: 0, costUsd: 0 };
    current.tokensUsed += tokens;
    current.costUsd += costUsd;
    this.usage.set(agentSlug, current);
  }

  /** Get budget snapshot for a single agent. */
  getSnapshot(agentSlug: string): BudgetSnapshot {
    const current = this.usage.get(agentSlug) ?? { tokensUsed: 0, costUsd: 0 };
    const percentTokens = this.globalBudget.maxTokens > 0
      ? (current.tokensUsed / this.globalBudget.maxTokens) * 100
      : 0;
    const percentCost = this.globalBudget.maxCostUsd > 0
      ? (current.costUsd / this.globalBudget.maxCostUsd) * 100
      : 0;
    const percentUsed = Math.max(percentTokens, percentCost);

    return {
      agentSlug,
      tokensUsed: current.tokensUsed,
      costUsd: current.costUsd,
      budgetLimit: { ...this.globalBudget },
      percentUsed,
      warning: percentUsed > 80,
      exceeded: percentUsed > 100,
    };
  }

  /** Get snapshots for all tracked agents. */
  getAllSnapshots(): BudgetSnapshot[] {
    const slugs = [...this.usage.keys()];
    return slugs.map((slug) => this.getSnapshot(slug));
  }

  /** Check if agent can proceed (not over budget). */
  canProceed(agentSlug: string): boolean {
    return !this.getSnapshot(agentSlug).exceeded;
  }

  /** Check if agent is in warning zone (>80% usage). */
  isWarning(agentSlug: string): boolean {
    return this.getSnapshot(agentSlug).warning;
  }

  /** Reset tracked usage for an agent. */
  reset(agentSlug: string): void {
    this.usage.delete(agentSlug);
  }

  /** Get aggregate global usage across all agents. */
  getGlobalUsage(): {
    tokens: number;
    cost: number;
    percentTokens: number;
    percentCost: number;
  } {
    let tokens = 0;
    let cost = 0;
    for (const u of this.usage.values()) {
      tokens += u.tokensUsed;
      cost += u.costUsd;
    }
    const percentTokens = this.globalBudget.maxTokens > 0
      ? (tokens / this.globalBudget.maxTokens) * 100
      : 0;
    const percentCost = this.globalBudget.maxCostUsd > 0
      ? (cost / this.globalBudget.maxCostUsd) * 100
      : 0;
    return { tokens, cost, percentTokens, percentCost };
  }
}
