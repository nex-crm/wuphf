import type { SchedulerJob } from "../../api/client";
import { routineOwner } from "./routines/routineModel";

/**
 * True when a routine is broker/system-managed (internal plumbing — e.g.
 * "Nex insights", "Wiki archive sweep", follow-up reminders) rather than
 * something the operator created. System routines are hidden by default on
 * the Routines surface and never shown on the work board, where they would
 * otherwise bury the operator's actual tasks under cron noise.
 */
export function isSystemRoutine(job: SchedulerJob): boolean {
  return routineOwner(job).kind === "system";
}

/**
 * True when a scheduler job is a recurring *routine* — a broker-managed
 * cron, an interval cadence, or a cron expression — as opposed to a
 * transient one-shot job.
 *
 * The broker enqueues one-shot watchdog jobs (`task_follow_up`,
 * `request_follow_up`, `recheck`) on every task-lifecycle transition. Those
 * are internal plumbing, not routines, and must stay out of the Routines
 * surface.
 *
 * Wire-shape trap: `schedulerJob.IntervalMinutes` is serialized WITHOUT
 * `omitempty`, so every job — one-shot watchdogs included — arrives with
 * `interval_minutes: 0`. A bare `typeof job.interval_minutes === "number"`
 * check is therefore true for ALL jobs, which let one-shot follow-ups flood
 * the Routines list. A cadence requires a *positive* interval.
 */
export function isCadenceSchedulerJob(job: SchedulerJob): boolean {
  return (
    job.system_managed === true ||
    hasPositiveInterval(job) ||
    hasCronExpression(job)
  );
}

function hasPositiveInterval(job: SchedulerJob): boolean {
  // interval_override (human cadence override) wins over interval_minutes,
  // mirroring routineModel.routineSchedule and the broker's nextRoutineRun.
  return (
    isPositiveNumber(job.interval_override) ||
    isPositiveNumber(job.interval_minutes)
  );
}

function isPositiveNumber(value: number | undefined): boolean {
  return typeof value === "number" && value > 0;
}

function hasCronExpression(job: SchedulerJob): boolean {
  const expr = job.schedule_expr ?? job.cron;
  return typeof expr === "string" && expr.trim().length > 0;
}
