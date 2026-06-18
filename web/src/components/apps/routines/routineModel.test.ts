import { describe, expect, it } from "vitest";
import type { SchedulerJob } from "../../../api/client";
import { routineOwner } from "./routineModel";

describe("routineOwner", () => {
  it("uses the scheduling agent for a Composio workflow job, not the vendor", () => {
    // Regression: a scheduled Composio workflow showed owner "composio" (the
    // integration vendor) instead of the real agent that scheduled it.
    const job: SchedulerJob = {
      slug: "composio-workflow-daily-email-digest",
      kind: "composio_workflow",
      target_type: "workflow",
      target_id: "daily-email-digest",
      agent: "ceo",
      provider: "composio",
    };
    expect(routineOwner(job)).toEqual({ slug: "ceo", kind: "agent" });
  });

  it("never treats the vendor as an agent when no agent is set", () => {
    const job: SchedulerJob = {
      slug: "one-workflow-x",
      kind: "one_workflow",
      target_type: "workflow",
      target_id: "x",
      provider: "one",
    };
    expect(routineOwner(job)).toEqual({ slug: null, kind: "workflow" });
  });

  it("resolves an explicit agent target", () => {
    const job: SchedulerJob = {
      slug: "agent-loop-tess",
      target_type: "agent",
      target_id: "tess",
    };
    expect(routineOwner(job)).toEqual({ slug: "tess", kind: "agent" });
  });

  it("reports system-managed crons as system", () => {
    const job: SchedulerJob = { slug: "wiki-lint", system_managed: true };
    expect(routineOwner(job)).toEqual({ slug: null, kind: "system" });
  });
});
