import { describe, expect, it } from "vitest";

import { actionLabel, buildMockRun, flattenRun } from "./browserExec";

describe("buildMockRun", () => {
  it("runs the inbound scenario and gates the external Slack send", () => {
    const steps = buildMockRun({ goal: "route inbound demo requests" });

    // Covers the full workflow shape.
    expect(steps.map((s) => s.kind)).toEqual([
      "trigger",
      "enrich",
      "ai",
      "decision",
      "action",
      "branch",
    ]);

    // Exactly one action sends externally, and it is gated for human approval.
    const gated = flattenRun(steps).filter((f) => f.action.gated);
    expect(gated).toHaveLength(1);
    expect(gated[0].action.target).toBe("Send");
    expect(gated[0].stepKind).toBe("action");

    // The run ends with a done action carrying a result.
    const last = flattenRun(steps).at(-1);
    expect(last?.action.kind).toBe("done");
    expect(last?.action.value).toMatch(/complete/i);
  });
});

describe("flattenRun", () => {
  it("flattens actions in order with their step context", () => {
    const steps = buildMockRun({ goal: "x" });
    const flat = flattenRun(steps);

    expect(flat.length).toBeGreaterThan(steps.length);
    // First action belongs to the trigger step.
    expect(flat[0].stepIndex).toBe(0);
    expect(flat[0].stepKind).toBe("trigger");
    // Step indices never decrease.
    for (let i = 1; i < flat.length; i++) {
      expect(flat[i].stepIndex).toBeGreaterThanOrEqual(flat[i - 1].stepIndex);
    }
  });
});

describe("actionLabel", () => {
  it("renders human one-liners per action kind", () => {
    expect(actionLabel({ kind: "navigate", value: "app.hubspot.com" })).toBe(
      "Opened app.hubspot.com",
    );
    expect(
      actionLabel({ kind: "type", value: "Acme", target: "company search" }),
    ).toBe("Typed “Acme” into company search");
    expect(
      actionLabel({ kind: "click", target: "the #ae-handoffs channel" }),
    ).toBe("Clicked the #ae-handoffs channel");
    expect(actionLabel({ kind: "read", value: "200+ employees" })).toBe(
      "Read: 200+ employees",
    );
  });
});
