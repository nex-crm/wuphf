import { fireEvent, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { WorkflowPlan } from "../builder/planWorkflow";
import { WorkflowBuilder } from "./WorkflowBuilder";

// The builder talks to buildPlanSmart (real engine, mock fallback). Pin it to a
// deterministic plan so the test exercises the handoff, not the planner.
vi.mock("../builder/agentClient", () => ({
  buildPlanSmart: vi.fn(),
}));

import { buildPlanSmart } from "../builder/agentClient";

const buildPlanSmartMock = vi.mocked(buildPlanSmart);

// A plan with one clarifying question on the decision step, so we can prove the
// operator's answer (the clarification) is carried in the finish snapshot.
const PLAN: WorkflowPlan = {
  name: "Inbound demo-request routing",
  toolId: "inbound-routing",
  steps: [
    {
      id: "p-trigger",
      kind: "trigger",
      title: "New demo request",
      detail: "When a demo form is submitted.",
    },
    {
      id: "p-decision",
      kind: "decision",
      title: "Does it clear the bar?",
      detail: "Set the cutoff that decides what moves on.",
    },
  ],
  narration: "Here is the workflow I put together.",
  clarify: {
    field: "threshold",
    stepId: "p-decision",
    prompt: "At what fit score should it move on to a person?",
  },
};

describe("WorkflowBuilder finish handoff", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    buildPlanSmartMock.mockResolvedValue(PLAN);
  });
  afterEach(() => {
    vi.useRealTimers();
    buildPlanSmartMock.mockReset();
  });

  it("hands off the built snapshot with clarified steps, and open vs run are distinct", async () => {
    const onFinish = vi.fn();
    const { getByLabelText, getByRole } = render(
      <WorkflowBuilder onClose={() => {}} onFinish={onFinish} />,
    );

    // Describe the workflow; the planner (mocked) returns PLAN, the steps
    // reveal, then the builder asks its one clarifying question.
    fireEvent.change(
      getByLabelText("Describe the workflow you want to build"),
      { target: { value: "route demo requests to an AE" } },
    );
    fireEvent.click(getByRole("button", { name: "Send" }));
    await vi.runAllTimersAsync();

    // Answer the clarification with a concrete cutoff. This must be applied to
    // the decision step in place, then the finish card appears.
    fireEvent.change(
      getByLabelText("Describe the workflow you want to build"),
      { target: { value: "75" } },
    );
    fireEvent.click(getByRole("button", { name: "Send" }));
    await vi.runAllTimersAsync();

    // Open the tool: onFinish gets the full snapshot, not just an id.
    fireEvent.click(getByRole("button", { name: /Open the tool/ }));
    expect(onFinish).toHaveBeenCalledTimes(1);
    const [draft, mode] = onFinish.mock.calls[0];
    expect(mode).toBe("open");
    expect(draft.toolId).toBe("inbound-routing");
    expect(draft.name).toBe("Inbound demo-request routing");
    // The clarified cutoff (75) survived into the snapshot's decision step.
    const decision = draft.steps.find(
      (s: { id: string }) => s.id === "p-decision",
    );
    expect(decision?.title).toContain("75");

    // Run on test data is a distinct action carrying the same snapshot.
    fireEvent.click(getByRole("button", { name: "Run on test data" }));
    expect(onFinish).toHaveBeenCalledTimes(2);
    expect(onFinish.mock.calls[1][1]).toBe("run");
    expect(onFinish.mock.calls[1][0].steps).toEqual(draft.steps);
  });
});
