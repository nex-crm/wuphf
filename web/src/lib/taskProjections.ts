import type { Task } from "../api/tasks";

/**
 * Canonical task statuses recognised by the broker. Free-form `task.status`
 * values from the wire are normalised into one of these via
 * `normalizeTaskStatus` so projection helpers always return predictable bucket
 * keys.
 */
export const TASK_STATUSES = [
  "in_progress",
  "open",
  "review",
  "pending",
  "blocked",
  "done",
  "canceled",
] as const;

export type TaskStatus = (typeof TASK_STATUSES)[number];

const STATUS_SET = new Set<string>(TASK_STATUSES);

/**
 * Normalise a wire-format status string into a canonical {@link TaskStatus}.
 *
 * The broker emits a few legacy aliases (`completed`, `in_review`, `cancelled`,
 * casing/whitespace variants). Unknown values fall back to `"open"` which
 * matches the existing `TasksApp` behaviour.
 */
export function normalizeTaskStatus(raw: string | undefined): TaskStatus {
  if (!raw) return "open";
  const normalised = raw.toLowerCase().replace(/[\s-]+/g, "_");
  if (normalised === "completed") return "done";
  if (normalised === "in_review") return "review";
  if (normalised === "cancelled") return "canceled";
  if (STATUS_SET.has(normalised)) return normalised as TaskStatus;
  return "open";
}

/**
 * Coarse lifecycle buckets useful for office overviews and dashboards. Maps
 * every canonical {@link TaskStatus} into one of four lanes.
 */
export interface LifecycleBuckets {
  open: Task[];
  review: Task[];
  blocked: Task[];
  done: Task[];
}

/**
 * Bucket key used by {@link groupTasksByAgent} when a task has no owner.
 */
export const UNASSIGNED_BUCKET = "unassigned";

/**
 * Bucket key used by {@link groupTasksByWeek} for tasks without a `due_at`.
 */
export const UNSCHEDULED_BUCKET = "unscheduled";

function emptyStatusBuckets(): Record<TaskStatus, Task[]> {
  return {
    in_progress: [],
    open: [],
    review: [],
    pending: [],
    blocked: [],
    done: [],
    canceled: [],
  };
}

/**
 * Group tasks by their canonical status. Returns a record with every status
 * key present (empty arrays where there is no task), so callers can iterate
 * `TASK_STATUSES` for stable column ordering without null checks.
 */
export function groupTasksByStatus(tasks: Task[]): Record<TaskStatus, Task[]> {
  const groups = emptyStatusBuckets();
  for (const task of tasks) {
    groups[normalizeTaskStatus(task.status)].push(task);
  }
  return groups;
}

/**
 * Group tasks by owner slug. Tasks without an owner land in the
 * {@link UNASSIGNED_BUCKET} bucket. The bucket is always present in the
 * returned record (even if empty) so consumers can render a stable column.
 */
export function groupTasksByAgent(tasks: Task[]): Record<string, Task[]> {
  const groups: Record<string, Task[]> = { [UNASSIGNED_BUCKET]: [] };
  for (const task of tasks) {
    const slug = task.owner?.trim();
    const key = slug ? slug : UNASSIGNED_BUCKET;
    if (!groups[key]) groups[key] = [];
    groups[key].push(task);
  }
  return groups;
}

function isoDay(value: Date): string {
  // YYYY-MM-DD slice of the ISO timestamp. Using UTC keeps results stable
  // across timezones — projections are pure data shapes; surfaces format for
  // display.
  return value.toISOString().slice(0, 10);
}

function startOfUtcDay(value: Date): Date {
  return new Date(
    Date.UTC(value.getUTCFullYear(), value.getUTCMonth(), value.getUTCDate()),
  );
}

/**
 * Group tasks by the ISO date (UTC, `YYYY-MM-DD`) they are due, for the seven
 * days starting at `weekStart`. Tasks without a `due_at` land in
 * {@link UNSCHEDULED_BUCKET}. Tasks outside the seven-day window are dropped
 * (callers ask for a specific week).
 *
 * The seven daily keys are always present (empty arrays where applicable) so
 * callers can render stable day columns. The unscheduled bucket is always
 * present too.
 */
export function groupTasksByWeek(
  tasks: Task[],
  weekStart: Date,
): Record<string, Task[]> {
  const start = startOfUtcDay(weekStart);
  const groups: Record<string, Task[]> = { [UNSCHEDULED_BUCKET]: [] };
  const dayKeys: string[] = [];
  for (let offset = 0; offset < 7; offset += 1) {
    const day = new Date(start.getTime());
    day.setUTCDate(start.getUTCDate() + offset);
    const key = isoDay(day);
    dayKeys.push(key);
    groups[key] = [];
  }
  const validDays = new Set(dayKeys);
  for (const task of tasks) {
    if (!task.due_at) {
      groups[UNSCHEDULED_BUCKET].push(task);
      continue;
    }
    const due = new Date(task.due_at);
    if (Number.isNaN(due.getTime())) {
      groups[UNSCHEDULED_BUCKET].push(task);
      continue;
    }
    const key = isoDay(due);
    if (validDays.has(key)) {
      groups[key].push(task);
    }
  }
  return groups;
}

/**
 * Tasks without a scheduled `due_at`. Useful for inboxes and "needs scheduling"
 * lanes.
 */
export function selectUnscheduled(tasks: Task[]): Task[] {
  return tasks.filter((task) => {
    if (!task.due_at) return true;
    const due = new Date(task.due_at);
    return Number.isNaN(due.getTime());
  });
}

/**
 * Tasks without an owner. Useful for triage queues.
 */
export function selectUnassigned(tasks: Task[]): Task[] {
  return tasks.filter((task) => !task.owner?.trim());
}

/**
 * Group tasks into four coarse lifecycle lanes useful for overview surfaces:
 *
 *   - `open`: active work (`open`, `in_progress`, `pending`)
 *   - `review`: awaiting review
 *   - `blocked`: blocked
 *   - `done`: completed or cancelled (terminal)
 *
 * Unknown statuses fall through {@link normalizeTaskStatus} into `open`.
 */
export function groupTasksByLifecycle(tasks: Task[]): LifecycleBuckets {
  const buckets: LifecycleBuckets = {
    open: [],
    review: [],
    blocked: [],
    done: [],
  };
  for (const task of tasks) {
    const status = normalizeTaskStatus(task.status);
    switch (status) {
      case "open":
      case "in_progress":
      case "pending":
        buckets.open.push(task);
        break;
      case "review":
        buckets.review.push(task);
        break;
      case "blocked":
        buckets.blocked.push(task);
        break;
      case "done":
      case "canceled":
        buckets.done.push(task);
        break;
    }
  }
  return buckets;
}
