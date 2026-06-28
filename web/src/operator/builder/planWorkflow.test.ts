import { describe, expect, it } from "vitest";

import { applyClarify, planWorkflow } from "./planWorkflow";

describe("planWorkflow enrich integration", () => {
  it("honors Salesforce when the prompt names it", () => {
    const plan = planWorkflow(
      "When a demo request comes in, look it up in Salesforce and route hot ones to an AE",
    );
    const enrich = plan.steps.find((s) => s.kind === "enrich");
    expect(enrich?.integration).toBe("Salesforce");
  });

  it("defaults a generic CRM lookup to HubSpot", () => {
    const plan = planWorkflow(
      "When a demo request comes in, look up the company in our CRM and route hot ones",
    );
    const enrich = plan.steps.find((s) => s.kind === "enrich");
    expect(enrich?.integration).toBe("HubSpot");
  });
});

describe("applyClarify preserves step semantics", () => {
  it("keeps email wording for a Gmail handoff instead of forcing a #channel", () => {
    const plan = planWorkflow("When a demo request comes in, email the owner");
    const action = plan.steps.find((s) => s.kind === "action");
    // Sanity: this flow really is a Gmail handoff with a channel clarify.
    expect(action?.integration).toBe("Gmail");
    expect(plan.clarify?.field).toBe("channel");

    const refined = applyClarify(plan.steps, "channel", "owner@acme.com");
    const refinedAction = refined.find((s) => s.kind === "action");
    expect(refinedAction?.detail).toContain("owner@acme.com");
    expect(refinedAction?.detail).not.toContain("Post to #");
  });

  it("keeps #channel wording for a Slack handoff", () => {
    const plan = planWorkflow(
      "When a demo request comes in, post it to the team channel in Slack",
    );
    const refined = applyClarify(plan.steps, "channel", "ae-handoffs");
    const refinedAction = refined.find((s) => s.kind === "action");
    expect(refinedAction?.detail).toContain("#ae-handoffs");
  });

  it("keeps severity wording for a support classify flow", () => {
    const plan = planWorkflow(
      "Triage support escalations by severity and page the on-call engineer for the worst ones",
    );
    const decision = plan.steps.find((s) => s.kind === "decision");
    expect(decision).toBeDefined();

    const refined = applyClarify(plan.steps, "threshold", "1");
    const refinedDecision = refined.find((s) => s.kind === "decision");
    expect(refinedDecision?.title.toLowerCase()).toContain("severity");
    expect(refinedDecision?.title).not.toContain("score");
  });
});
