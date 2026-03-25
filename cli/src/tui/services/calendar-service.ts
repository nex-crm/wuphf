/**
 * TUI service layer for the calendar/scheduling system.
 * Wraps Scheduler and CalendarStore into a single facade
 * that the calendar view can consume without touching backend internals.
 */

import { join } from 'node:path';
import { homedir } from 'node:os';
import { Scheduler } from '../../calendar/scheduler.js';
import { CalendarStore } from '../../calendar/store.js';
import type { ScheduledTask } from '../../calendar/types.js';

export class CalendarService {
  private scheduler: Scheduler;
  private store: CalendarStore;
  private listeners: Array<() => void> = [];

  constructor(calendarFilePath?: string) {
    const filePath = calendarFilePath ?? join(homedir(), '.wuphf', 'calendar.json');
    this.store = new CalendarStore(filePath);
    this.scheduler = new Scheduler(this.store);
  }

  // ── View data ──

  /**
   * Returns events formatted for the calendar grid view.
   * Maps scheduled tasks to day/hour slots for the given week.
   */
  getWeekEvents(weekOffset: number = 0): Array<{
    id: string;
    agentName: string;
    day: number;
    hour: number;
    label: string;
  }> {
    const { start, end } = this.getWeekRange(weekOffset);
    const tasks = this.scheduler.getUpcoming(start, end);
    const allTasks = this.store.load().filter(t => t.enabled);

    // For recurring tasks, generate occurrences within the week window
    const events: Array<{
      id: string;
      agentName: string;
      day: number;
      hour: number;
      label: string;
    }> = [];

    // Include tasks whose nextRun falls within the week
    for (const task of tasks) {
      const runDate = new Date(task.nextRun);
      events.push({
        id: task.id,
        agentName: task.agentSlug,
        day: runDate.getDay(),
        hour: runDate.getHours(),
        label: `${task.agentSlug} ${task.type}`,
      });
    }

    // Also scan all enabled tasks and project their cron into the week
    for (const task of allTasks) {
      // Skip tasks already captured via getUpcoming
      if (tasks.some(t => t.id === task.id)) continue;

      try {
        let cursor = new Date(start.getTime());
        // Walk through the week generating occurrences
        while (cursor.getTime() <= end.getTime()) {
          const next = this.scheduler.nextRun(task.cronExpression, cursor);
          if (next.getTime() > end.getTime()) break;
          if (next.getTime() >= start.getTime()) {
            events.push({
              id: task.id,
              agentName: task.agentSlug,
              day: next.getDay(),
              hour: next.getHours(),
              label: `${task.agentSlug} ${task.type}`,
            });
          }
          // Advance cursor past this occurrence
          cursor = new Date(next.getTime());
        }
      } catch {
        // Skip tasks with unparseable cron
      }
    }

    return events;
  }

  // ── Scheduling ──

  scheduleHeartbeat(agentSlug: string, agentName: string, cronExpression: string): void {
    this.scheduler.schedule({
      agentSlug,
      type: 'heartbeat',
      cronExpression,
      enabled: true,
    });
    this.notify();
  }

  removeSchedule(agentSlug: string): void {
    const agentTasks = this.store.getByAgent(agentSlug);
    for (const task of agentTasks) {
      this.store.remove(task.id);
    }
    this.notify();
  }

  // ── Queries ──

  getUpcoming(hours: number = 24): ScheduledTask[] {
    const now = new Date();
    const to = new Date(now.getTime() + hours * 60 * 60 * 1000);
    return this.scheduler.getUpcoming(now, to);
  }

  getMissed(): ScheduledTask[] {
    return this.scheduler.getMissed();
  }

  // ── Subscription for TUI re-renders ──

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

  // ── Internal helpers ──

  private getWeekRange(offset: number): { start: Date; end: Date } {
    const now = new Date();
    const dayOfWeek = now.getDay();
    const start = new Date(now);
    start.setDate(now.getDate() - dayOfWeek + offset * 7);
    start.setHours(0, 0, 0, 0);
    const end = new Date(start);
    end.setDate(start.getDate() + 6);
    end.setHours(23, 59, 59, 999);
    return { start, end };
  }
}

// ── Singleton accessor ──

let instance: CalendarService | undefined;

export function getCalendarService(): CalendarService {
  if (!instance) {
    instance = new CalendarService();
  }
  return instance;
}
