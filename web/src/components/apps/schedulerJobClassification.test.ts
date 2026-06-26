import { describe, expect, it } from "vitest";

import type { SchedulerJob } from "../../api/client";
import { isCadenceSchedulerJob } from "./schedulerJobClassification";

// Mirror the broker wire shape: schedulerJob serializes IntervalMinutes
// WITHOUT omitempty, so EVERY job arrives with interval_minutes present —
// one-shot watchdogs included (IntervalMinutes: 0 → "interval_minutes": 0).
// The fixture defaults to 0 on purpose so these tests catch the
// `typeof interval_minutes === "number"` regression that let one-shot
// follow-ups leak into the Routines list.
function job(overrides: Partial<SchedulerJob>): SchedulerJob {
  return {
    slug: "job",
    label: "Job",
    enabled: true,
    interval_minutes: 0,
    ...overrides,
  };
}

describe("isCadenceSchedulerJob", () => {
  it("classifies system, interval, override, and non-empty cron jobs as cadence jobs", () => {
    expect(isCadenceSchedulerJob(job({ system_managed: true }))).toBe(true);
    expect(isCadenceSchedulerJob(job({ interval_minutes: 5 }))).toBe(true);
    expect(isCadenceSchedulerJob(job({ interval_override: 30 }))).toBe(true);
    expect(isCadenceSchedulerJob(job({ schedule_expr: "0 9 * * MON" }))).toBe(
      true,
    );
    expect(isCadenceSchedulerJob(job({ cron: "*/15 * * * *" }))).toBe(true);
  });

  it("keeps one-shot, zero-interval, and blank-cron jobs out of routines", () => {
    expect(isCadenceSchedulerJob(job({ due_at: "2026-05-02T12:00:00Z" }))).toBe(
      false,
    );
    // interval_minutes: 0 is the broker default for one-shots — it is NOT a
    // cadence even though the field is present on the wire.
    expect(isCadenceSchedulerJob(job({ interval_minutes: 0 }))).toBe(false);
    expect(isCadenceSchedulerJob(job({ schedule_expr: "   " }))).toBe(false);
  });

  // Regression: the broker enqueues one-shot watchdog jobs per task-lifecycle
  // transition. On the wire they carry interval_minutes: 0, no cron, and are
  // not system-managed. They are internal plumbing, not user routines, and
  // must never appear on the Routines surface.
  it("excludes one-shot lifecycle watchdog jobs (follow-up / recheck)", () => {
    const watchdogs: Array<Partial<SchedulerJob>> = [
      {
        kind: "task_follow_up",
        target_type: "task",
        slug: "task_follow_up:general:t-1",
      },
      {
        kind: "request_follow_up",
        target_type: "request",
        slug: "request_follow_up:general:r-1",
      },
      {
        kind: "recheck",
        target_type: "task",
        slug: "recheck:general:task:t-1",
      },
    ];
    for (const w of watchdogs) {
      expect(
        isCadenceSchedulerJob(
          job({
            ...w,
            target_id: "t-1",
            interval_minutes: 0,
            due_at: "2026-05-02T12:00:00Z",
            next_run: "2026-05-02T12:00:00Z",
            status: "scheduled",
          }),
        ),
      ).toBe(false);
    }
  });
});
