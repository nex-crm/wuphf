/**
 * Type definitions for the agent calendar/scheduling system.
 */

export interface ScheduledTask {
  id: string;
  agentSlug: string;
  type: 'heartbeat' | 'task' | 'review';
  cronExpression: string;
  nextRun: number;
  lastRun?: number;
  missed: boolean;
  enabled: boolean;
}

export interface CalendarEntry {
  date: string; // YYYY-MM-DD
  tasks: ScheduledTask[];
}
