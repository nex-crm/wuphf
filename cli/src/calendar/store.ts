/**
 * Calendar persistence layer.
 * Stores scheduled tasks in a JSON file at ~/.wuphf/calendar.json
 */

import { readFileSync, writeFileSync, mkdirSync, existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { homedir } from 'node:os';
import type { ScheduledTask } from './types.js';

export class CalendarStore {
  private filePath: string;

  constructor(filePath?: string) {
    this.filePath = filePath ?? join(homedir(), '.wuphf', 'calendar.json');
  }

  load(): ScheduledTask[] {
    try {
      if (!existsSync(this.filePath)) return [];
      const raw = readFileSync(this.filePath, 'utf-8');
      const data = JSON.parse(raw);
      return Array.isArray(data) ? data as ScheduledTask[] : [];
    } catch {
      return [];
    }
  }

  save(tasks: ScheduledTask[]): void {
    mkdirSync(dirname(this.filePath), { recursive: true });
    writeFileSync(this.filePath, JSON.stringify(tasks, null, 2) + '\n', 'utf-8');
  }

  add(task: ScheduledTask): void {
    const tasks = this.load();
    tasks.push(task);
    this.save(tasks);
  }

  remove(taskId: string): void {
    const tasks = this.load().filter(t => t.id !== taskId);
    this.save(tasks);
  }

  update(taskId: string, updates: Partial<ScheduledTask>): void {
    const tasks = this.load();
    const idx = tasks.findIndex(t => t.id === taskId);
    if (idx >= 0) {
      tasks[idx] = { ...tasks[idx], ...updates };
      this.save(tasks);
    }
  }

  getByAgent(agentSlug: string): ScheduledTask[] {
    return this.load().filter(t => t.agentSlug === agentSlug);
  }
}
