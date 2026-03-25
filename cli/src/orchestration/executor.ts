/**
 * Concurrent agent executor for multi-agent orchestration.
 * Manages task checkout/release, concurrency limits, and lifecycle events.
 */

import { EventEmitter } from 'node:events';
import type { TaskDefinition, OrchestratorConfig } from './types.js';

export type ExecutorEvent =
  | { type: 'task:start'; taskId: string; agentSlug: string }
  | { type: 'task:complete'; taskId: string; result?: string }
  | { type: 'task:fail'; taskId: string; error: string }
  | { type: 'task:timeout'; taskId: string };

export class OrchestratorExecutor {
  private config: OrchestratorConfig;
  private tasks: Map<string, TaskDefinition> = new Map();
  private locks: Map<string, string> = new Map(); // taskId -> agentSlug
  private activeCount = 0;
  private emitter = new EventEmitter();
  private timers: Map<string, ReturnType<typeof setTimeout>> = new Map();
  private stopped = false;

  constructor(config: OrchestratorConfig) {
    this.config = config;
  }

  /** Subscribe to executor events. */
  on(handler: (event: ExecutorEvent) => void): () => void {
    this.emitter.on('event', handler);
    return () => {
      this.emitter.off('event', handler);
    };
  }

  private emit(event: ExecutorEvent): void {
    this.emitter.emit('event', event);
  }

  /**
   * Atomic task checkout -- prevents duplicate assignment.
   * Returns true if the task was successfully locked for this agent.
   */
  async checkout(taskId: string, agentSlug: string): Promise<boolean> {
    // Already locked by someone else
    if (this.locks.has(taskId)) return false;

    const task = this.tasks.get(taskId);
    if (!task) return false;
    if (task.status !== 'pending') return false;

    // Concurrency limit
    if (this.activeCount >= this.config.maxConcurrentAgents) return false;

    // Lock it
    this.locks.set(taskId, agentSlug);
    task.status = 'locked';
    task.assignedAgent = agentSlug;
    this.tasks.set(taskId, task);
    return true;
  }

  /**
   * Release a task on completion or failure.
   */
  async release(
    taskId: string,
    result?: string,
    error?: string,
  ): Promise<void> {
    const task = this.tasks.get(taskId);
    if (!task) return;

    // Clear timeout timer
    const timer = this.timers.get(taskId);
    if (timer) {
      clearTimeout(timer);
      this.timers.delete(taskId);
    }

    this.locks.delete(taskId);

    if (error) {
      task.status = 'failed';
      task.result = error;
      task.completedAt = Date.now();
      this.emit({ type: 'task:fail', taskId, error });
    } else {
      task.status = 'completed';
      task.result = result;
      task.completedAt = Date.now();
      this.emit({ type: 'task:complete', taskId, result });
    }

    this.tasks.set(taskId, task);
    if (this.activeCount > 0) this.activeCount--;
  }

  /**
   * Submit a task for execution.
   * Stores the task; actual execution happens during runBatch or by
   * the external executor loop calling checkout/release.
   */
  async submit(task: TaskDefinition): Promise<void> {
    this.tasks.set(task.id, { ...task, status: 'pending' });
  }

  /**
   * Run multiple tasks concurrently, respecting maxConcurrentAgents.
   * Assigns tasks to their assignedAgent (or marks them as in_progress).
   * Returns final task states.
   */
  async runBatch(
    tasks: TaskDefinition[],
  ): Promise<Map<string, TaskDefinition>> {
    this.stopped = false;

    // Register all tasks
    for (const t of tasks) {
      await this.submit(t);
    }

    const pending = tasks.map((t) => t.id);
    const results = new Map<string, TaskDefinition>();

    // Process in waves respecting concurrency limit
    const runNext = async (): Promise<void> => {
      while (pending.length > 0 && !this.stopped) {
        // Fill up to max concurrent
        const toStart: string[] = [];
        while (
          pending.length > 0 &&
          this.activeCount < this.config.maxConcurrentAgents
        ) {
          const taskId = pending.shift()!;
          const task = this.tasks.get(taskId);
          if (!task) continue;

          const agent = task.assignedAgent ?? 'default';
          const locked = await this.checkout(taskId, agent);
          if (locked) {
            toStart.push(taskId);
            this.activeCount++;
          } else {
            // Put back for retry
            pending.push(taskId);
            break;
          }
        }

        // Start timeout timers and mark as in_progress
        const completions = toStart.map((taskId) => {
          const task = this.tasks.get(taskId)!;
          task.status = 'in_progress';
          this.tasks.set(taskId, task);
          this.emit({
            type: 'task:start',
            taskId,
            agentSlug: task.assignedAgent ?? 'default',
          });

          // Set timeout
          return new Promise<void>((resolve) => {
            const timer = setTimeout(async () => {
              const current = this.tasks.get(taskId);
              if (current && current.status === 'in_progress') {
                this.emit({ type: 'task:timeout', taskId });
                await this.release(taskId, undefined, 'Task timed out');
              }
              resolve();
            }, this.config.taskTimeout);
            this.timers.set(taskId, timer);

            // Simulate immediate completion for batch mode
            // (real execution is driven externally)
            resolve();
          });
        });

        await Promise.all(completions);

        // Collect results for started tasks
        for (const taskId of toStart) {
          const task = this.tasks.get(taskId);
          if (task) results.set(taskId, task);
        }

        // If all tasks started, break
        if (pending.length === 0) break;
      }
    };

    await runNext();

    // Return all task states
    for (const [id, task] of this.tasks) {
      results.set(id, task);
    }
    return results;
  }

  /** Get currently active (in_progress) tasks. */
  getActive(): TaskDefinition[] {
    const active: TaskDefinition[] = [];
    for (const task of this.tasks.values()) {
      if (task.status === 'in_progress' || task.status === 'locked') {
        active.push(task);
      }
    }
    return active;
  }

  /** Stop all running tasks. */
  async stopAll(): Promise<void> {
    this.stopped = true;
    for (const [taskId, timer] of this.timers) {
      clearTimeout(timer);
      this.timers.delete(taskId);
    }
    for (const task of this.tasks.values()) {
      if (task.status === 'in_progress' || task.status === 'locked') {
        task.status = 'failed';
        task.result = 'Stopped by orchestrator';
        task.completedAt = Date.now();
        this.locks.delete(task.id);
      }
    }
    this.activeCount = 0;
  }
}
