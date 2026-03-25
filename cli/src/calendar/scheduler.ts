/**
 * Cron parser and scheduling engine.
 * Supports: standard 5-field cron, "daily", "hourly", "Nh" shorthand.
 * No external dependencies.
 */

import { randomUUID } from 'node:crypto';
import type { CalendarStore } from './store.js';
import type { ScheduledTask } from './types.js';

/**
 * Parse human-friendly cron expressions to standard 5-field cron.
 * Supports:
 *   "daily"   -> "0 0 * * *"
 *   "hourly"  -> "0 * * * *"
 *   "6h"      -> "0 *​/6 * * *"  (every N hours)
 *   "30m"     -> "*​/30 * * * *" (every N minutes)
 *   Standard 5-field cron passed through as-is.
 */
export function parseCron(expression: string): string {
  const trimmed = expression.trim().toLowerCase();

  if (trimmed === 'daily') return '0 0 * * *';
  if (trimmed === 'hourly') return '0 * * * *';
  if (trimmed === 'weekly') return '0 0 * * 0';

  // Nh pattern (e.g. "6h", "12h")
  const hourMatch = trimmed.match(/^(\d+)h$/);
  if (hourMatch) {
    const n = parseInt(hourMatch[1], 10);
    if (n > 0 && n <= 23) return `0 */${n} * * *`;
  }

  // Nm pattern (e.g. "30m", "15m")
  const minMatch = trimmed.match(/^(\d+)m$/);
  if (minMatch) {
    const n = parseInt(minMatch[1], 10);
    if (n > 0 && n <= 59) return `*/${n} * * * *`;
  }

  // Standard 5-field cron: validate it has 5 space-separated fields with valid chars
  const fields = expression.trim().split(/\s+/);
  const cronFieldRe = /^(\*|\d+)([/-]\d+)?([,](\*|\d+)([/-]\d+)?)*$/;
  if (fields.length === 5 && fields.every(f => cronFieldRe.test(f))) {
    return expression.trim();
  }

  throw new Error(`Invalid cron expression: ${expression}`);
}

/**
 * Parse a single cron field and return the set of matching values.
 * Supports: *, N, N-M, *​/N, N-M/S, comma-separated lists.
 */
function parseCronField(field: string, min: number, max: number): number[] {
  const values = new Set<number>();

  for (const part of field.split(',')) {
    const trimmed = part.trim();

    // */N
    const stepWild = trimmed.match(/^\*\/(\d+)$/);
    if (stepWild) {
      const step = parseInt(stepWild[1], 10);
      for (let i = min; i <= max; i += step) values.add(i);
      continue;
    }

    // N-M/S
    const rangeStep = trimmed.match(/^(\d+)-(\d+)\/(\d+)$/);
    if (rangeStep) {
      const start = parseInt(rangeStep[1], 10);
      const end = parseInt(rangeStep[2], 10);
      const step = parseInt(rangeStep[3], 10);
      for (let i = start; i <= end; i += step) values.add(i);
      continue;
    }

    // N-M
    const range = trimmed.match(/^(\d+)-(\d+)$/);
    if (range) {
      const start = parseInt(range[1], 10);
      const end = parseInt(range[2], 10);
      for (let i = start; i <= end; i++) values.add(i);
      continue;
    }

    // *
    if (trimmed === '*') {
      for (let i = min; i <= max; i++) values.add(i);
      continue;
    }

    // Single number
    const n = parseInt(trimmed, 10);
    if (!isNaN(n) && n >= min && n <= max) {
      values.add(n);
    }
  }

  return Array.from(values).sort((a, b) => a - b);
}

/**
 * Calculate the next run time for a cron expression after the given date.
 * Brute-forces by advancing minute-by-minute (bounded to 400 days).
 */
export function nextRun(cronExpression: string, after?: Date): Date {
  const cron = parseCron(cronExpression);
  const fields = cron.split(/\s+/);
  if (fields.length !== 5) throw new Error(`Invalid cron: ${cronExpression}`);

  const minutes = parseCronField(fields[0], 0, 59);
  const hours = parseCronField(fields[1], 0, 23);
  const daysOfMonth = parseCronField(fields[2], 1, 31);
  const months = parseCronField(fields[3], 1, 12);
  const daysOfWeek = parseCronField(fields[4], 0, 6);

  const start = after ? new Date(after.getTime()) : new Date();
  // Advance to next minute (UTC)
  start.setUTCSeconds(0, 0);
  start.setUTCMinutes(start.getUTCMinutes() + 1);

  const maxIterations = 400 * 24 * 60; // ~400 days in minutes
  const candidate = new Date(start.getTime());

  for (let i = 0; i < maxIterations; i++) {
    const m = candidate.getUTCMinutes();
    const h = candidate.getUTCHours();
    const dom = candidate.getUTCDate();
    const mon = candidate.getUTCMonth() + 1; // 0-indexed -> 1-indexed
    const dow = candidate.getUTCDay();

    if (
      minutes.includes(m) &&
      hours.includes(h) &&
      daysOfMonth.includes(dom) &&
      months.includes(mon) &&
      daysOfWeek.includes(dow)
    ) {
      return candidate;
    }

    candidate.setUTCMinutes(candidate.getUTCMinutes() + 1);
  }

  // Fallback: return 24h from now if no match found
  return new Date(Date.now() + 24 * 60 * 60 * 1000);
}

export class Scheduler {
  private store: CalendarStore;

  constructor(store: CalendarStore) {
    this.store = store;
  }

  parseCron(expression: string): string {
    return parseCron(expression);
  }

  nextRun(cronExpression: string, after?: Date): Date {
    return nextRun(cronExpression, after);
  }

  getMissed(): ScheduledTask[] {
    const now = Date.now();
    const tasks = this.store.load();
    return tasks.filter(t => t.enabled && t.nextRun < now && !t.missed);
  }

  schedule(task: Omit<ScheduledTask, 'id' | 'nextRun' | 'missed'>): ScheduledTask {
    const nextRunTime = nextRun(task.cronExpression);
    const full: ScheduledTask = {
      ...task,
      id: randomUUID(),
      nextRun: nextRunTime.getTime(),
      missed: false,
    };
    this.store.add(full);
    return full;
  }

  getUpcoming(from: Date, to: Date): ScheduledTask[] {
    const fromMs = from.getTime();
    const toMs = to.getTime();
    return this.store.load().filter(
      t => t.enabled && t.nextRun >= fromMs && t.nextRun <= toMs,
    );
  }

  markCompleted(taskId: string): void {
    const tasks = this.store.load();
    const task = tasks.find(t => t.id === taskId);
    if (!task) return;

    const nextRunTime = nextRun(task.cronExpression);
    this.store.update(taskId, {
      lastRun: Date.now(),
      nextRun: nextRunTime.getTime(),
      missed: false,
    });
  }
}
