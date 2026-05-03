import { describe, expect, it } from "vitest";

import type { SchedulerJob } from "../../api/client";
import { isCadenceSchedulerJob } from "./schedulerJobClassification";

function job(overrides: Partial<SchedulerJob>): SchedulerJob {
  return {
    slug: "job",
    label: "Job",
    enabled: true,
    ...overrides,
  };
}

describe("isCadenceSchedulerJob", () => {
  it("classifies system, interval, and non-empty cron jobs as cadence jobs", () => {
    expect(isCadenceSchedulerJob(job({ system_managed: true }))).toBe(true);
    expect(isCadenceSchedulerJob(job({ interval_minutes: 5 }))).toBe(true);
    expect(isCadenceSchedulerJob(job({ schedule_expr: "0 9 * * MON" }))).toBe(
      true,
    );
  });

  it("keeps one-shot and blank-cron jobs in the timeline", () => {
    expect(isCadenceSchedulerJob(job({ due_at: "2026-05-02T12:00:00Z" }))).toBe(
      false,
    );
    expect(isCadenceSchedulerJob(job({ schedule_expr: "   " }))).toBe(false);
  });
});
