import type { SchedulerJob } from "../../api/client";

export function isCadenceSchedulerJob(job: SchedulerJob): boolean {
  return (
    job.system_managed === true ||
    typeof job.interval_minutes === "number" ||
    hasCronExpression(job)
  );
}

function hasCronExpression(job: SchedulerJob): boolean {
  return (
    typeof job.schedule_expr === "string" && job.schedule_expr.trim().length > 0
  );
}
